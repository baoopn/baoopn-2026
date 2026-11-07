A track from Spotify comes with a name, artists, an album, a duration and an ISRC. That is enough for a history list, but not for the fun charts. I wanted tempo, key, energy, mood and genres for every song.

Spotify used to give audio features through its own API. Since the February 2026 Web API changes, Development Mode apps no longer get them. So I switched to Soundcharts, which has a song lookup by ISRC that returns both audio features and genres.

This part covers how the enricher works: where work comes from, how it stays within rate limits, and what happens when a lookup fails.

## Enrichment states

Each track has one row in `track_features`. The row has a status:

| Status | Meaning |
|---|---|
| `pending` | Waiting for a lookup, or waiting for a retry. |
| `enriched` | Soundcharts returned audio features, genres, or both. |
| `unavailable` | Soundcharts does not know this ISRC, or the track has no ISRC. |
| `failed` | Every retry failed. A human can re-queue it. |

The row also holds `attempts`, `last_error` and `next_attempt_at`. That last column is the key to retries. The enricher only picks up rows where `status = 'pending'` and `next_attempt_at <= now()`.

I keep "unavailable" apart from "failed" on purpose. Unavailable is a final answer: asking again will not help. Failed means something went wrong on the way, and a later try might work.

## Two ways in

Work reaches the enricher by two paths.

**The stream.** When the API sees a track for the first time, it creates a `pending` row and appends the track to a Redis stream. The enricher reads that stream with a consumer group, so several enricher copies can share the work without picking the same message. This path is fast: a new song is often enriched while it is still playing.

**The sweep.** Every minute (`ENRICH_INTERVAL_MS`), the enricher asks the API for pending tracks over gRPC, in pages of 50. This catches anything the stream missed, plus all the scheduled retries.

The stream consumer acks every message, even when the lookup fails:

```go
g.Go(func() error {
	trackID, _ := msg.Values["track_id"].(string)
	isrc, _ := msg.Values["isrc"].(string)
	if trackID != "" {
		e.enrich(gctx, trackID, isrc)
	}
	// Ack regardless: failures are tracked in the database and
	// retried by the sweep with backoff.
	_ = e.rdb.XAck(ctx, redisx.StreamEnrich, redisx.GroupEnrich, msg.ID).Err()
	return nil
})
```

This was a choice. Redis streams can track unacked messages and redeliver them. But then I would have two retry systems: one in Redis and one in Postgres. I chose to keep a single one. The database row is the record of truth, and the stream is a hint that says "look at this one now". If the API cannot write to the stream at all, it logs a warning and moves on. The sweep will find the track.

The sweep has one more detail. It loops over pages until a page comes back short. A failed track gets a `next_attempt_at` in the future, so it drops out of the next page. That stops the loop from fetching the same broken track over and over.

## Rate limits

Soundcharts has request limits, and I share one credential across all workers. The client uses a token-bucket limiter from `golang.org/x/time/rate`, shared by all goroutines. The default is 10 requests per second (`SOUNDCHARTS_RPS`), with 5 workers (`ENRICH_CONCURRENCY`).

A 429 needs more than a local limiter. If Soundcharts says "slow down", every worker should stop, not only the one that got the 429. So the client keeps a shared "paused until" time:

```go
func (c *Client) wait(ctx context.Context) error {
	if until := time.Unix(0, c.pausedUntil.Load()); time.Now().Before(until) {
		t := time.NewTimer(time.Until(until))
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	return c.limiter.Wait(ctx)
}
```

When a response is a 429, the client sets `pausedUntil` to now plus `Retry-After`. If the header is missing, it pauses for 10 seconds. Every request goes through `wait`, so all workers hold off together. An atomic int64 is enough here. There is no lock to fight over.

## Sorting the answers

The client maps each response to one of four outcomes:

- **404** returns `(nil, nil)`: the ISRC is unknown. The track becomes `unavailable`.
- **429 or 5xx** returns a `RetryableError`. A network error does too.
- **Other non-200 codes** return a plain error.
- **200** parses the song. If it has an `audio` object, the features get filled in. Genres come as a root genre with a list of sub-genres.

If Soundcharts knows the song but has no audio data and no genres, the enricher still marks it `unavailable`. A track with no ISRC gets marked `unavailable` without a lookup.

## Backoff

When a lookup fails, the enricher reports it to the API over gRPC. The API adds one to `attempts` and schedules the next try:

```go
// Backoff returns the retry delay after n failed attempts.
func Backoff(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	d := time.Duration(math.Pow(2, float64(n-1))) * time.Minute
	if d > 24*time.Hour || d <= 0 {
		return 24 * time.Hour
	}
	return d
}
```

So the waits go 1 minute, 2, 4, 8, and so on, up to a cap of 24 hours. After 8 attempts (`ENRICH_MAX_ATTEMPTS`), the status becomes `failed`.

From the admin dashboard I can re-queue one track, or re-queue every failed track at once. Both reset the row to `pending`, so the next sweep picks it up. The single-track action also pushes the track onto the stream, so it runs right away.

## Freshness on the page

Enrichment is async, so a new song often shows up before its stats do. Two things handle this.

On the server, a successful enrichment drops the response cache (more on that in Part 4), so the next stats request sees the new data. Track responses that are still being analysed are sent with `Cache-Control: no-store`.

On the public page, the track detail query polls faster while the track is still pending. When the stats arrive, the page switches to its normal refresh interval.

![The song stats panel on the public page: a radar of audio features, a tempo gauge, a key wheel, a loudness meter and a genre bar](screenshot:public-song-stats)

## Artist images

One more background job lives in the API, not the enricher. Spotify no longer gives Development Mode apps artist popularity or followers, but `GET /artists/{id}` still returns images. Every minute the API checks up to 25 artists it has never looked up (most recently played first), 2 seconds apart. It checks each one again after 30 days. If a lookup fails or has no image, it keeps the old one.

Part 4 moves to the two web apps: the public page, the admin dashboard, and the design behind them.
