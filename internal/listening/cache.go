package listening

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// staleFactor: on upstream failure an entry is served up to 10× its TTL.
const staleFactor = 10

type entry struct {
	body       []byte
	exp        time.Time
	staleUntil time.Time
}

type cache struct {
	mu  sync.Mutex
	m   map[string]entry
	now func() time.Time
}

func newCache(now func() time.Time) *cache {
	return &cache{m: map[string]entry{}, now: now}
}

// get returns the entry and whether it is still fresh.
func (c *cache) get(key string) (e entry, fresh, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok = c.m[key]
	return e, ok && c.now().Before(e.exp), ok
}

func (c *cache) put(key string, body []byte, ttl time.Duration) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = entry{body: body, exp: now.Add(ttl), staleUntil: now.Add(staleFactor * ttl)}
}

// decodeData unwraps the upstream {"data": X} envelope into v.
func decodeData(raw []byte, v any) error {
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	if len(env.Data) == 0 {
		return errors.New("listening: missing data")
	}
	return json.Unmarshal(env.Data, v)
}

var errUpstream = errors.New("listening: upstream unavailable")

// fetch GETs path from spotify-tracker and returns the trimmed JSON of data.
func (m *Module) fetch(ctx context.Context, path string, trim func([]byte) (any, error)) ([]byte, error) {
	if m.base == "" {
		return nil, fmt.Errorf("%w: SPOTIFY_API_URL not set", errUpstream)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errUpstream, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, fmt.Errorf("%w: HTTP %d", errUpstream, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("%w: read: %v", errUpstream, err)
	}
	v, err := trim(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: decode: %v", errUpstream, err)
	}
	return json.Marshal(v)
}

// serve answers from the cache, refreshing it from upstream when expired.
// On upstream failure a stale entry within 10× TTL is served instead.
func (m *Module) serve(w http.ResponseWriter, r *http.Request, path string, ttl time.Duration, trim func([]byte) (any, error)) {
	e, fresh, ok := m.cache.get(path)
	if !fresh {
		body, err := m.fetch(r.Context(), path, trim)
		switch {
		case err == nil:
			m.cache.put(path, body, ttl)
			e = entry{body: body}
		case ok && m.cache.now().Before(e.staleUntil):
			m.log.WarnContext(r.Context(), "listening: serving stale", slog.String("path", path), logging.Err(err))
		default:
			m.log.WarnContext(r.Context(), "listening: upstream failed", slog.String("path", path), logging.Err(err))
			httpapi.Error(w, http.StatusBadGateway, "upstream_unavailable", "listening data is unavailable right now")
			return
		}
	}
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(ttl/time.Second)))
	httpapi.JSON(w, http.StatusOK, json.RawMessage(e.body))
}
