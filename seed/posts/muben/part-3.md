The admin is where the brand team works. It had to feel like a writing tool, not a database form. It also had to handle two languages for every field, image uploads from phones, and a strict rule on who can sign in.

## The content model

The schema is small. Three tables hold content:

- `site_content`: one row per editable section, keyed by name (`about`, `home`). About uses the bilingual title and body. Home uses one hero image URL.
- `blog_posts`: the journal.
- `contact_messages`: form submissions. These are user content, so they are not split by language.

Every text field in `blog_posts` comes in a pair:

```sql
CREATE TABLE blog_posts (
    id                 INTEGER PRIMARY KEY,
    slug               TEXT NOT NULL UNIQUE,
    status             TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    title_en           TEXT NOT NULL DEFAULT '',
    title_vi           TEXT NOT NULL DEFAULT '',
    content_en         TEXT NOT NULL DEFAULT '{}',
    content_vi         TEXT NOT NULL DEFAULT '{}',
    content_html_en    TEXT NOT NULL DEFAULT '',
    content_html_vi    TEXT NOT NULL DEFAULT '',
    -- ... excerpt, category, SEO title and description, each _en and _vi
    published_at       TEXT,
    -- ...
);

CREATE INDEX idx_blog_posts_published
    ON blog_posts (published_at DESC)
    WHERE status = 'published';
```

I chose columns over a separate translations table. With two fixed languages, a pair of columns is easy to query and easy to read. A third language would mean a migration, and I accept that trade.

The slug is shared across languages. `/en/blogs/x` and `/vi/blogs/x` are the same post. The partial index covers published posts, which is the one query the public site runs.

### IDs

Post IDs are Snowflake IDs: 64-bit, time-sorted numbers made in the app. They fit in a SQLite `INTEGER` column. In JSON they travel as strings, because JavaScript numbers lose precision past 53 bits.

## The editor

The editor uses Tiptap, which is built on ProseMirror. Editors get a toolbar for headings, lists, links, quotes, code and images. They never see Markdown.

Tiptap stores a document as JSON, and the API keeps that JSON as the source. HTML is a build product. When an editor clicks Publish, the Go API renders the JSON to HTML and stores it in `content_html_en` and `content_html_vi`. The public site serves that stored HTML.

I wrote the renderer in Go instead of trusting HTML from the browser. It walks the node tree, escapes all text, and allows a fixed set of nodes and marks. Links and image sources go through a scheme check:

```go
func safeURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || strings.ContainsAny(s, "\x00\r\n\t") {
		return ""
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "#") ||
		strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return s
	}
	lower := strings.ToLower(s)
	for _, scheme := range []string{"http://", "https://", "mailto:", "tel:"} {
		if strings.HasPrefix(lower, scheme) {
			return s
		}
	}
	// ...
	return s
}
```

A `javascript:` link loses its anchor and keeps its text. Unknown node types render their children, so content is never dropped without a trace. The editor's link dialog runs the same scheme rules, so the editor sees the error before the server does.

One bug taught me to care about the edit path, not just publish. Saving a published post updated the JSON but not the stored HTML. The public page kept serving the old body. Now an update to a published post re-renders both languages right away.

### The form

The blog form has an EN/VI toggle at the top. A `BiField` component binds to both the `en` and `vi` values and shows the input for the active language. One Tiptap editor is mounted at a time, for the active language.

Next to the form sits a live preview. It uses the same `Prose` component as the public post page, so what the editor sees is what readers get. On narrow screens the preview folds away so the form stays usable.

## Images

Image uploads take three steps:

1. The browser shrinks the image. Anything over 2000 px on its long edge is scaled down and re-encoded to WebP at 0.85 quality. Small files pass through.
2. The browser asks the API for a presigned PUT URL.
3. The browser uploads the file straight to the bucket.

Step 1 means the file in storage is already web-sized, so I need no image CDN or resize service. iPhone photos arrive as HEIC, which no browser can draw to a canvas. For those, the admin loads a WASM HEIC decoder on demand, then converts to WebP. EXIF orientation is applied so phone photos are not sideways.

The API decides where files go. Keys follow `media/blog/<postId>/<snowflake>.webp` or `media/hero/...`. The API signs uploads for raster types and nothing else. SVG is blocked because it can carry scripts. Delete requests must match that key pattern and cannot contain `..` segments.

Each post has its own media folder, which makes cleanup simple. Deleting a post deletes its folder. A "sweep" button lists every folder under `media/blog/`, compares the IDs to the posts table, and removes folders with no post. If either list fails, the sweep stops and deletes nothing.

## Who can sign in

The admin is a client-side app. Its layout sets `ssr = false`, so there is nothing for crawlers and the token never touches the server render.

I built a custom sign-in screen on Clerk's headless SDK instead of using Clerk's ready-made components. That kept the look on brand. The flow supports password, email codes and second factors like an authenticator app, and hides any method the screen cannot finish.

On the API, a valid Clerk token is not enough. Clerk proves the caller has an account. It does not prove they should run the CMS. So the middleware looks up the account's email and checks it against an allowlist from config. An empty list denies everyone. If Clerk cannot be reached, the API returns 503 instead of guessing.

```go
email, err := lookupEmail(c.UserContext(), claims.Subject)
if err != nil {
	if errors.Is(err, errNoEmail) {
		return httpx.Error(c, fiber.StatusForbidden, "forbidden",
			"this account is not permitted to administer this site")
	}
	// Clerk unreachable or erroring. Deny rather than guess.
	return httpx.Error(c, fiber.StatusServiceUnavailable, "upstream_error",
		"could not verify administrator access, try again")
}
```

The email lookup is cached in memory for five minutes per user, so a burst of admin clicks is not a burst of Clerk calls.

## Small pieces that help

- **Contact inbox.** Messages show newest first with an unread badge in the sidebar. The public form has a hidden honeypot field. A bot that fills it gets a 201 and nothing is stored.
- **SMTP settings.** The team can set up email alerts in the admin. The password is write-only. The API encrypts it with AES-256-GCM before it hits the database, and the key lives in a deploy secret, never in the database. If the admin has no SMTP set up, the API falls back to SMTP from its environment.
- **Storage check.** The dashboard has a button that tests the bucket connection, which saves a lot of guessing when credentials change.
