package media

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/baoopn-2026/internal/mediarefs"
)

// Orphan candidate kinds (types.ts OrphanKind). A candidate's key is
// "<kind>:<name>"; POST /media/orphans takes those keys.
const (
	KindRow       = "row"        // a media row nothing references (and its files)
	KindObject    = "object"     // a bucket object with no row
	KindLocal     = "local"      // a MEDIA_DIR file with no row
	KindLocalCopy = "local_copy" // the local copy of a file that is in the bucket
	KindOriginal  = "original"   // MEDIA_DIR/originals/<stem> no job needs
	KindTemp      = "temp"       // an interrupted upload's temp file
)

// Why a candidate is not eligible yet.
const (
	BlockedGrace    = "grace"
	BlockedDisabled = "disabled"
)

// scratchAge is how old an original or a temp file must be before cleanup.
const scratchAge = 24 * time.Hour

// tempPrefixes are the temp files written straight into MEDIA_DIR: uploads,
// requeues, fallbacks and worker results (mediagrpc CompleteJob). Brand
// files use .tmp- inside brand/, which the scan does not enter.
var tempPrefixes = []string{".up-", ".rq-", ".fb-", ".wk-"}

// OrphanCandidate is one thing the cleanup would delete (types.ts).
type OrphanCandidate struct {
	Key         string     `json:"key"`
	Kind        string     `json:"kind"`
	Name        string     `json:"name"`
	MediaID     int64      `json:"media_id,omitempty"`
	URL         string     `json:"url,omitempty"` // rows: the file's public URL
	Original    string     `json:"original,omitempty"`
	ContentType string     `json:"content_type,omitempty"`
	SizeBytes   int64      `json:"size_bytes"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	ModifiedAt  time.Time  `json:"modified_at"` // rows: touched_at
	AgeSeconds  int64      `json:"age_seconds"`
	Reason      string     `json:"reason"`
	Eligible    bool       `json:"eligible"`
	Blocked     string     `json:"blocked"`
	EligibleAt  *time.Time `json:"eligible_at,omitempty"`

	stem    string
	objects []string // row: bucket keys with its stem
	locals  []string // row: MEDIA_DIR files with its stem
}

// OrphanMissing is a stem content references that has no row and no file.
type OrphanMissing struct {
	Stem  string `json:"stem"`
	Where string `json:"where"`
}

// OrphanKept counts what the plan keeps.
type OrphanKept struct {
	Referenced       int   `json:"referenced"`
	Processing       int   `json:"processing"`
	InGrace          int   `json:"in_grace"`
	RetainedOriginal int   `json:"retained_original"`
	Foreign          int   `json:"foreign"`
	ForeignBytes     int64 `json:"foreign_bytes"`
}

// OrphanTotals sums the candidates.
type OrphanTotals struct {
	Candidates    int   `json:"candidates"`
	Eligible      int   `json:"eligible"`
	Bytes         int64 `json:"bytes"`
	EligibleBytes int64 `json:"eligible_bytes"`
	WouldDelete   int   `json:"would_delete"`
}

// OrphanPlan is the dry run (GET /media/orphans).
type OrphanPlan struct {
	Enabled      bool              `json:"enabled"`
	Auto         bool              `json:"auto"`
	GraceSeconds int64             `json:"grace_seconds"`
	MaxPerPass   int               `json:"max_per_pass"`
	Storage      string            `json:"storage"` // bucket | disk
	GeneratedAt  time.Time         `json:"generated_at"`
	Truncated    bool              `json:"truncated"`
	Candidates   []OrphanCandidate `json:"candidates"`
	Missing      []OrphanMissing   `json:"missing"`
	Kept         OrphanKept        `json:"kept"`
	Totals       OrphanTotals      `json:"totals"`
	LastRun      *OrphanRun        `json:"last_run"`

	// For the auto brake: media rows, ready rows, referenced stems, and
	// stored-name files (bucket root objects plus MEDIA_DIR files).
	rows, readyRows, refs, files int
}

type orphanRow struct {
	id                       int64
	filename, stem, original string
	ctype, status            string
	size                     int64
	created, touched         time.Time
	jobStatus                *string
	originalDeleted          *bool
}

// active reports a job a worker or the janitor still needs.
func (r orphanRow) active() bool {
	return r.jobStatus != nil && (*r.jobStatus == "queued" || *r.jobStatus == "running")
}

// retained reports a failed job whose original is kept for a retry.
func (r orphanRow) retained() bool {
	return r.jobStatus != nil && *r.jobStatus == "failed" && r.originalDeleted != nil && !*r.originalDeleted
}

type localFile struct {
	name string
	size int64
	mod  time.Time
}

// OrphansEnabled reports whether orphan deletion is on (MEDIA_ORPHAN_GRACE > 0).
func (s *Store) OrphansEnabled() bool { return s.orphanGrace > 0 }

// PlanOrphans lists what a cleanup run would delete now, and why the rest is
// kept. CleanOrphans works from the same plan.
func (s *Store) PlanOrphans(ctx context.Context) (OrphanPlan, error) {
	now := time.Now().UTC()
	p := OrphanPlan{Enabled: s.OrphansEnabled(), MaxPerPass: s.orphanMax, Storage: "disk", GeneratedAt: now,
		Candidates: []OrphanCandidate{}, Missing: []OrphanMissing{}}
	if p.Enabled {
		p.GraceSeconds = int64(s.orphanGrace.Seconds())
	}
	if s.s3 != nil {
		p.Storage = "bucket"
	}
	st, err := s.orphanSettings(ctx)
	if err != nil {
		return p, err
	}
	p.Auto, p.LastRun = st.Auto, st.LastRun

	var rows []orphanRow
	var refs map[string]string
	err = pgx.BeginTxFunc(ctx, s.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly},
		func(tx pgx.Tx) error {
			var err error
			if rows, err = loadOrphanRows(ctx, tx); err != nil {
				return err
			}
			refs, err = mediarefs.ScanWhere(ctx, tx)
			return err
		})
	if err != nil {
		return p, err
	}
	p.rows, p.refs = len(rows), len(refs)

	var objects []listed
	if s.s3 != nil {
		if objects, p.Truncated, err = s.s3.list(ctx, s.listMax); err != nil {
			return p, err
		}
	}
	locals, temps, err := s.scanDir(&p.Kept)
	if err != nil {
		return p, err
	}
	originals, err := s.scanOriginals(&p.Kept)
	if err != nil {
		return p, err
	}

	byStem := map[string]*orphanRow{}
	for i := range rows {
		byStem[rows[i].stem] = &rows[i]
		if rows[i].status == StatusReady {
			p.readyRows++
		}
	}
	objByStem := map[string][]listed{}
	objByName := map[string]listed{}
	for _, o := range objects {
		if strings.Contains(o.Key, "/") || !fileRe.MatchString(o.Key) {
			p.Kept.Foreign++
			p.Kept.ForeignBytes += o.Size
			continue
		}
		st := stemOf(o.Key)
		objByStem[st] = append(objByStem[st], o)
		objByName[o.Key] = o
		p.files++
	}
	localByStem := map[string][]localFile{}
	for _, f := range locals {
		localByStem[stemOf(f.name)] = append(localByStem[stemOf(f.name)], f)
		p.files++
	}

	add := func(c OrphanCandidate, min time.Duration) {
		s.judge(&c, now, min, &p.Kept)
		p.Candidates = append(p.Candidates, c)
	}

	for _, r := range rows {
		if !fileRe.MatchString(r.filename) {
			// Not a name the store gives: no reference could ever match it.
			p.Kept.Foreign++
			continue
		}
		switch {
		case r.status == StatusProcessing || r.active():
			p.Kept.Processing++
			continue
		case r.status == StatusFailed && r.retained():
			p.Kept.RetainedOriginal++
			continue
		case refs[r.stem] != "":
			p.Kept.Referenced++
			continue
		}
		created := r.created.UTC()
		c := OrphanCandidate{Kind: KindRow, Name: r.filename, MediaID: r.id, URL: s.URL(r.filename),
			Original: r.original, ContentType: r.ctype, SizeBytes: r.size, CreatedAt: &created,
			ModifiedAt: r.touched.UTC(), Reason: "not referenced by any post, project or setting", stem: r.stem}
		if r.status == StatusFailed {
			c.Reason = "failed upload, not referenced"
		}
		for _, o := range objByStem[r.stem] {
			c.objects = append(c.objects, o.Key)
		}
		for _, f := range localByStem[r.stem] {
			c.locals = append(c.locals, f.name)
		}
		c.Key = KindRow + ":" + strconv.FormatInt(r.id, 10)
		add(c, s.orphanGrace)
	}

	if !p.Truncated {
		for _, o := range objects {
			st := stemOf(o.Key)
			if _, ok := objByName[o.Key]; !ok || byStem[st] != nil {
				continue // foreign, or covered by its row
			}
			if refs[st] != "" {
				p.Kept.Referenced++
				continue
			}
			add(OrphanCandidate{Key: KindObject + ":" + o.Key, Kind: KindObject, Name: o.Key, SizeBytes: o.Size,
				ModifiedAt: o.Modified.UTC(), Reason: "bucket object with no media row", stem: st}, s.orphanGrace)
		}
	}

	for _, f := range locals {
		st := stemOf(f.name)
		if r := byStem[st]; r != nil {
			// The stored file itself (disk), or the copy MigrateToBucket left.
			if s.s3 == nil || p.Truncated || r.status != StatusReady {
				continue
			}
			if o, ok := objByName[f.name]; ok && o.Size == f.size {
				add(OrphanCandidate{Key: KindLocalCopy + ":" + f.name, Kind: KindLocalCopy, Name: f.name,
					MediaID: r.id, SizeBytes: f.size, ModifiedAt: f.mod,
					Reason: "local copy of a file that is in the bucket (the last local backup)", stem: st}, s.orphanGrace)
			}
			continue
		}
		if refs[st] != "" {
			p.Kept.Referenced++
			continue
		}
		add(OrphanCandidate{Key: KindLocal + ":" + f.name, Kind: KindLocal, Name: f.name, SizeBytes: f.size,
			ModifiedAt: f.mod, Reason: "file in MEDIA_DIR with no media row", stem: st}, s.orphanGrace)
	}

	for _, f := range originals {
		if r := byStem[f.name]; r != nil && (r.active() || r.retained()) {
			continue // the worker, the janitor or a retry still needs it
		}
		add(OrphanCandidate{Key: KindOriginal + ":" + f.name, Kind: KindOriginal, Name: f.name, SizeBytes: f.size,
			ModifiedAt: f.mod, Reason: "upload original no job needs", stem: f.name}, scratchAge)
	}
	for _, f := range temps {
		add(OrphanCandidate{Key: KindTemp + ":" + f.name, Kind: KindTemp, Name: f.name, SizeBytes: f.size,
			ModifiedAt: f.mod, Reason: "interrupted upload"}, scratchAge)
	}

	if !p.Truncated {
		for st, where := range refs {
			if byStem[st] == nil && len(objByStem[st]) == 0 && len(localByStem[st]) == 0 {
				p.Missing = append(p.Missing, OrphanMissing{Stem: st, Where: where})
			}
		}
		sort.Slice(p.Missing, func(i, j int) bool { return p.Missing[i].Stem < p.Missing[j].Stem })
	}

	sort.SliceStable(p.Candidates, func(i, j int) bool {
		a, b := p.Candidates[i], p.Candidates[j]
		if !a.ModifiedAt.Equal(b.ModifiedAt) {
			return a.ModifiedAt.Before(b.ModifiedAt)
		}
		return a.Key < b.Key
	})
	for _, c := range p.Candidates {
		p.Totals.Candidates++
		p.Totals.Bytes += c.SizeBytes
		if c.Eligible {
			p.Totals.Eligible++
			p.Totals.EligibleBytes += c.SizeBytes
		}
	}
	p.Totals.WouldDelete = min(p.Totals.Eligible, p.MaxPerPass)
	return p, nil
}

// judge sets a candidate's age and eligibility: it must be unused for min.
func (s *Store) judge(c *OrphanCandidate, now time.Time, min time.Duration, kept *OrphanKept) {
	c.ModifiedAt = c.ModifiedAt.UTC()
	c.AgeSeconds = max(0, int64(now.Sub(c.ModifiedAt).Seconds()))
	at := c.ModifiedAt.Add(min)
	switch {
	case !s.OrphansEnabled():
		c.Blocked = BlockedDisabled
	case now.Before(at):
		c.Blocked = BlockedGrace
		c.EligibleAt = &at
		kept.InGrace++
	default:
		c.Eligible = true
	}
}

func loadOrphanRows(ctx context.Context, tx pgx.Tx) ([]orphanRow, error) {
	rows, err := tx.Query(ctx, `SELECT m.id, m.filename, m.stem, m.original, m.content_type, m.status,
		m.size_bytes, m.created_at, m.touched_at, j.status, j.original_deleted
		FROM media m LEFT JOIN media_jobs j ON j.media_id = m.id ORDER BY m.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (orphanRow, error) {
		var x orphanRow
		return x, r.Scan(&x.id, &x.filename, &x.stem, &x.original, &x.ctype, &x.status, &x.size,
			&x.created, &x.touched, &x.jobStatus, &x.originalDeleted)
	})
}

// scanDir lists MEDIA_DIR: stored-name files, temp files, and (counted as
// foreign) anything else except the originals directory.
func (s *Store) scanDir(kept *OrphanKept) (files, temps []localFile, err error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if name != "originals" {
				kept.Foreign++
			}
			continue
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue // removed meanwhile
		}
		if err != nil {
			return nil, nil, err
		}
		f := localFile{name: name, size: info.Size(), mod: info.ModTime().UTC()}
		switch {
		case !info.Mode().IsRegular():
			kept.Foreign++
		case fileRe.MatchString(name):
			files = append(files, f)
		case isTemp(name):
			temps = append(temps, f)
		default:
			kept.Foreign++
			kept.ForeignBytes += f.size
		}
	}
	return files, temps, nil
}

func isTemp(name string) bool {
	for _, p := range tempPrefixes {
		if strings.HasPrefix(name, p) && len(name) > len(p) && len(name) <= 64 {
			return true
		}
	}
	return false
}

// scanOriginals lists MEDIA_DIR/originals (files named by a stem).
func (s *Store) scanOriginals(kept *OrphanKept) ([]localFile, error) {
	entries, err := os.ReadDir(s.originalsDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []localFile
	for _, e := range entries {
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if e.IsDir() || !info.Mode().IsRegular() || !isStem(e.Name()) {
			kept.Foreign++
			kept.ForeignBytes += info.Size()
			continue
		}
		out = append(out, localFile{name: e.Name(), size: info.Size(), mod: info.ModTime().UTC()})
	}
	return out, nil
}

func isStem(s string) bool {
	st := mediarefs.Stems(s)
	return len(s) == mediarefs.StemLen && len(st) == 1
}

// localPath is a MEDIA_DIR top-level file (names come from a listing).
func (s *Store) localPath(name string) string { return filepath.Join(s.dir, filepath.Base(name)) }
