package listening

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
)

// PlaylistTrack is types.ts PlaylistTrack.
type PlaylistTrack struct {
	Position int   `json:"position"`
	Plays    int64 `json:"plays"`
	Track    Track `json:"track"`
}

// TopPlaylist is types.ts TopPlaylist.
type TopPlaylist struct {
	Period      string          `json:"period"`
	PeriodStart time.Time       `json:"period_start"`
	PeriodEnd   time.Time       `json:"period_end"`
	TimeZone    string          `json:"time_zone"`
	Label       string          `json:"label"`
	Name        string          `json:"name"`
	SpotifyID   string          `json:"spotify_id"`
	SpotifyURL  string          `json:"spotify_url"`
	TrackCount  int             `json:"track_count"`
	TotalPlays  int64           `json:"total_plays"`
	PublishedAt time.Time       `json:"published_at"`
	Tracks      []PlaylistTrack `json:"tracks"`
}

// PlaylistPeriods is types.ts TopPlaylists.periods (newest first).
type PlaylistPeriods struct {
	Week  []TopPlaylist `json:"week"`
	Month []TopPlaylist `json:"month"`
	Year  []TopPlaylist `json:"year"`
}

// TopPlaylists is types.ts TopPlaylists.
type TopPlaylists struct {
	TimeZone string          `json:"time_zone"`
	Periods  PlaylistPeriods `json:"periods"`
}

// emptyTopPlaylists is the value served when SPOTIFY_API_URL is unset.
func emptyTopPlaylists() TopPlaylists {
	return TopPlaylists{TimeZone: "UTC", Periods: PlaylistPeriods{
		Week: []TopPlaylist{}, Month: []TopPlaylist{}, Year: []TopPlaylist{}}}
}

const spotifyOpenPrefix = "https://open.spotify.com/"

var (
	spotifyIDRe = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	timeZoneRe  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+\-/]*$`)
)

func cleanTimeZone(tz string) string {
	if len(tz) > 64 || !timeZoneRe.MatchString(tz) {
		return "UTC"
	}
	return tz
}

// cleanPlaylists keeps the valid entries listed under period, in upstream order.
func cleanPlaylists(period string, in []TopPlaylist) []TopPlaylist {
	out := []TopPlaylist{}
	for _, p := range in {
		if p.Period != period || !spotifyIDRe.MatchString(p.SpotifyID) || !strings.HasPrefix(p.SpotifyURL, spotifyOpenPrefix) {
			continue
		}
		p.PeriodStart = p.PeriodStart.UTC()
		p.PeriodEnd = p.PeriodEnd.UTC()
		p.PublishedAt = p.PublishedAt.UTC()
		p.TimeZone = cleanTimeZone(p.TimeZone)
		if p.Tracks == nil {
			p.Tracks = []PlaylistTrack{}
		}
		for i := range p.Tracks {
			p.Tracks[i].Track.normalize()
		}
		out = append(out, p)
	}
	return out
}

func (m *Module) topPlaylists(w http.ResponseWriter, r *http.Request) {
	limit, ok := intParam(r.URL.Query().Get("limit"), 6, 1, 24)
	if !ok {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "limit must be between 1 and 24")
		return
	}
	m.serve(w, r, "/api/v1/playlists/top?limit="+strconv.Itoa(limit), ttlTop, func(raw []byte) (any, error) {
		var v TopPlaylists
		if err := decodeData(raw, &v); err != nil {
			return nil, err
		}
		v.TimeZone = cleanTimeZone(v.TimeZone)
		v.Periods = PlaylistPeriods{
			Week:  cleanPlaylists("week", v.Periods.Week),
			Month: cleanPlaylists("month", v.Periods.Month),
			Year:  cleanPlaylists("year", v.Periods.Year),
		}
		return v, nil
	})
}
