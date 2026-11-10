package httpapi

import (
	"net/http"
	"strings"
)

// AllowedOrigin returns the request's Origin when it is in the list, else "".
// Only exact matches count; "null" never does.
func AllowedOrigin(r *http.Request, origins []string) string {
	o := strings.ToLower(strings.TrimSpace(r.Header.Get("Origin")))
	if o == "" || o == "null" {
		return ""
	}
	for _, s := range origins {
		if s == o {
			return o
		}
	}
	return ""
}
