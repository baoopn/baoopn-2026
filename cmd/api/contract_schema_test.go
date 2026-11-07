package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// A shape checks one decoded JSON value (encoding/json into any) against a
// TypeScript type from web/src/lib/api/types.ts and returns the problems.
type shape func(path string, v any) []string

func typeErr(path string, want string, v any) []string {
	return []string{fmt.Sprintf("%s: want %s, got %T(%v)", path, want, v, v)}
}

var (
	str shape = func(p string, v any) []string {
		if _, ok := v.(string); !ok {
			return typeErr(p, "string", v)
		}
		return nil
	}
	num shape = func(p string, v any) []string {
		if _, ok := v.(float64); !ok {
			return typeErr(p, "number", v)
		}
		return nil
	}
	boolean shape = func(p string, v any) []string {
		if _, ok := v.(bool); !ok {
			return typeErr(p, "boolean", v)
		}
		return nil
	}
	// ts is an RFC 3339 timestamp string in UTC (types.ts header comment).
	ts shape = func(p string, v any) []string {
		s, ok := v.(string)
		if !ok {
			return typeErr(p, "RFC 3339 string", v)
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return []string{fmt.Sprintf("%s: %q is not RFC 3339", p, s)}
		}
		if _, off := t.Zone(); off != 0 {
			return []string{fmt.Sprintf("%s: %q is not UTC", p, s)}
		}
		return nil
	}
)

func null(s shape) shape {
	return func(p string, v any) []string {
		if v == nil {
			return nil
		}
		return s(p, v)
	}
}

func enum(vals ...string) shape {
	return func(p string, v any) []string {
		s, ok := v.(string)
		if !ok || !slices.Contains(vals, s) {
			return typeErr(p, "one of "+strings.Join(vals, "|"), v)
		}
		return nil
	}
}

func arr(el shape) shape {
	return func(p string, v any) []string {
		a, ok := v.([]any)
		if !ok {
			return typeErr(p, "array", v)
		}
		var out []string
		for i, x := range a {
			out = append(out, el(fmt.Sprintf("%s[%d]", p, i), x)...)
		}
		return out
	}
}

// record is Record<string, T>.
func record(el shape) shape {
	return func(p string, v any) []string {
		m, ok := v.(map[string]any)
		if !ok {
			return typeErr(p, "object", v)
		}
		var out []string
		for k, x := range m {
			out = append(out, el(p+"."+k, x)...)
		}
		return out
	}
}

// fields is an interface: a key ending in "?" is optional. The key set must
// match exactly.
type fields map[string]shape

func obj(f fields) shape {
	return func(p string, v any) []string {
		m, ok := v.(map[string]any)
		if !ok {
			return typeErr(p, "object", v)
		}
		var out []string
		known := map[string]bool{}
		for k, s := range f {
			name, optional := strings.CutSuffix(k, "?")
			known[name] = true
			x, present := m[name]
			if !present {
				if !optional {
					out = append(out, fmt.Sprintf("%s: missing key %q", p, name))
				}
				continue
			}
			out = append(out, s(p+"."+name, x)...)
		}
		var extra []string
		for k := range m {
			if !known[k] {
				extra = append(extra, k)
			}
		}
		sort.Strings(extra)
		for _, k := range extra {
			out = append(out, fmt.Sprintf("%s: unexpected key %q", p, k))
		}
		return out
	}
}

// partial is Partial<T> for an obj shape's fields.
func partial(f fields) shape {
	g := fields{}
	for k, s := range f {
		g[strings.TrimSuffix(k, "?")+"?"] = s
	}
	return obj(g)
}

// ── Shapes from types.ts ─────────────────────────────────────────────────────

var (
	pageMeta = obj(fields{"total": num, "page": num, "limit": num})
	apiError = obj(fields{"code": str, "message": str})

	socialLink     = obj(fields{"label": str, "url": str, "icon": str, "handle?": str})
	skillGroup     = obj(fields{"group": str, "items": arr(str)})
	experienceItem = obj(fields{"role": str, "org": str, "url?": str, "location?": str, "start": str,
		"end?": str, "summary_md?": str, "summary_html?": str})
	profileFields = fields{
		"name": str, "headline": str, "location": str, "pronouns?": str, "email": str,
		"avatar_url": str, "cover_url": str, "resume_url": str, "available_for_work": boolean,
		"bio_md": str, "bio_html": str, "socials": arr(socialLink), "skills": arr(skillGroup),
		"experience":  arr(experienceItem),
		"seo":         obj(fields{"title": str, "description": str, "og_image": str}),
		"updated_at?": ts,
	}
	profileShape = obj(profileFields)

	projectSummaryFields = fields{
		"slug": str, "title": str, "summary": str, "tech": arr(str), "period": str, "cover_url": str,
		"logo_url": str, "logo_bg": enum("tile", "ink"), "demo_url": str, "repo_url": str, "language": str, "stars": num, "forks": num,
		"archived": boolean, "featured": boolean, "pushed_at": null(ts), "post_slug": str,
	}
	projectSummary = obj(projectSummaryFields)
	projectDetail  = obj(extend(projectSummaryFields, fields{
		"body_html": str, "body_source": enum("case_study", "readme", "none"),
		"languages": record(num), "topics": arr(str), "created_at": null(ts),
	}))
	adminProject = obj(fields{
		"id": num, "slug": str, "github_id": null(num), "repo_full_name": str, "repo_url": str,
		"gh_homepage": str, "gh_description": str, "gh_language": str, "gh_languages": record(num),
		"gh_topics": arr(str), "gh_stars": num, "gh_forks": num, "gh_archived": boolean,
		"gh_fork": boolean, "gh_private": boolean, "gh_pushed_at": null(ts), "synced_at": null(ts),
		"title": str, "summary": str, "body_md": str, "tech": arr(str), "period": str,
		"cover_url": str, "logo_url": str, "logo_bg": enum("tile", "ink"), "demo_url": str, "post_slug": str,
		"visible": boolean, "featured": boolean, "sort_order": num, "views_30d": num, "created_at": ts, "updated_at": ts,
	})
	syncResult = obj(fields{"added": num, "updated": num, "total": num, "synced_at": ts})

	tocItem           = obj(fields{"id": str, "text": str, "level": num})
	postSummaryFields = fields{
		"slug": str, "title": str, "excerpt": str, "cover_url": str, "category": enum("tech", "life"), "tags": arr(str),
		"part_count": num, "published_at": ts, "updated_at": ts, "reading_minutes": num, "views": num,
	}
	postSummary = obj(postSummaryFields)
	partRef     = obj(fields{"no": num, "title": str})
	postPart    = obj(fields{"no": num, "title": str, "content_html": str, "toc": arr(tocItem), "reading_minutes": num})
	postDetail  = obj(extend(postSummaryFields, fields{
		"parts": arr(partRef), "part": postPart, "prev": null(partRef), "next": null(partRef),
	}))
	adminPostPart = obj(fields{"no": num, "title": str, "content_md": str, "reading_minutes": num})
	adminPost     = obj(fields{
		"id": num, "slug": str, "title": str, "excerpt": str, "cover_url": str, "category": enum("tech", "life"),
		"tags": arr(str), "status": enum("draft", "published"), "state": enum("draft", "scheduled", "published"),
		"published_at": null(ts), "reading_minutes": num, "views": num, "parts": arr(adminPostPart),
		"created_at": ts, "updated_at": ts,
	})
	previewResult = obj(fields{"html": str, "toc": arr(tocItem), "reading_minutes": num})
	tagCount      = obj(fields{"tag": str, "count": num})

	mediaShape = obj(fields{"id": num, "url": str, "filename": str, "original": str,
		"content_type": str, "size_bytes": num, "status": enum("ready", "processing", "failed"), "error": str,
		"converted": boolean, "created_at": ts})

	direction     = enum("inbound", "outbound")
	threadSummary = obj(fields{
		"id": num, "name": str, "email": str, "subject": str, "status": enum("open", "archived", "spam"),
		"unread": boolean, "last_message_at": ts, "last_direction": direction, "last_preview": str,
		"message_count": num, "created_at": ts,
	})
	threadMessage = obj(fields{
		"id": num, "direction": direction, "source": enum("form", "email", "admin"), "body": str,
		"full_body": str, "status": enum("received", "sending", "sent", "failed"), "error": str,
		"from_address": str, "other_sender": boolean, "attachments_dropped": num, "created_at": ts,
		"sent_at": null(ts),
	})
	threadDetail = obj(fields{"thread": threadSummary, "messages": arr(threadMessage), "reply_enabled": boolean})

	breakdown       = obj(fields{"key": str, "views": num, "visitors?": num, "avg_visible_ms?": num})
	analyticsReport = obj(fields{
		"range": enum("24h", "7d", "30d", "90d"), "bucket": enum("hour", "day"), "time_zone": str,
		"totals": obj(fields{"views": num, "visitors": num, "avg_visible_ms": num, "live_now": num,
			"views_prev": num, "visitors_prev": num}),
		"timeseries": arr(obj(fields{"t": ts, "views": num, "visitors": num})),
		"paths":      arr(breakdown), "referrers": arr(breakdown), "devices": arr(breakdown),
		"browsers": arr(breakdown), "countries": arr(breakdown),
	})

	adminConfigShape = obj(fields{"clerk_publishable_key": str, "version": str, "mail_enabled": boolean,
		"inbound_enabled": boolean, "github_user": str})
	adminUser     = obj(fields{"id": str, "email": str, "name": str, "image_url": str, "role?": str})
	overviewShape = obj(fields{
		"unread_threads": num, "open_threads": num,
		"posts":    obj(fields{"published": num, "drafts": num, "scheduled": num}),
		"projects": obj(fields{"visible": num, "total": num}),
		"views_7d": num, "visitors_7d": num, "live_now": num, "last_sync_at": null(ts),
		"recent_threads": arr(threadSummary), "top_paths_7d": arr(breakdown),
	})
	siteSettings = obj(fields{"notify_email": str, "contact_enabled": boolean, "auto_reply_enabled": boolean,
		"auto_reply_text": str, "signature": str})

	spotifyArtist = obj(fields{"id": str, "name": str, "external_url?": str, "image_url?": str})
	spotifyTrack  = obj(fields{
		"id": str, "name": str, "duration_ms": num, "explicit": boolean, "external_url?": str,
		"album?":  obj(fields{"id": str, "name": str, "cover_image_url?": str, "external_url?": str}),
		"artists": arr(spotifyArtist),
	})
	nowPlaying = obj(fields{"is_playing": boolean, "progress_ms": null(num), "fetched_at": ts,
		"track": null(spotifyTrack)})
	recentPlay = obj(fields{"id": str, "played_at": ts, "track": spotifyTrack})
	topArtist  = obj(fields{"artist": spotifyArtist, "plays": num, "last_played": ts})

	playlistTrack = obj(fields{"position": num, "plays": num, "track": spotifyTrack})
	topPlaylist   = obj(fields{
		"period": enum("week", "month", "year"), "period_start": ts, "period_end": ts, "time_zone": str,
		"label": str, "name": str, "spotify_id": str, "spotify_url": str, "track_count": num,
		"total_plays": num, "published_at": ts, "tracks": arr(playlistTrack),
	})
	topPlaylists = obj(fields{"time_zone": str,
		"periods": obj(fields{"week": arr(topPlaylist), "month": arr(topPlaylist), "year": arr(topPlaylist)})})

	okShape     = obj(fields{"ok": boolean})
	countShape  = obj(fields{"count": num})
	sentToShape = obj(fields{"sent_to": str})
)

func extend(base, more fields) fields {
	out := fields{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range more {
		out[k] = v
	}
	return out
}

// TestShapeChecker guards the checker itself: a contract test that cannot fail
// proves nothing.
func TestShapeChecker(t *testing.T) {
	s := obj(fields{"a": str, "b?": num, "c": null(ts), "d": arr(enum("x", "y"))})
	good := map[string]any{"a": "s", "c": nil, "d": []any{"x"}}
	if p := s("$", good); len(p) != 0 {
		t.Fatalf("good value flagged: %v", p)
	}
	for name, v := range map[string]map[string]any{
		"missing":  {"c": nil, "d": []any{}},
		"extra":    {"a": "s", "c": nil, "d": []any{}, "z": 1},
		"type":     {"a": 1.0, "c": nil, "d": []any{}},
		"not utc":  {"a": "s", "c": "2026-11-07T10:00:00+07:00", "d": []any{}},
		"bad enum": {"a": "s", "c": nil, "d": []any{"q"}},
		"nullness": {"a": nil, "c": nil, "d": []any{}},
	} {
		if p := s("$", v); len(p) == 0 {
			t.Errorf("%s not flagged", name)
		}
	}
}
