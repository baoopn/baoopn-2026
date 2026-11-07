package media

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
)

const maxOrphanKeys = 500

var orphanKeyRe = regexp.MustCompile(`^(row|object|local|local_copy|original|temp):[A-Za-z0-9._-]{1,64}$`)

// orphansPlan handles GET /media/orphans: the dry run.
func (s *Store) orphansPlan(w http.ResponseWriter, r *http.Request) {
	p, err := s.PlanOrphans(r.Context())
	if err != nil {
		s.internal(w, r, "media: orphan plan", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, p)
}

// orphansClean handles POST /media/orphans, body {"keys": [...]} (optional;
// empty or absent: every eligible candidate except local copies).
func (s *Store) orphansClean(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "request body too large")
		return
	}
	var in struct {
		Keys []string `json:"keys"`
	}
	if strings.TrimSpace(string(body)) != "" {
		dec := json.NewDecoder(strings.NewReader(string(body)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			httpapi.Error(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
			return
		}
	}
	if len(in.Keys) > maxOrphanKeys {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "at most 500 keys per run")
		return
	}
	for _, k := range in.Keys {
		if !orphanKeyRe.MatchString(k) {
			httpapi.Error(w, http.StatusBadRequest, "bad_request", "invalid key: keys look like row:12 or object:<file>")
			return
		}
	}
	keys := in.Keys
	if len(keys) == 0 {
		keys = nil
	}
	res, err := s.CleanOrphans(r.Context(), TriggerManual, keys)
	switch {
	case errors.Is(err, ErrDisabled):
		httpapi.Error(w, http.StatusConflict, "disabled", "orphan cleanup is off (MEDIA_ORPHAN_GRACE=0)")
	case errors.Is(err, ErrBusy):
		httpapi.Error(w, http.StatusConflict, "busy", "a cleanup is already running, try again shortly")
	case err != nil:
		s.internal(w, r, "media: orphan cleanup", err)
	default:
		httpapi.JSON(w, http.StatusOK, res)
	}
}

// orphansAuto handles PUT /media/orphans/auto, body {"enabled": bool}, and
// answers with the plan.
func (s *Store) orphansAuto(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if in.Enabled == nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "enabled (true or false) is required")
		return
	}
	if err := s.SetOrphanAuto(r.Context(), *in.Enabled); err != nil {
		s.internal(w, r, "media: orphan auto", err)
		return
	}
	s.orphansPlan(w, r)
}
