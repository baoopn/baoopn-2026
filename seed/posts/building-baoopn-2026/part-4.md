I wanted to know which posts people read and where they came from. I did not want cookies, a consent banner, or a third-party script. So the site counts visits itself, in Postgres, with no IDs stored in the browser. This part covers how a page view gets from the browser into a report.

![The admin analytics page with the views and visitors chart for the last 30 days](screenshot:admin-analytics)

## The beacon

The browser side is about 60 lines. On each client navigation it sends one `pageview` event with the path, the referrer, and the window width. When the page is hidden or closed, it sends an `engagement` event with how long the page was visible.

```ts
function send(event: BeaconEvent): void {
	const body = JSON.stringify(event);
	try {
		if (navigator.sendBeacon?.(ENDPOINT, body)) return;
	} catch {
		// fall through to fetch
	}
	void fetch(ENDPOINT, { method: 'POST', body, keepalive: true }).catch(() => {});
}

// Automated browsers (tests, scrapers) announce themselves; don't count them.
const enabled = () => typeof navigator !== 'undefined' && !navigator.webdriver;
```

`sendBeacon` is built for this. The browser delivers it even as the page unloads. If it is missing or refuses, a `fetch` with `keepalive` does the same job. There are no cookies, no local storage, and no IDs. Browsers driven by automation set `navigator.webdriver`, and the beacon stays off for them.

## The endpoint

`POST /api/v1/events` takes a body of at most 2 KB. It answers `202 Accepted` for every well-formed event, whether it counts it or not. A script testing the endpoint cannot tell what passed the filters.

Then the filters run:

- **Bots.** An empty user agent, or one that matches a list of words like `bot`, `crawl`, `spider`, `headless`, `lighthouse`, `curl`, `python` or `playwright`, is dropped.
- **Paths.** Only known routes count: `/`, `/projects`, `/blog`, `/about`, `/contact`, and one slug segment under `/blog/` or `/projects/`. The path is cleaned first: lower case, no query, no trailing slash, at most 100 characters.

```go
// Only known routes are accepted, so a script cannot fill the stats
// tables with arbitrary unique paths: the fixed pages, plus one slug segment
// under /blog and /projects, which the web app serves.
func excludedPath(p string) bool {
	switch p {
	case "/", "/projects", "/blog", "/about", "/contact":
		return false
	}
	for _, pre := range []string{"/blog/", "/projects/"} {
		if rest, ok := strings.CutPrefix(p, pre); ok && slugPath.MatchString(rest) {
			return false
		}
	}
	return true
}
```

The path allow-list is the filter I care about most. Every distinct path becomes rows in the stats tables. Without the list, a loop that posts random paths could grow those tables without limit.

The rest of the event becomes a few coarse fields:

| Field | How it is found |
|---|---|
| Device | Window width: under 768 px is mobile, under 1024 px is tablet, else desktop |
| Browser | A short match on the user agent: Edge, Opera, Firefox, Chrome, Safari or Other |
| Country | The country header from the proxy, trusted only with the internal token |
| Referrer | The host alone, without `www.`; links from the site itself count as direct |

For the referrer check, the API compares against the host in its public URL setting, not the request's `Host` header. The proxy strips `Host` on the way in, so the API sees its own internal name there.

## Counting visitors without IDs

A view count is easy. A count of unique visitors needs some way to tell two visits from one person apart. I use a hash that changes every day:

```go
// visitorHash is 24 hex characters, stable within one UTC day.
func (r *Recorder) visitorHash(now time.Time, ip, ua string) string {
	day := now.UTC().Format("20060102")
	sum := sha256.Sum256([]byte(r.salt + "|" + day + "|" + ip + "|" + ua))
	return hex.EncodeToString(sum[:12])
}
```

The input is a secret salt, the UTC date, the IP, and the user agent. The salt comes from an HMAC of the app's secrets key, so it stays the same across restarts but no one outside the server knows it. Without the salt, someone with a table dump could hash every IP and match them. With the date in the input, the same person gets a new hash the next day, so visits on two days cannot be linked. The hash keeps 12 bytes of the SHA-256, which is plenty to keep visitors apart on a personal site.

The IP and user agent go into the hash and nowhere else. They are not stored.

The trade-off is clear: "unique visitors" means unique per day. A person who reads on Monday and on Tuesday counts twice in a weekly total. I accept that.

## Hourly buckets

The recorder does not write to Postgres on each event. It keeps counts in memory, keyed by the UTC hour plus path, device, browser, country, and referrer. Every minute it flushes them with an upsert that adds to the existing row:

```sql
ON CONFLICT (bucket, path, device, browser, country, referrer) DO UPDATE SET ...
```

Visible time from engagement events goes into the same rows, without the referrer. Each report is capped at 30 minutes, so a tab left open all night does not skew the average.

Visitor hashes go into two more tables. `page_visitors` holds one row per visitor per UTC day, for the whole site and for each path. `page_visitor_hours` holds one row per visitor per UTC hour. The recorder keeps a set of what it has queued today, so it writes each row once. The inserts use `ON CONFLICT DO NOTHING` as a backstop.

A small map in memory tracks hashes seen in the last five minutes. That is the "live now" count on the dashboard. A daily job deletes stats older than 400 days.

## Reports in my time zone

The dashboard sends the browser's IANA time zone with each report request, such as `Asia/Ho_Chi_Minh`. The API checks the name before it uses it.

Short ranges (24 hours, 7 days) use UTC hours, since an hour is an hour in any zone. Long ranges (30 and 90 days) use calendar days in my zone. That matters: a visit at 23:30 UTC is the next morning for me, and it should count on that day.

Postgres does the grouping:

```sql
SELECT (hour AT TIME ZONE $3)::date, count(DISTINCT visitor) FROM page_visitor_hours
WHERE hour >= $1 AND hour < $2 GROUP BY 1
```

This is why the hourly visitor table exists. With visitor rows per UTC day alone, I could not regroup them into local days. With rows per hour, any zone works.

On the Go side, the day boundaries come from `time.Date` and `AddDate` in the zone, never from adding 24 hours. On the days the clocks change for daylight saving, a day is 23 or 25 hours long, and adding 24 hours would put the bucket edges in the wrong place. Each report also returns totals for the period of the same length before it, so the dashboard can show the change.

The runtime image has no zoneinfo files, so the binary imports `time/tzdata` to carry its own copy. Without that line, every zone except UTC would fail in production and work on my laptop.

Part 5 covers how all this is built, tested, and deployed.
