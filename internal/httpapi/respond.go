package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// JSON writes v as {"data": v}.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": v})
}

// Error writes {"error": {"code", "message"}}.
func Error(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

// maxJSONBody caps a JSON request body.
const maxJSONBody = 2 << 20

// Decode reads a JSON body (max 2 MiB) into v, rejecting unknown fields.
// A body over the cap answers 413 too_large; any other decode error answers
// 400 with a fixed message (the decoder's detail, which echoes field names
// and types, is only logged at debug).
func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			Error(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
			return false
		}
		slog.DebugContext(r.Context(), "invalid JSON body", slog.String("path", r.URL.Path), slog.String("err", err.Error()))
		Error(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return false
	}
	return true
}

// PageMeta is pagination metadata for list responses.
type PageMeta struct {
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
}

// JSONMeta writes v as {"data": v, "meta": meta}.
func JSONMeta(w http.ResponseWriter, status int, v any, meta PageMeta) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": v, "meta": meta})
}
