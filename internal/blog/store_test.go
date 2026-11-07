package blog

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/testdb"
)

type harness struct {
	t      *testing.T
	m      *Module
	public chi.Router
	admin  chi.Router
}

func newHarness(t *testing.T) *harness {
	pool := testdb.New(t)
	m := New(Deps{DB: pool, Log: slog.New(slog.DiscardHandler)})
	pub, adm := chi.NewRouter(), chi.NewRouter()
	m.Public(pub)
	m.Admin(adm)
	return &harness{t: t, m: m, public: pub, admin: adm}
}

func (h *harness) do(r chi.Router, method, path string, body any, out any) int {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, &buf))
	if out != nil && rec.Code < 300 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			h.t.Fatalf("%s %s: decode %v: %s", method, path, err, rec.Body)
		}
	}
	return rec.Code
}

func (h *harness) create(in PostInput) AdminPost {
	h.t.Helper()
	var out struct{ Data AdminPost }
	if code := h.do(h.admin, "POST", "/posts", in, &out); code != http.StatusCreated {
		h.t.Fatalf("create %q: %d", in.Title, code)
	}
	return out.Data
}

func at(d time.Duration) *time.Time {
	t := time.Now().Add(d).UTC().Truncate(time.Second)
	return &t
}

func one(md string) []PartInput { return []PartInput{{ContentMD: md}} }
