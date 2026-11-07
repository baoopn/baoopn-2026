I like to know what I listen to. Spotify gives you a "Wrapped" once a year, but I wanted the data every day, in my own database, with my own charts. So I built spotify-tracker. It watches my Spotify account, writes down each track I play, and adds audio features and genres to each one. A public page shows what I am playing right now, and a private dashboard lets me look at the numbers.

![The public page at listening.baoopn.com, with the current track, album art and progress line](screenshot:public-home)

This post walks through how it works. This first part covers the why and the big picture. The next parts go into the poller, the enrichment worker, the two web apps, and how I run it.

## Where it came from

This is not the first version. The old one kept its data in Directus collections (artists, albums, tracks, track features, listening history, current playback). A small Deno poller wrote to Directus, and a second worker filled in the track features.

It worked, but it had limits:

- Directus was a general content tool doing a narrow job. Every write went through its REST API.
- The rules for "is this a new play or the same one?" lived in a worker, with state that a restart could lose.
- Spotify changed its Web API in 2026. Development Mode apps lost access to audio features, so the old enrichment path broke.

I wanted one codebase with clear ownership of the data, so I rewrote it in Go. I kept the play rules of the old Deno poller on purpose, so the new history would line up with the old one.

## The pieces

The system has three long-running services, plus PostgreSQL and Redis.

```
 Spotify  <-- poller   --gRPC-->  api  --SQL-->  PostgreSQL
Soundcharts <-- enricher --gRPC-->  api  --cache--> Redis
                  ^                                  |
                  +------ enrichment stream (Redis) --+
```

| Service | Job |
|---|---|
| `api` | Owns the database. Serves the public REST API, the live stream, the admin dashboard and its API, and an internal gRPC server. Sends alert emails. |
| `poller` | Asks Spotify what is playing every 3 seconds and forwards each snapshot over gRPC. |
| `enricher` | Looks up each new track on Soundcharts by ISRC (the code that identifies a recording) and sends back audio features and genres. |

There are two more commands for one-off jobs: `migrate` applies database migrations, and `importer` copies data from the old Directus setup.

All of them build into one Docker image. The container command picks which binary runs.

## One owner for the data

The main rule is simple: only the `api` talks to PostgreSQL. The poller and enricher hold no database credentials. They call the API over gRPC, and the API applies the business rules.

The gRPC services are small:

```protobuf
service IngestService {
  rpc RecordPlayback(RecordPlaybackRequest) returns (RecordPlaybackResponse);
  rpc ClearPlayback(ClearPlaybackRequest) returns (ClearPlaybackResponse);
}

service EnrichmentService {
  rpc ListPendingTracks(ListPendingTracksRequest) returns (ListPendingTracksResponse);
  rpc SubmitTrackFeatures(SubmitTrackFeaturesRequest) returns (SubmitTrackFeaturesResponse);
  rpc ReportEnrichmentFailure(ReportEnrichmentFailureRequest) returns (ReportEnrichmentFailureResponse);
}

service SpotifyService {
  rpc GetAccessToken(GetAccessTokenRequest) returns (GetAccessTokenResponse);
}
```

This split pays off in a few ways:

- The workers are stateless. I can restart them at any time. The enricher can run as more than one copy.
- Secrets stay in one place. The poller never sees the Spotify client secret or the refresh token. It asks the API for a short-lived access token.
- The rules for history, replays and enrichment live in one Go package, `internal/tracker`, with tests next to them.

The gRPC port lives on the private Docker network and is never published. Calls carry a shared bearer token, and clients retry `UNAVAILABLE` errors with exponential backoff, so a short API restart does not lose a poll.

## What lives where

PostgreSQL is the source of truth. The schema is plain:

```
artists --+                  +-- albums
          +-- track_artists --+
                             tracks --+-- listening_history  (track_id, played_at) unique
                                      +-- track_features     1:1, enrichment status + backoff
                                      +-- current_playback   one row
```

There are also tables for settings (with encrypted secrets), an admin audit log, playlists, and hourly analytics. Migrations are plain SQL files run by goose. They are embedded in the binary, so the image carries its own schema.

Redis holds everything that I can lose or rebuild:

- the last playback state, used to detect replays
- the cached now-playing JSON and a pub/sub channel for live updates
- a stream of tracks waiting for enrichment
- the cached Spotify access token and OAuth state
- worker heartbeats and analytics counters
- cached API responses

If Redis restarts empty, nothing in the history breaks. The enricher has a sweep that finds pending tracks in Postgres, and the next poll rebuilds the playback state.

## The stack

- Go 1.27, gRPC and protobuf (generated with buf), chi for HTTP routing
- PostgreSQL 17 through pgx, migrations with goose
- Redis 7
- SvelteKit with Svelte 5 and Tailwind for the public page and the admin app
- Clerk for admin sign-in
- Prometheus metrics and JSON logs with `log/slog`
- Distroless Docker images, built by GitHub Actions, deployed with Coolify

Both web apps build to static files and get embedded into the `api` binary with `go:embed`. That means one domain serves everything:

| Path | What |
|---|---|
| `/` | Public page: now playing and recently played |
| `/api/v1/*` | Public read-only REST API and the live stream |
| `/admin`, `/admin/api/*` | Admin dashboard and its API |

There is no separate frontend deploy and no CORS setup between my own apps. One image, one domain, one deploy.

## What's next

In Part 2 I look at the poller: how it gets tokens, how it decides when a play starts or repeats, and how a new snapshot reaches the browser within a few seconds.
