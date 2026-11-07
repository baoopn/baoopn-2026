// Package seedimport imports the embedded seed posts (seed/posts/<slug>/)
// as drafts, each once, and uploads the seeded profile's local images.
package seedimport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/blog"
	"github.com/baoopn/baoopn-2026/internal/logging"
	"github.com/baoopn/baoopn-2026/internal/media"
	"github.com/baoopn/baoopn-2026/internal/settings"
)

// FlagKey holds the JSON list of seed post slugs already handled. A slug in
// it is never imported again, even after the post is deleted; folders added
// later are imported on the next start.
const FlagKey = "seed.posts_2026"

// Deps are the importer's dependencies.
type Deps struct {
	DB    *pgxpool.Pool
	Log   *slog.Logger
	Media *media.Store
	Blog  *blog.Module
	FS    fs.FS // seed.FS: posts/<slug>/{post.json,part-N.md,img/<id>.*}, assets/<path>
}

// postFile is seed/posts/<slug>/post.json.
type postFile struct {
	Slug     string   `json:"slug"`
	Title    string   `json:"title"`
	Excerpt  string   `json:"excerpt"`
	Category string   `json:"category"` // optional: "tech" (default) or "life"
	Tags     []string `json:"tags"`
	Parts    []struct {
		Title string `json:"title"`
		File  string `json:"file"`
	} `json:"parts"`
	Screenshots []struct {
		ID   string `json:"id"`
		File string `json:"file"`
	} `json:"screenshots"`
}

// Run imports every seed post not handled before and fixes up the profile
// images. A bad post or a missing image is logged and skipped.
func Run(ctx context.Context, d Deps) error {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.DB == nil || d.Media == nil || d.Blog == nil || d.FS == nil {
		return errors.New("seedimport: missing dependency")
	}
	im := &importer{d: d}
	if err := im.profileImages(ctx); err != nil {
		d.Log.Warn("seedimport: profile images", logging.Err(err))
	}

	var done []string
	if _, err := settings.Get(ctx, d.DB, FlagKey, &done); err != nil {
		return fmt.Errorf("seedimport: read flag: %w", err)
	}
	handled := map[string]bool{}
	for _, s := range done {
		handled[s] = true
	}
	files, err := fs.Glob(d.FS, "posts/*/post.json")
	if err != nil {
		return fmt.Errorf("seedimport: glob: %w", err)
	}
	sort.Strings(files)
	inserted := 0
	for _, f := range files {
		slug, ok, err := im.importPost(ctx, f, handled)
		if err != nil {
			d.Log.Warn("seedimport: post skipped", slog.String("file", f), logging.Err(err))
			continue
		}
		if slug != "" && !handled[slug] {
			handled[slug] = true
			done = append(done, slug)
		}
		if ok {
			inserted++
		}
	}
	if len(done) > 0 {
		if err := settings.Put(ctx, d.DB, FlagKey, done); err != nil {
			return fmt.Errorf("seedimport: set flag: %w", err)
		}
	}
	if inserted > 0 {
		d.Log.Info("seedimport: posts imported as drafts", slog.Int("posts", inserted))
	}
	return nil
}

type importer struct{ d Deps }

// importPost imports one post.json as a draft. It returns the post's slug
// once it is handled (imported now, or already present) and whether it was
// inserted now.
func (im *importer) importPost(ctx context.Context, file string, handled map[string]bool) (string, bool, error) {
	dir := path.Dir(file)
	raw, err := fs.ReadFile(im.d.FS, file)
	if err != nil {
		return "", false, err
	}
	var pf postFile
	if err := json.Unmarshal(raw, &pf); err != nil {
		return "", false, err
	}
	slug := strings.TrimSpace(pf.Slug)
	if slug == "" {
		slug = path.Base(dir)
	}
	if handled[slug] {
		return slug, false, nil
	}
	var exists bool
	if err := im.d.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM posts WHERE slug = $1)`, slug).Scan(&exists); err != nil {
		return "", false, err
	}
	if exists {
		return slug, false, nil
	}

	shots := map[string]string{}
	for _, s := range pf.Screenshots {
		shots[s.ID] = s.File
	}
	img := &images{im: im, dir: dir, files: shots, urls: map[string]string{}}
	in := blog.PostInput{Slug: slug, Title: pf.Title, Excerpt: pf.Excerpt, Category: pf.Category, Tags: pf.Tags,
		Status: "draft"}
	for i, p := range pf.Parts {
		name := p.File
		if name == "" {
			name = "part-" + strconv.Itoa(i+1) + ".md"
		}
		src, err := im.read(dir, name)
		if err != nil {
			return "", false, fmt.Errorf("part %d: %w", i+1, err)
		}
		md := rewriteScreenshots(string(src), func(id string) (string, bool) { return img.url(ctx, id) })
		in.Parts = append(in.Parts, blog.PartInput{Title: p.Title, ContentMD: md})
	}
	got, err := im.d.Blog.Import(ctx, []blog.PostInput{in})
	if err != nil {
		return "", false, err
	}
	return slug, len(got) == 1, nil
}

// read opens name relative to dir; it refuses paths that leave dir.
func (im *importer) read(dir, name string) ([]byte, error) {
	p := path.Join(dir, name)
	if !fs.ValidPath(p) || !strings.HasPrefix(p, dir+"/") {
		return nil, fmt.Errorf("%q is outside the post folder", name)
	}
	return fs.ReadFile(im.d.FS, p)
}

// images uploads a post's screenshots on first use.
type images struct {
	im    *importer
	dir   string
	files map[string]string // id -> file from post.json ("" = img/<id>.*)
	urls  map[string]string // id -> /media URL; "" = missing
}

func (s *images) url(ctx context.Context, id string) (string, bool) {
	if u, ok := s.urls[id]; ok {
		return u, u != ""
	}
	s.urls[id] = ""
	candidates := []string{"img/" + id + ".png", "img/" + id + ".jpg", "img/" + id + ".webp"}
	if f := s.files[id]; f != "" {
		candidates = []string{f, "img/" + f}
	}
	log := s.im.d.Log.With(slog.String("post", path.Base(s.dir)), slog.String("screenshot", id))
	for _, c := range candidates {
		data, err := s.im.read(s.dir, c)
		if err != nil {
			continue
		}
		m, err := s.im.d.Media.Save(ctx, path.Base(c), bytes.NewReader(data))
		if err != nil {
			log.Warn("seedimport: screenshot not saved, image dropped", logging.Err(err))
			return "", false
		}
		s.urls[id] = m.URL
		return m.URL, true
	}
	log.Warn("seedimport: screenshot missing, image dropped")
	return "", false
}

// profileImages uploads the seeded profile's local avatar/cover paths (files
// under seed/assets/) and points the profile at the /media copies. It does
// nothing once both are URLs.
func (im *importer) profileImages(ctx context.Context) error {
	var p map[string]any
	ok, err := settings.Get(ctx, im.d.DB, "profile", &p)
	if err != nil || !ok {
		return err
	}
	changed := false
	for _, k := range []string{"avatar_url", "cover_url"} {
		v, _ := p[k].(string)
		clean := path.Clean(v)
		if !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || strings.HasPrefix(clean, "/media/") ||
			strings.Contains(v, "..") {
			continue
		}
		f, err := im.d.FS.Open("assets" + clean)
		if err != nil {
			im.d.Log.Warn("seedimport: missing profile asset, left as is", slog.String("path", v))
			continue
		}
		m, err := im.d.Media.Save(ctx, path.Base(clean), f)
		_ = f.Close()
		if err != nil {
			im.d.Log.Warn("seedimport: profile asset not saved", slog.String("path", v), logging.Err(err))
			continue
		}
		p[k] = m.URL
		changed = true
	}
	if !changed {
		return nil
	}
	return settings.Put(ctx, im.d.DB, "profile", p)
}
