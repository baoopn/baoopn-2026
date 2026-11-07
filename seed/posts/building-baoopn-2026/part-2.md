The admin dashboard lives at `/admin`. It runs in the browser, signs in with Clerk, and calls the Go API with a session token. From there I write posts, manage projects, upload images, read the inbox, and check analytics. This part covers three pieces of it: how the API decides who is an admin, the Markdown editor, and the GitHub sync.

## Who is an admin

Clerk handles sign-in. The browser gets a short-lived session token (a signed JWT) and sends it as `Authorization: Bearer <token>`. The Go API checks it with Clerk's SDK.

A valid token proves who you are. It does not prove you are me. So after the signature check, the API asks a second question: is this user on the allow-list? There are three ways in:

1. The user ID is in `ADMIN_ALLOWED_USER_IDS`.
2. The user's role is `admin`. The role comes from Clerk's `public_metadata`, which users cannot edit.
3. One of the user's **verified** emails is in `ADMIN_ALLOWED_EMAILS`.

The token check also pins the authorized party (the `azp` claim) to the site's own origin. In development, it also accepts tokens minted on localhost.

To learn the user's emails, the API fetches the user from Clerk's Backend API and caches the result for five minutes. It reads the email list and keeps an address only if its status is `verified`. Banned or locked users come back with no email and no role, so no rule can match them.

### No fallback to the email claim

The session token can carry an `email` claim too. It would be faster to trust it and skip the lookup. I chose not to. That claim is the primary address, and nothing in it says whether the address was verified. If an account could hold my address without confirming it, and the API trusted the claim, that account would be an admin.

So the rule is strict. If Clerk says the user has no verified email, and no role or ID grants access, the answer is 403. The token's email claim plays no part.

There is one fallback, and it is for the role. If Clerk's API is down, the role claim in a verified token still counts, since the role comes from metadata the user cannot touch. If there is no role either, the API returns 502 and says it could not check the user.

```go
u, err := a.lookup(r.Context(), claims.Subject)
switch {
case err == nil:
	if tc != nil && tc.Role != "" {
		u.Role = tc.Role
	}
case errors.Is(err, errNoVerifiedEmail):
	httpapi.Error(w, http.StatusForbidden, "forbidden", "this account has no verified email")
	return
case tc != nil && tc.Role != "":
	// Clerk's Backend API is unreachable. The verified token's role
	// claim (from public_metadata, which users cannot edit) still
	// authorizes; its email claim is not trusted (may be unverified).
	u = User{ID: claims.Subject, Role: tc.Role}
default:
	httpapi.Error(w, http.StatusBadGateway, "auth_unavailable", "could not verify the user with Clerk")
	return
}
if !a.allowed(u) {
	httpapi.Error(w, http.StatusForbidden, "forbidden", "this account is not an admin")
	return
}
```

The dashboard also calls `GET /api/admin/config` without auth. It returns the Clerk publishable key and the build version, plus flags for whether mail and inbound mail are set up. The web app does not need its own copy of the Clerk key.

## The Markdown editor

I write posts in Markdown, so the editor is CodeMirror 6 with the Markdown language and code-block highlighting for many languages. Its theme reads the site's CSS variables, so light and dark mode apply to the editor with no extra setup.

![The admin post editor in split mode, Markdown on the left and the rendered preview on the right](screenshot:admin-editor-split)

The editor has three modes: write, split, and preview. A toolbar wraps text in bold or code, adds headings, links, tables and code blocks. To add an image I paste it or drop it in. The editor puts a placeholder like `![Uploading photo…](#upload-3)` at the cursor, uploads the file, and swaps the placeholder for the real URL when the upload ends.

### Preview from the server

The preview is not a JavaScript Markdown library. The editor sends the text to `POST /api/admin/posts/preview`, and the Go API renders it with the same code that renders the published post. What I see in the preview is what readers get.

Two details keep this smooth. The editor waits 400 ms after the last keystroke before it asks. And each request has a sequence number, so a slow old answer cannot replace a newer one:

```ts
async function render(md: string) {
	const seq = ++previewSeq;
	previewBusy = true;
	try {
		const r = await post<PreviewResult>('/posts/preview', { markdown: md });
		if (seq !== previewSeq) return;
		// ...
	} catch (err) {
		if (seq === previewSeq) previewError = errorMessage(err);
	} finally {
		if (seq === previewSeq) previewBusy = false;
	}
}
```

On the Go side, the renderer is goldmark with GitHub-flavored Markdown, footnotes, and typographic quotes. Code blocks go through chroma, which emits CSS classes instead of inline colors, so the site's stylesheet controls the theme. Headings get anchor IDs. While it walks the document, the renderer collects H2 to H4 into a table of contents and works out reading time.

```go
highlighting.NewHighlighting(
	highlighting.WithFormatOptions(chromahtml.WithClasses(true), chromahtml.TabWidth(4)),
	highlighting.WithGuessLanguage(false),
),
```

There are two renderers. Posts I write are trusted and may contain raw HTML. GitHub READMEs are not trusted, so raw HTML is not passed through. For READMEs, the renderer also rewrites relative links and images to point at the repo on GitHub, so a README's `docs/diagram.png` still loads.

Uploaded files go through a content sniff, not the file name. The API accepts common image types and PDF, and it stores each file under a random name. It also hashes the file with SHA-256. If the same bytes were uploaded before, it returns the existing record instead of a second copy.

## Projects from GitHub

The projects page comes from my GitHub repos. A syncer pulls them every six hours by default, and I can start a sync from the dashboard.

![The admin projects list with synced repositories and their visibility toggles](screenshot:admin-projects)

A few rules shape it:

- **New repos arrive hidden.** Nothing shows on the site until I turn it on.
- **My edits survive.** On update, the sync writes the GitHub columns (description, stars, language, topics and so on) and leaves my own fields alone: title, summary, case study, tech list, cover, order and visibility. Where I set a summary or a demo link, it wins over the GitHub value.
- **Private repos are skipped**, and so are repos that disappear from the list. The sync logs how many and leaves them as they are.

The repo list uses an ETag. The syncer sends `If-None-Match`, and a `304 Not Modified` means nothing changed, so there is nothing to write.

### Spending the rate limit with care

The list is cheap. The details are not: each project needs a README call and a languages call. So the syncer fetches details only for visible projects whose last push time changed since the last fetch. It does at most 20 per run.

The GitHub client reads `X-RateLimit-Remaining` from every response. Before each detail fetch, the syncer checks it. If fewer than three calls are left, it stops and leaves the rest for the next run.

```go
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
	// ...
}
```

When I make a project visible, the dashboard does not wait for the next run. It "kicks" the syncer with that project's ID on a small buffered channel. If the channel is full, the kick is dropped, and the next run picks it up.

One more guard: the client follows `Link: rel="next"` for paging, but only to URLs on the GitHub API host. A bad `Link` header cannot send my token somewhere else.

Part 3 follows a message from the contact form, out by email, and back in as a reply.
