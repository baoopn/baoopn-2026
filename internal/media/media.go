// Package media stores uploaded files in MEDIA_DIR, or in an S3-compatible
// bucket (Cloudflare R2, RustFS in development) when one is configured,
// records them in the media table (deduplicated by content hash) and serves
// them at /media/<filename> (from disk, streamed from the bucket, or as a
// redirect to the bucket's public domain).
//
// Raster uploads are not stored as sent: the original is kept privately in
// MEDIA_DIR/originals and a job is queued (queue.go) for the media worker,
// which converts it to WebP over gRPC. SVG, PDF, AVIF and animated GIF/WebP
// files are stored as they are.
package media

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/imagemeta"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// Deps are the media store's dependencies.
type Deps struct {
	DB       *pgxpool.Pool
	Log      *slog.Logger
	Dir      string
	MaxBytes int64
	// S3 moves storage to a bucket when S3.Enabled(); Dir then only holds
	// upload temp files, the private originals and the files still to migrate.
	S3 S3Config

	// MaxPixels rejects raster uploads with more pixels (decompression bombs).
	MaxPixels int64
	// UploadWait is how long POST /media waits for a queued conversion.
	UploadWait time.Duration
	// JobLease is how long a worker owns a leased job.
	JobLease time.Duration
	// MaxAttempts per job before it falls back or fails.
	MaxAttempts int
	// FallbackAfter: a conversion job not leased for this long is published
	// as its (stripped) original, or failed when browsers cannot show it.
	FallbackAfter time.Duration
	// FailedRetention keeps a failed job's original for a retry.
	FailedRetention time.Duration
	// OrphanGrace: unreferenced files are deleted only after going this long
	// unused (orphans.go). Negative turns all orphan deletion off; 0 is the
	// default (7 days).
	OrphanGrace time.Duration
	// OrphanMaxPerPass caps the deletions of one cleanup run (default 50).
	OrphanMaxPerPass int
	// OnRename is called when a fallback publishes a file under a new name
	// (the WebP name changes extension), so stored references can follow.
	OnRename func(ctx context.Context, oldURL, newURL string) error
}

// Store saves, lists, deletes and serves media files.
type Store struct {
	db       *pgxpool.Pool
	log      *slog.Logger
	dir      string
	maxBytes int64
	files    backend
	s3       *s3Backend // the bucket when files is one; nil: local disk
	public   string     // bucket's public origin; "" = served by the API

	maxPixels       int64
	uploadWait      time.Duration
	jobLease        time.Duration
	maxAttempts     int
	fallbackAfter   time.Duration
	failedRetention time.Duration
	onRename        func(ctx context.Context, oldURL, newURL string) error

	orphanGrace time.Duration // <= 0: deletion disabled
	orphanMax   int
	listMax     int        // bucket listing limit (tests lower it)
	orphanMu    sync.Mutex // serializes media.orphans settings updates
}

// Media statuses.
const (
	StatusReady      = "ready"
	StatusProcessing = "processing"
	StatusFailed     = "failed"
)

// Media is one stored file (types.ts Media).
type Media struct {
	ID          int64     `json:"id"`
	URL         string    `json:"url"`
	Filename    string    `json:"filename"`
	Original    string    `json:"original"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	Status      string    `json:"status"`
	Error       string    `json:"error"`
	Converted   bool      `json:"converted"`
	CreatedAt   time.Time `json:"created_at"`
}

// Crop is an optional upload crop in original pixels, after EXIF orientation.
type Crop struct{ X, Y, W, H int }

var (
	// ErrTooLarge is returned when the file exceeds MaxBytes.
	ErrTooLarge = errors.New("media: file too large")
	// ErrUnsupported is returned when the content type is not allowed.
	ErrUnsupported = errors.New("media: unsupported file type")
	// ErrUndecodable is returned when an image's header cannot be read.
	ErrUndecodable = errors.New("media: the image could not be decoded")
	// ErrTooManyPixels is the decompression-bomb guard.
	ErrTooManyPixels = errors.New("media: image dimensions too large")
	// ErrBadCrop is returned when the crop does not fit the image.
	ErrBadCrop = errors.New("media: crop rectangle is outside the image")
	// ErrOriginalsUnavailable means the private originals store is not writable.
	ErrOriginalsUnavailable = errors.New("media: originals store unavailable")
)

const (
	defaultMaxBytes        = 10 << 20
	defaultMaxPixels       = 50_000_000
	defaultUploadWait      = 10 * time.Second
	defaultJobLease        = 5 * time.Minute
	defaultMaxAttempts     = 5
	defaultFallbackAfter   = 2 * time.Minute
	defaultFailedRetention = 24 * time.Hour
	defaultOrphanGrace     = 7 * 24 * time.Hour
	defaultOrphanMax       = 50
	defaultListMax         = 10_000
)

// New creates the media directory (0o755) and returns the store.
func New(d Deps) (*Store, error) {
	if d.Dir == "" {
		return nil, errors.New("media: Dir is required")
	}
	if err := os.MkdirAll(d.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("media: create dir: %w", err)
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.MaxBytes <= 0 {
		d.MaxBytes = defaultMaxBytes
	}
	s := &Store{db: d.DB, log: d.Log, dir: d.Dir, maxBytes: d.MaxBytes, files: localDisk{d.Dir},
		maxPixels: orDefault(d.MaxPixels, defaultMaxPixels), uploadWait: d.UploadWait,
		jobLease: orDefault(d.JobLease, defaultJobLease), maxAttempts: orDefault(d.MaxAttempts, defaultMaxAttempts),
		fallbackAfter:   orDefault(d.FallbackAfter, defaultFallbackAfter),
		failedRetention: orDefault(d.FailedRetention, defaultFailedRetention), onRename: d.OnRename,
		orphanGrace: d.OrphanGrace, orphanMax: orDefault(d.OrphanMaxPerPass, defaultOrphanMax), listMax: defaultListMax}
	if s.orphanGrace == 0 {
		s.orphanGrace = defaultOrphanGrace
	}
	if s.uploadWait == 0 {
		s.uploadWait = defaultUploadWait
	}
	if s.uploadWait < 0 { // negative: do not wait (tests)
		s.uploadWait = 0
	}
	if d.S3.Enabled() {
		s.s3 = newS3Backend(d.S3)
		s.files = s.s3
		s.public = strings.TrimRight(d.S3.PublicURL, "/")
	}
	return s, nil
}

func orDefault[T int | int64 | time.Duration](v, def T) T {
	if v <= 0 {
		return def
	}
	return v
}

// UsesBucket reports whether files are stored in the S3 bucket.
func (s *Store) UsesBucket() bool { return s.s3 != nil }

// URL is the address of a stored file: the bucket's public domain when it
// has one, otherwise /media/<filename> on the site.
func (s *Store) URL(filename string) string {
	if s.s3 != nil && s.public != "" {
		return s.public + "/" + filename
	}
	return "/media/" + filename
}

// headWriter keeps the first sniffLen bytes written to it.
type headWriter struct{ buf []byte }

func (h *headWriter) Write(p []byte) (int, error) {
	if room := sniffLen - len(h.buf); room > 0 {
		h.buf = append(h.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// Save stores the contents of r without a crop.
func (s *Store) Save(ctx context.Context, original string, r io.Reader) (Media, error) {
	return s.SaveCrop(ctx, original, r, nil)
}

// SaveCrop stores the contents of r. An identical file (same sha256, or the
// same uncropped original) already in the table is reused instead. A raster
// image is queued for WebP conversion and returned as processing; crop
// applies to those only.
func (s *Store) SaveCrop(ctx context.Context, original string, r io.Reader, crop *Crop) (Media, error) {
	tmp, err := os.CreateTemp(s.dir, ".up-*")
	if err != nil {
		return Media{}, fmt.Errorf("media: temp file: %w", err)
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()

	hash := sha256.New()
	head := &headWriter{}
	n, err := io.Copy(io.MultiWriter(tmp, hash, head), io.LimitReader(r, s.maxBytes+1))
	if err != nil {
		return Media{}, fmt.Errorf("media: write: %w", err)
	}
	if n > s.maxBytes {
		return Media{}, ErrTooLarge
	}
	if err := tmp.Close(); err != nil {
		return Media{}, fmt.Errorf("media: close: %w", err)
	}
	original = cleanOriginal(original)
	ctype, ext, ok := sniff(head.buf, original)
	if !ok {
		return Media{}, ErrUnsupported
	}
	sum := hex.EncodeToString(hash.Sum(nil))

	raster := imagemeta.Raster(ctype)
	var info imagemeta.Info
	if raster {
		data, err := os.ReadFile(tmpName)
		if err != nil {
			return Media{}, fmt.Errorf("media: read upload: %w", err)
		}
		if info, err = imagemeta.Probe(data, ctype); err != nil {
			return Media{}, ErrUndecodable
		}
		if info.Animated {
			raster = false // animated GIF/WebP: stored as they are
		} else {
			if info.SizeKnown && info.Pixels() > s.maxPixels {
				return Media{}, ErrTooManyPixels
			}
			if crop != nil && info.SizeKnown {
				w, h := info.Oriented()
				if !cropFits(*crop, w, h) {
					return Media{}, ErrBadCrop
				}
			}
		}
	}
	if !raster {
		crop = nil
	}
	if crop == nil {
		existing, err := s.bySHA(ctx, sum)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Media{}, fmt.Errorf("media: dedupe lookup: %w", err)
		}
	}

	stem := randomStem()
	if raster {
		m, err := s.enqueue(ctx, stem, original, ctype, sum, n, info.Orientation, crop, tmpName)
		if err == nil {
			keep = true // moved into originals/
		}
		return m, err
	}

	filename := stem + ext
	if err := s.files.put(ctx, filename, ctype, tmpName, n); err != nil {
		return Media{}, err
	}
	if s.s3 == nil {
		keep = true // renamed into place
	}
	m := Media{Filename: filename, Original: original, ContentType: ctype, SizeBytes: n, Status: StatusReady}
	err = s.db.QueryRow(ctx, `
		INSERT INTO media (filename, original, content_type, size_bytes, sha256, original_sha256)
		VALUES ($1, $2, $3, $4, $5, $5) RETURNING id, created_at`,
		filename, original, ctype, n, sum).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		s.removeFile(ctx, filename)
		return Media{}, fmt.Errorf("media: insert: %w", err)
	}
	return s.finish(m), nil
}

func cropFits(c Crop, w, h int) bool {
	return c.X >= 0 && c.Y >= 0 && c.W >= 1 && c.H >= 1 && c.X <= w-c.W && c.Y <= h-c.H
}

func randomStem() string {
	rnd := make([]byte, 12)
	_, _ = rand.Read(rnd)
	return hex.EncodeToString(rnd)
}

// mediaCols are the columns scanMedia reads, in order.
const mediaCols = `id, filename, original, content_type, size_bytes, status, error, converted, created_at`

type scanner interface{ Scan(dest ...any) error }

func scanMedia(row scanner, extra ...any) (Media, error) {
	var m Media
	err := row.Scan(append([]any{&m.ID, &m.Filename, &m.Original, &m.ContentType, &m.SizeBytes,
		&m.Status, &m.Error, &m.Converted, &m.CreatedAt}, extra...)...)
	return m, err
}

// bySHA finds a non-failed file whose stored bytes or uncropped original
// have this hash, and bumps its touched_at: the upload is about to be used,
// so the orphan cleanup must not take it. A row the cleanup is deleting
// waits on its lock and then matches nothing (pgx.ErrNoRows: a new file).
func (s *Store) bySHA(ctx context.Context, sum string) (Media, error) {
	m, err := scanMedia(s.db.QueryRow(ctx, `UPDATE media SET touched_at = now() WHERE id = (
		SELECT id FROM media WHERE (sha256 = $1 OR original_sha256 = $1) AND status <> 'failed'
		ORDER BY id LIMIT 1) RETURNING `+mediaCols, sum))
	if err != nil {
		return Media{}, err
	}
	return s.finish(m), nil
}

// Get returns one media row; pgx.ErrNoRows when it does not exist.
func (s *Store) Get(ctx context.Context, id int64) (Media, error) {
	m, err := scanMedia(s.db.QueryRow(ctx, `SELECT `+mediaCols+` FROM media WHERE id = $1`, id))
	if err != nil {
		return Media{}, err
	}
	return s.finish(m), nil
}

func (s *Store) finish(m Media) Media {
	m.URL = s.URL(m.Filename)
	m.CreatedAt = m.CreatedAt.UTC()
	return m
}

// List returns one page of media, newest first, and the total count.
func (s *Store) List(ctx context.Context, page, limit int) ([]Media, int64, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+mediaCols+`, count(*) OVER ()
		FROM media ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`,
		limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Media{}
	var total int64
	for rows.Next() {
		m, err := scanMedia(rows, &total)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s.finish(m))
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(out) == 0 && page > 1 {
		// Past the last page the window count is absent; report the real total.
		if err := s.db.QueryRow(ctx, `SELECT count(*) FROM media`).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

// Delete removes the row (and its job), its file and any kept original. It
// reports false when no row matched.
func (s *Store) Delete(ctx context.Context, id int64) (bool, error) {
	var filename string
	err := s.db.QueryRow(ctx, `DELETE FROM media WHERE id = $1 RETURNING filename`, id).Scan(&filename)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s.removeFile(ctx, filename)
	s.removeOriginal(stemOf(filename))
	return true, nil
}

// backend is where stored files live: localDisk (MEDIA_DIR) or the S3 bucket.
type backend interface {
	// put stores the finished temp file at tmpName as filename. localDisk
	// renames it; the bucket uploads it (the caller removes the temp file).
	put(ctx context.Context, filename, ctype, tmpName string, size int64) error
	remove(ctx context.Context, filename string) error
}

// localDisk keeps files in MEDIA_DIR.
type localDisk struct{ dir string }

func (l localDisk) put(_ context.Context, filename, _, tmpName string, _ int64) error {
	final := filepath.Join(l.dir, filename)
	if err := os.Rename(tmpName, final); err != nil {
		return fmt.Errorf("media: rename: %w", err)
	}
	_ = os.Chmod(final, 0o644)
	return nil
}

func (l localDisk) remove(_ context.Context, filename string) error {
	if err := os.Remove(filepath.Join(l.dir, filename)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// removeFile deletes a stored file from the backend and, with a bucket, also
// the local copy a migration leaves behind. Failures are logged.
func (s *Store) removeFile(ctx context.Context, filename string) {
	if err := s.files.remove(ctx, filename); err != nil {
		s.log.Warn("media: remove stored file", slog.String("filename", filename), logging.Err(err))
	}
	if s.s3 != nil {
		if err := (localDisk{s.dir}).remove(ctx, filename); err != nil {
			s.log.Warn("media: remove local copy", slog.String("filename", filename), logging.Err(err))
		}
	}
}

// CreateTemp creates a temp file in MEDIA_DIR (same filesystem as the
// stored files, so local storage can rename it into place).
func (s *Store) CreateTemp(pattern string) (*os.File, error) { return os.CreateTemp(s.dir, pattern) }
