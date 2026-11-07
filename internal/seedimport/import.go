// Package seedimport imports the embedded v3 blog series (and their images)
// on first start, once, guarded by the "seed.blog_v3" settings flag.
package seedimport

import (
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

// FlagKey marks the import as done.
const FlagKey = "seed.blog_v3"

// Deps are the importer's dependencies.
type Deps struct {
	DB    *pgxpool.Pool
	Log   *slog.Logger
	Media *media.Store
	Blog  *blog.Module
	FS    fs.FS // seed.FS: blog/<series>/{metadata.json,page-N.md}, assets/<path>
}

type metadata struct {
	Name  string   `json:"name"`
	Slug  string   `json:"slug"`
	Date  string   `json:"date"`
	Tags  []string `json:"tags"`
	Cover string   `json:"cover"`
	Pages []struct {
		Title string `json:"title"`
		Slug  string `json:"slug"`
		No    int    `json:"no"`
	} `json:"pages"`
}

// Run imports the seed blog unless the flag is already set.
func Run(ctx context.Context, d Deps) error {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.DB == nil || d.Media == nil || d.Blog == nil || d.FS == nil {
		return errors.New("seedimport: missing dependency")
	}
	done, err := settings.Flag(ctx, d.DB, FlagKey)
	if err != nil {
		return fmt.Errorf("seedimport: read flag: %w", err)
	}
	if done {
		return nil
	}
	im := &importer{d: d, mapped: map[string]string{}}
	posts, err := im.buildPosts(ctx)
	if err != nil {
		return err
	}
	n, err := d.Blog.Import(ctx, posts)
	if err != nil {
		return fmt.Errorf("seedimport: %w", err)
	}
	if err := im.profileImages(ctx); err != nil {
		d.Log.Warn("seedimport: profile images", logging.Err(err))
	}
	if err := settings.SetFlag(ctx, d.DB, FlagKey); err != nil {
		return fmt.Errorf("seedimport: set flag: %w", err)
	}
	d.Log.Info("seedimport: blog imported", slog.Int("posts", len(posts)), slog.Int("inserted", n),
		slog.Int("images", len(im.mapped)))
	return nil
}

type importer struct {
	d      Deps
	mapped map[string]string // local path -> /media/<file>; "" = missing
}

// resolve uploads assets/<p> once and returns its /media URL.
func (im *importer) resolve(ctx context.Context, p string) (string, bool) {
	if to, ok := im.mapped[p]; ok {
		return to, to != ""
	}
	clean := path.Clean(p)
	if !strings.HasPrefix(clean, "/") || strings.Contains(clean, "..") || strings.HasPrefix(clean, "/media/") {
		im.mapped[p] = ""
		return "", false
	}
	f, err := im.d.FS.Open("assets" + clean)
	if err != nil {
		im.d.Log.Warn("seedimport: missing asset, left as is", slog.String("path", p))
		im.mapped[p] = ""
		return "", false
	}
	defer func() { _ = f.Close() }()
	m, err := im.d.Media.Save(ctx, path.Base(clean), f)
	if err != nil {
		im.d.Log.Warn("seedimport: asset not saved, left as is", slog.String("path", p), logging.Err(err))
		im.mapped[p] = ""
		return "", false
	}
	im.mapped[p] = m.URL
	return m.URL, true
}

func (im *importer) buildPosts(ctx context.Context) ([]blog.ImportPost, error) {
	metas, err := fs.Glob(im.d.FS, "blog/*/metadata.json")
	if err != nil {
		return nil, fmt.Errorf("seedimport: glob: %w", err)
	}
	sort.Strings(metas)
	var posts []blog.ImportPost
	for _, mp := range metas {
		raw, err := fs.ReadFile(im.d.FS, mp)
		if err != nil {
			return nil, fmt.Errorf("seedimport: %s: %w", mp, err)
		}
		var meta metadata
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("seedimport: %s: %w", mp, err)
		}
		if meta.Slug == "" {
			meta.Slug = path.Base(path.Dir(mp))
		}
		cover := meta.Cover
		if cover != "" {
			if to, ok := im.resolve(ctx, cover); ok {
				cover = to
			}
		}
		for _, pg := range meta.Pages {
			file := path.Join(path.Dir(mp), "page-"+strconv.Itoa(pg.No)+".md")
			src, err := fs.ReadFile(im.d.FS, file)
			if err != nil {
				return nil, fmt.Errorf("seedimport: %s: %w", file, err)
			}
			at, err := publishedAt(meta.Date, pg.No)
			if err != nil {
				return nil, fmt.Errorf("seedimport: %s date: %w", mp, err)
			}
			md := rewriteImages(stripPartHeading(string(src)), func(p string) (string, bool) { return im.resolve(ctx, p) })
			posts = append(posts, blog.ImportPost{
				PostInput: blog.PostInput{
					Slug: postSlug(meta.Slug, pg.Slug), Title: pg.Title, ContentMD: md, CoverURL: cover,
					Tags: meta.Tags, Series: meta.Slug, SeriesTitle: meta.Name, SeriesOrder: pg.No,
					Status: "published", PublishedAt: &at,
				},
				LegacyPath: legacyPath(meta.Slug, pg.No),
			})
		}
	}
	return posts, nil
}

// profileImages uploads the seeded profile's local avatar/cover paths (v3
// served them from public/) and points the profile at the /media copies.
func (im *importer) profileImages(ctx context.Context) error {
	var p map[string]any
	ok, err := settings.Get(ctx, im.d.DB, "profile", &p)
	if err != nil || !ok {
		return err
	}
	changed := false
	for _, k := range []string{"avatar_url", "cover_url"} {
		v, _ := p[k].(string)
		if !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "/media/") || strings.HasPrefix(v, "//") {
			continue
		}
		if to, ok := im.resolve(ctx, v); ok {
			p[k] = to
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return settings.Put(ctx, im.d.DB, "profile", p)
}
