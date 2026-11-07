// Package app holds build metadata and the container healthcheck probe.
package app

import (
	"net"
	"net/http"
	"time"
)

// Version is set at build time via -ldflags "-X .../app.Version=...".
var Version = "dev"

// Healthcheck GETs /healthz on the local server; returns a process exit code.
func Healthcheck(addr string) int {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 1
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
