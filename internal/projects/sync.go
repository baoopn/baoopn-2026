package projects

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/logging"
	"github.com/baoopn/baoopn-2026/internal/markdown"
	"github.com/baoopn/baoopn-2026/internal/settings"
)

// SyncDeps are the GitHub syncer's dependencies.
type SyncDeps struct {
	DB          *pgxpool.Pool
	Log         *slog.Logger
	User, Token string
	HTTP        *http.Client
	APIBase     string // default https://api.github.com
}

// Syncer pulls the user's GitHub repos into the projects table.
type Syncer struct {
	db   *pgxpool.Pool
	log  *slog.Logger
	user string
	gh   *ghClient
	mu   sync.Mutex
	kick chan int64

	startDelay time.Duration
}

// SyncResult is the outcome of a sync (types.ts SyncResult).
type SyncResult struct {
	Added    int       `json:"added"`
	Updated  int       `json:"updated"`
	Total    int       `json:"total"`
	SyncedAt time.Time `json:"synced_at"`
}

// syncState is stored under the github_sync settings key.
type syncState struct {
	SyncedAt  *time.Time `json:"synced_at"`
	ETag      string     `json:"etag"`
	LastError string     `json:"last_error"`
}

const (
	stateKey       = "github_sync"
	detailsPerRun  = 20
	defaultStartIn = 30 * time.Second
)

// ErrNotConfigured is returned when GITHUB_USER is empty.
var ErrNotConfigured = errors.New("projects: GitHub user not configured")

// NewSyncer returns a syncer.
func NewSyncer(d SyncDeps) *Syncer {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Syncer{db: d.DB, log: d.Log, user: d.User, gh: newGHClient(d.HTTP, d.APIBase, d.Token),
		kick: make(chan int64, 32), startDelay: defaultStartIn}
}

// Kick asks Run to fetch the README and languages of one project soon.
// It never blocks; a full queue drops the request.
func (s *Syncer) Kick(id int64) {
	if s == nil {
		return
	}
	select {
	case s.kick <- id:
	default:
	}
}

// Run syncs every interval (after a 30 s start delay, and only if the last
// sync is older than interval) and serves Kick requests until ctx ends.
func (s *Syncer) Run(ctx context.Context, interval time.Duration) {
	if s.user == "" || interval <= 0 {
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(s.startDelay):
	}
	var st syncState
	if _, err := settings.Get(ctx, s.db, stateKey, &st); err != nil {
		s.log.Warn("github sync: load state", logging.Err(err))
	}
	if st.SyncedAt == nil || time.Since(*st.SyncedAt) >= interval {
		s.runOnce(ctx)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.runOnce(ctx)
		case id := <-s.kick:
			s.mu.Lock()
			if err := s.details(ctx, &id); err != nil && ctx.Err() == nil {
				s.log.Warn("github sync: details", slog.Int64("project_id", id), logging.Err(err))
			}
			s.mu.Unlock()
		}
	}
}

func (s *Syncer) runOnce(ctx context.Context) {
	res, err := s.Sync(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("github sync failed", logging.Err(err))
		}
		return
	}
	s.log.Info("github sync", slog.Int("added", res.Added), slog.Int("updated", res.Updated), slog.Int("total", res.Total))
}

// Sync runs a full sync, waiting for any sync already in progress.
func (s *Syncer) Sync(ctx context.Context) (SyncResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncLocked(ctx)
}

// syncLocked runs a full sync; the caller holds s.mu.
func (s *Syncer) syncLocked(ctx context.Context) (SyncResult, error) {
	if s.user == "" {
		return SyncResult{}, ErrNotConfigured
	}
	var st syncState
	if _, err := settings.Get(ctx, s.db, stateKey, &st); err != nil {
		return SyncResult{}, err
	}
	repos, etag, notModified, err := s.gh.listRepos(ctx, s.user, st.ETag)
	if err != nil {
		st.LastError = err.Error()
		if perr := settings.Put(ctx, s.db, stateKey, st); perr != nil {
			s.log.Warn("github sync: save state", logging.Err(perr))
		}
		return SyncResult{}, err
	}
	var res SyncResult
	if !notModified {
		if res.Added, res.Updated, err = s.upsert(ctx, repos); err != nil {
			return SyncResult{}, err
		}
		st.ETag = etag
	}
	st.LastError = ""
	if err := s.details(ctx, nil); err != nil {
		if ctx.Err() != nil {
			return SyncResult{}, err
		}
		st.LastError = err.Error()
		s.log.Warn("github sync: details pass", logging.Err(err))
	}
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM projects WHERE github_id IS NOT NULL`).Scan(&res.Total); err != nil {
		return SyncResult{}, err
	}
	now := time.Now().UTC()
	res.SyncedAt = now
	st.SyncedAt = &now
	if err := settings.Put(ctx, s.db, stateKey, st); err != nil {
		return SyncResult{}, err
	}
	return res, nil
}

// upsert writes the GitHub columns of every public repo. Admin-owned columns
// are never touched on update; new repos arrive hidden.
func (s *Syncer) upsert(ctx context.Context, repos []ghRepo) (added, updated int, err error) {
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		bySlug := map[string]bool{}
		byID := map[int64]string{}
		rows, err := tx.Query(ctx, `SELECT github_id, slug FROM projects`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var gid *int64
			var slug string
			if err := rows.Scan(&gid, &slug); err != nil {
				rows.Close()
				return err
			}
			bySlug[slug] = true
			if gid != nil {
				byID[*gid] = slug
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		seen := map[int64]bool{}
		for _, r := range repos {
			if r.Private || r.ID == 0 || seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			slug, ok := byID[r.ID]
			if !ok {
				slug = uniqueSlug(r.Name, bySlug)
				bySlug[slug] = true
			}
			topics := orEmpty(r.Topics)
			var inserted bool
			err := tx.QueryRow(ctx, `
				INSERT INTO projects (slug, github_id, title, repo_full_name, repo_url, gh_homepage, gh_description,
					gh_language, gh_topics, gh_stars, gh_forks, gh_archived, gh_fork, gh_private, gh_pushed_at,
					gh_created_at, gh_default_branch, synced_at, visible)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, now(), false)
				ON CONFLICT (github_id) DO UPDATE SET repo_full_name = EXCLUDED.repo_full_name,
					repo_url = EXCLUDED.repo_url, gh_homepage = EXCLUDED.gh_homepage,
					gh_description = EXCLUDED.gh_description, gh_language = EXCLUDED.gh_language,
					gh_topics = EXCLUDED.gh_topics, gh_stars = EXCLUDED.gh_stars, gh_forks = EXCLUDED.gh_forks,
					gh_archived = EXCLUDED.gh_archived, gh_fork = EXCLUDED.gh_fork, gh_private = EXCLUDED.gh_private,
					gh_pushed_at = EXCLUDED.gh_pushed_at, gh_created_at = EXCLUDED.gh_created_at,
					gh_default_branch = EXCLUDED.gh_default_branch, synced_at = now()
				RETURNING (xmax = 0)`,
				slug, r.ID, r.Name, r.FullName, r.HTMLURL, r.Homepage, r.Description, r.Language, topics,
				r.Stars, r.Forks, r.Archived, r.Fork, r.Private, r.PushedAt, r.CreatedAt, r.DefaultBranch,
			).Scan(&inserted)
			if err != nil {
				return err
			}
			if inserted {
				added++
			} else {
				updated++
			}
		}
		missing := 0
		for gid := range byID {
			if !seen[gid] {
				missing++
			}
		}
		if missing > 0 {
			s.log.Info("github sync: repos no longer listed (left as is)", slog.Int("count", missing))
		}
		return nil
	})
	return added, updated, err
}

func uniqueSlug(name string, taken map[string]bool) string {
	base := markdown.Slugify(name)
	if base == "" {
		base = "project"
	}
	for n := 1; ; n++ {
		if s := suffixed(base, n); !taken[s] {
			return s
		}
	}
}

// details fetches README and languages for visible synced projects whose
// pushed_at changed since the last fetch (only project *only, when set).
func (s *Syncer) details(ctx context.Context, only *int64) error {
	q := `SELECT id, repo_full_name, gh_default_branch FROM projects
		WHERE visible AND github_id IS NOT NULL AND details_pushed_at IS DISTINCT FROM gh_pushed_at`
	args := []any{}
	if only != nil {
		q += ` AND id = $1`
		args = append(args, *only)
	}
	q += ` ORDER BY id LIMIT ` + strconv.Itoa(detailsPerRun)
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return err
	}
	type target struct {
		id           int64
		full, branch string
	}
	todo, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (target, error) {
		var t target
		err := r.Scan(&t.id, &t.full, &t.branch)
		return t, err
	})
	if err != nil {
		return err
	}
	for _, t := range todo {
		if s.gh.lowOnQuota() {
			s.log.Info("github sync: rate limit low, deferring details", slog.Int("left", len(todo)))
			return nil
		}
		readme, err := s.gh.readme(ctx, t.full)
		if err != nil {
			return err
		}
		langs, err := s.gh.languages(ctx, t.full)
		if err != nil {
			return err
		}
		html := ""
		if readme != "" {
			link, image := readmeBases(t.full, t.branch)
			res, err := markdown.Render(readme, markdown.Options{LinkBase: link, ImageBase: image})
			if err != nil {
				return err
			}
			html = res.HTML
		}
		if _, err := s.db.Exec(ctx, `UPDATE projects SET readme_html = $2, gh_languages = $3,
			details_pushed_at = gh_pushed_at, synced_at = now() WHERE id = $1`, t.id, html, langs); err != nil {
			return err
		}
	}
	return nil
}
