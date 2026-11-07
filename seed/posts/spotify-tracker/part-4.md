The backend collects the data. Two SvelteKit apps show it. The public page at `/` shows what I am playing now and what I played before. The admin app at `/admin` is where I check health, browse the data and change settings.

Both use Svelte 5, Tailwind and TanStack Query, and both build to static files with `adapter-static`. The Go API embeds the builds and serves them, so there is no Node server in production.

## The "liner notes" design

Before I wrote any UI, I wrote a short design doc. I did not want the page to look like a generic SaaS dashboard. The metaphor I picked was a record sleeve, or a zine printed on a risograph:

- **Paper and ink.** The background is soft pastel paper. Text and rules are a soft aubergine ink, not black or navy.
- **Flat spot colours.** Peach, mint, lilac, butter, sky and rose, used as fills only, like spot inks in print. Peach marks "now playing", mint means "enriched", butter means "pending", rose means an error.
- **No gradients at all.** No blurred album-art backdrops, no gradient text, no shimmer on loading skeletons.
- **Print offsets, not shadows.** Depth comes from a solid `2px 2px 0` offset in ink colour on buttons and the now-playing card. Pressing a button removes the offset.
- **Credits typography.** Outfit for big titles, Inter for body text, and JetBrains Mono with tabular numbers for labels and counts, the way liner notes list credits.

The doc also lists things to avoid, such as glassmorphism, neon, and icons in coloured circles. When I was unsure about a detail, the doc had the answer.

Text on a spot fill is always the dark ink, in light and dark mode.

To keep that rule honest, the admin build runs a contrast check. A small Node script reads the colour tokens from the CSS file, works out every text and background pair across all palettes and both modes, and exits with an error if any pair drops below WCAG 4.5:1. It runs as part of `pnpm check`, so CI catches a bad colour before it ships.

## The public page

![The recently played list, set as a numbered tracklist, with the recent playlists shown as square sleeves below](screenshot:public-recent)

The page has four blocks: the now-playing hero, the song stats from Part 3, a numbered "Recently played" list, and recent playlists.

The now-playing data comes from the SSE stream in Part 2. A small class in `live.svelte.ts` owns the connection and writes each event into the TanStack Query cache. The rest of the page reads from the cache and does not care where the data came from.

The connection has four states: `connecting`, `live`, `reconnecting` and `polling`. I took over reconnects from the browser, because `EventSource` gives up on some HTTP errors and retries too fast on others:

```ts
#scheduleReconnect(): void {
	const base = Math.min(BACKOFF_MAX_MS, BACKOFF_MIN_MS * 2 ** this.#attempt);
	this.#attempt++;
	const delay = base / 2 + Math.random() * (base / 2);
	this.retryAt = Date.now() + delay;
	this.#later(() => {
		if (this.#running && !this.#source) this.#connect();
	}, delay);
}
```

The wait doubles from 1 second up to 30, with jitter. While the stream is down, the page polls over HTTP, and the status says so. If the stream stays silent for 20 seconds (it should send about every 3), the page treats it as dead. That catches proxies that keep a connection open but pass nothing through.

Two smaller details:

- **Clock skew.** Progress bars and "3 min ago" labels depend on the client clock. The page keeps the smallest gap it has seen between "I got this" and the server's `fetched_at`, and uses it to correct the time. A wrong system clock no longer breaks the progress line.
- **Track changes.** When the track ID changes, the page refreshes history, playlists and stats. It does it again 4 seconds later, since the API records the play around the moment of the change.

### Page analytics without cookies

I wanted to know if anyone visits, without tracking them. The page sends one beacon per view, and one more with the visible time when the tab is hidden. There are no cookies, no local storage and no IDs. Automated browsers (`navigator.webdriver`) send nothing.

On the server, unique visitors come from a salted hash of IP and user agent. The salt changes every UTC day, and the hashes go into a Redis HyperLogLog, a structure that counts distinct items without storing them. Rows keep only coarse data: device class from the viewport width, browser family, country code, and the referring host.

## The public API and caching

The page uses the same public REST API anyone can call: now playing, history, track detail, and stats over ranges from 7 days to all time.

Stats queries are heavier, so the API caches responses in Redis for up to 5 minutes. The hard part of caching is knowing when to drop it. I did not want to track which keys a new play affects. Instead, every cache key includes a "generation" number:

```go
// The generation changes whenever listening data changes (see
// redisx.BumpCache), so a cached response is never older than the data.
gen, _ := h.rdb.Get(r.Context(), redisx.KeyCacheGeneration).Result()
sum := sha256.Sum256([]byte(gen + "|" + r.URL.Path + "?" + r.URL.Query().Encode()))
key := redisx.KeyCachePrefix + hex.EncodeToString(sum[:16])
```

A new play, a replay, or an enriched track runs one `INCR` on the generation. Every old key turns into a miss at once, and the old entries expire on their own TTL. Browsers get `Cache-Control: no-cache`, so they always check back with the server.

## The admin dashboard

The admin app is a single-page app with a left rail and these sections:

- **Overview:** now playing, totals, enrichment progress, Spotify token status, and the health of each service from the heartbeats.
- **Data:** history, tracks, artists and playlists, with search, sort and filters. Tracks show their enrichment status and can be re-queued.
- **Analytics:** listening insights (top tracks, artists, genres, audio profile), public API traffic, and page visitors.
- **Audit:** every sensitive admin action.
- **Settings:** Spotify connection, email alerts, and appearance (mode and palette).

The charts are hand-drawn SVG components, not a chart library, so they match the flat, ruled style. The API traffic view shows request counts, error rate and p50/p95/p99 latency, worked out from hourly histograms in Postgres.

### Clerk sign-in

I use Clerk for sign-in. The SPA does not bake the publishable key into the build. On load it fetches `/admin/api/config`, starts Clerk with that key, and then asks `/admin/api/auth/me` whether this user is an admin.

Every admin API call sends the Clerk session JWT as a bearer token. The API checks it against Clerk's keys, checks that the token was minted for this origin, and then checks the user:

```go
func (a *Authenticator) allowed(u User) bool {
	if slices.Contains(a.allowedUserIDs, u.ID) {
		return true
	}
	if a.adminRole != "" && u.Role == a.adminRole {
		return true
	}
	return u.Email != "" && slices.Contains(a.allowedEmails, strings.ToLower(u.Email))
}
```

A user passes by ID, by a role in Clerk's public metadata (which users cannot edit), or by a verified email on the allow-list. A bad token gets a 401 and the sign-in screen. A valid user who is not an admin gets a 403. If Clerk's backend is down, the API can still authorize from the claims in the verified token.

The admin page is served with a strict Content Security Policy. At startup the API hashes the inline scripts in `index.html` and adds the Clerk host, which it works out from the publishable key.

Part 5 covers how all of this runs in production.
