package analytics

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
)

// beacon is the event body (POST /api/v1/events); see BeaconEvent in types.ts.
type beacon struct {
	Type      string `json:"type"` // "pageview" | "engagement"
	Path      string `json:"path"`
	Referrer  string `json:"referrer"`
	Width     int    `json:"width"`
	VisibleMS int64  `json:"visible_ms"`
}

var botUA = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|headless|lighthouse|pingdom|uptime|monitor|preview|curl|wget|python|go-http|java/|node-fetch|axios|playwright|phantom`)

// EventsHandler accepts page-view beacons (text/plain or JSON). It answers
// 202 for every well-formed event and never reveals whether it was counted:
// bots and non-content paths are dropped silently.
func (r *Recorder) EventsHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	body, err := io.ReadAll(io.LimitReader(req.Body, maxEventBytes+1))
	if err != nil || len(body) > maxEventBytes {
		httpapi.Error(w, http.StatusRequestEntityTooLarge, "too_large", "event too large")
		return
	}
	var ev beacon
	if err := json.Unmarshal(body, &ev); err != nil || (ev.Type != "pageview" && ev.Type != "engagement") {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "invalid event")
		return
	}
	w.WriteHeader(http.StatusAccepted)
	r.record(req, ev)
}

func (r *Recorder) record(req *http.Request, ev beacon) {
	ua := req.UserAgent()
	if ua == "" || botUA.MatchString(ua) {
		return
	}
	path := cleanPath(ev.Path)
	if excludedPath(path) {
		return
	}
	now := r.now().UTC()
	k := pageKey{
		bucket:  now.Truncate(time.Hour),
		path:    path,
		device:  deviceOf(ev.Width, ua),
		browser: browserOf(ua),
		country: httpapi.Country(req),
	}
	visitor := r.visitorHash(now, httpapi.ClientIP(req), ua)
	if ev.Type == "engagement" {
		// Time on page is kept under the view's dimensions without the
		// referrer, which only describes how the visit started.
		if ms := min(max(ev.VisibleMS, 0), maxVisibleMS); ms > 0 {
			r.observePage(k, 0, ms)
		}
		r.markLive(visitor, now)
		return
	}
	k.referrer = referrerHost(ev.Referrer, r.siteHost)
	r.observePage(k, 1, 0)
	r.trackVisitor(now, path, visitor)
}

// excludedPath reports whether a (cleaned) path is not a public page we
// count. Only known routes are accepted, so a script cannot fill the stats
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

// slugPath is one URL slug segment as the API generates them.
var slugPath = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,99}$`)

// cleanPath keeps only a short, lower-case pathname without a trailing slash.
func cleanPath(p string) string {
	if u, err := url.Parse(p); err == nil {
		p = u.Path
	}
	if p == "" || p[0] != '/' {
		p = "/"
	}
	if len(p) > 100 {
		p = p[:100]
	}
	p = strings.ToLower(p)
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
		if p == "" {
			p = "/"
		}
	}
	return p
}

func deviceOf(width int, ua string) string {
	switch {
	case width > 0 && width < 768:
		return "mobile"
	case width > 0 && width < 1024:
		return "tablet"
	case width > 0:
		return "desktop"
	case strings.Contains(ua, "Mobi"):
		return "mobile"
	default:
		return "desktop"
	}
}

func browserOf(ua string) string {
	switch {
	case strings.Contains(ua, "Edg/"), strings.Contains(ua, "EdgA/"), strings.Contains(ua, "EdgiOS/"):
		return "Edge"
	case strings.Contains(ua, "OPR/"), strings.Contains(ua, "Opera"):
		return "Opera"
	case strings.Contains(ua, "Firefox/"), strings.Contains(ua, "FxiOS/"):
		return "Firefox"
	case strings.Contains(ua, "Chrome/"), strings.Contains(ua, "CriOS/"), strings.Contains(ua, "Chromium/"):
		return "Chrome"
	case strings.Contains(ua, "Safari/") && strings.Contains(ua, "Version/"):
		return "Safari"
	default:
		return "Other"
	}
}

// referrerHost reduces a referrer to its host. Visits from the site itself
// and unparsable values count as direct.
func referrerHost(ref, selfHost string) string {
	u, err := url.Parse(ref)
	if ref == "" || err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	self := strings.TrimPrefix(strings.ToLower(hostOnly(selfHost)), "www.")
	if host == self || len(host) > 100 {
		return ""
	}
	return host
}

func hostOnly(h string) string {
	if u, err := url.Parse("//" + h); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return h
}

// HostOf returns the host of a URL such as PUBLIC_URL ("" when unparsable).
func HostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
