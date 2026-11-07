The poller is the smallest service, but it decides what ends up in my history. If it gets a rule wrong, every chart downstream is wrong too. This part covers three things: tokens, play detection, and how a snapshot reaches the browser.

## The poll loop

Spotify has no push API for "the user started a new song". So the poller asks `GET /me/player/currently-playing` every 3 seconds (`POLL_INTERVAL_MS`, default 3000). Each answer is a snapshot: the track, the progress in milliseconds, whether it is playing, and the context (an album or playlist).

The loop is a timer with a `wait` that changes based on what went wrong:

```go
detail, err := p.cycle(ctx)
wait = p.interval
var rl *spotify.RateLimitError
switch {
case err == nil:
	consecutiveErrors = 0
	p.hb.OK(ctx, detail)
	continue
case errors.As(err, &rl):
	wait = max(rl.RetryAfter, p.interval)
case errors.Is(err, grpcapi.ErrSpotifyAuth):
	// Needs operator action (re-authorize in the dashboard); poll
	// slowly until the token works again.
	wait = 30 * time.Second
default:
	consecutiveErrors++
	// Exponential backoff on repeated failures, capped at 1 minute.
	wait = min(p.interval*time.Duration(1<<min(consecutiveErrors-1, 5)), time.Minute)
}
p.hb.Fail(ctx, err)
```

I split errors into three kinds, because each needs a different response:

- **Rate limited (429).** Spotify counts quotas per developer account and sends `Retry-After`. The poller waits that long, and never less than one interval.
- **Auth failure.** Retrying fast does not help. A human has to re-authorize. The poller drops to one try every 30 seconds.
- **Anything else.** Network errors, a restarting API. The wait doubles on each failure in a row, up to one minute.

Each cycle also writes a heartbeat to Redis, success or failure. The admin dashboard reads these to show service health.

## Tokens without secrets

The poller holds no Spotify secrets. It asks the API for an access token over gRPC. The API owns the refresh token, stored AES-256-GCM encrypted in the `settings` table.

On the API side, `spotify.Manager` caches the access token in Redis until a minute before it expires. A mutex and a second cache check stop two callers from refreshing at once:

```go
func (m *Manager) AccessToken(ctx context.Context, force bool) (string, time.Time, error) {
	if !force {
		if tok, exp, ok := m.cached(ctx); ok {
			return tok, exp, nil
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Another caller may have refreshed while we waited for the lock.
	if !force {
		if tok, exp, ok := m.cached(ctx); ok {
			return tok, exp, nil
		}
	}
	// ... refresh with Spotify, save state, cache the new token
}
```

Two details matter here. First, if Spotify rotates the refresh token on refresh, the manager saves the new one. Second, if Spotify returns `invalid_grant`, the manager calls a hook that sends an alert email.

On the poller side, a 401 from Spotify triggers one retry with `force=true`. That asks the API for a fresh token, even if the cached one looks valid. If the second try also gets a 401, the cycle fails.

Since early 2026, Spotify refresh tokens expire 180 days after you authorize. Refreshing does not extend that. So I added a dashboard flow to re-authorize, and alerts before the deadline. I cover those in Part 5.

## Is this a new play?

This is the core rule set, and it is small enough to fit in one function:

```go
func Decide(prev State, trackID string, progressMS *int32, threshold time.Duration) Decision {
	if prev.TrackID != trackID {
		return Decision{NewHistory: true}
	}
	if prev.ProgressMS != nil && progressMS != nil &&
		int64(*progressMS)+threshold.Milliseconds() < int64(*prev.ProgressMS) {
		return Decision{Replay: true}
	}
	return Decision{}
}
```

In words:

1. A different track from the last snapshot starts a new history row.
2. The same track, with progress more than 10 seconds behind the last snapshot, is a replay. That bumps `replay_count` on the latest row. (The threshold is `REPLAY_RESET_THRESHOLD`.)
3. Anything else continues the current row.

A fourth rule lives in `ClearPlayback`. When nothing is playing, the API deletes the saved state. If I resume the same song an hour later, it counts as a new play. That matches how the old Deno poller behaved.

`Decide` is a pure function. It takes the old state and the new snapshot and returns a decision. All the I/O sits around it. That makes it easy to test with a table of cases.

### Surviving restarts

The previous state lives in a Redis hash, not in the poller's memory. A poller restart does not create a duplicate play, because the API still knows which track it saw last.

There is a second guard in the database. `listening_history` has a unique key on `(track_id, played_at)`, and `played_at` comes from Spotify's own timestamp on the snapshot. The insert uses `ON CONFLICT DO NOTHING`:

```sql
INSERT INTO listening_history (track_id, played_at, updated_at, duration_ms, replay_count, context_type, context_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (track_id, played_at) DO NOTHING
```

So even if Redis loses the state, the same snapshot cannot write a second row. The same key makes the Directus import safe to run more than once.

### One transaction per snapshot

`RecordPlayback` in `internal/tracker` does all the writes in one transaction: catalog upserts (artists, album, track), the `current_playback` row, and the history or replay change. Side effects come after the commit and are best effort: save the Redis state, queue enrichment, drop the response cache, publish the now-playing update. If one of those fails, it logs a warning. The database stays correct.

To skip extra writes, the API sets a Redis marker for each track it has upserted, with a 6-hour TTL. While that marker exists, a snapshot of the same track skips the catalog upserts.

## From poll to browser

After each recorded snapshot, the API loads the now-playing view, caches it in Redis, and publishes it on a pub/sub channel in one pipeline.

The public endpoint `GET /api/v1/now-playing/stream` uses Server-Sent Events (SSE): a plain HTTP response that stays open while the server writes `event:` and `data:` lines. Each connection subscribes to the Redis channel:

```go
send := func(event string, data []byte) bool {
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
// ... send the current snapshot first, then:
for {
	select {
	case <-ctx.Done():
		return
	case msg, ok := <-ch:
		if !ok || !send("now-playing", []byte(msg.Payload)) {
			return
		}
	case <-keepAlive.C:
		fmt.Fprint(w, ": keep-alive\n\n")
		flusher.Flush()
	}
}
```

A few small choices keep the stream healthy:

- The handler flushes the headers right away. Until the first flush, no bytes reach the client, and my client treats a silent stream as dead and falls back to polling.
- It sets `X-Accel-Buffering: no` so a proxy does not hold the events in a buffer.
- It sends the current snapshot first, so a new visitor sees something at once.
- A comment line every 25 seconds keeps idle connections open.

Because the stream fans out through Redis, any number of API copies can serve it. The poller does not know or care how many browsers are watching.

In Part 3 I move to the enricher, which turns a bare track into tempo, key, energy and genres.
