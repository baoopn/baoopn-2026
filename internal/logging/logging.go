// Package logging configures the process-wide slog logger.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New builds a logger, installs it as the default and returns it.
func New(service, level, format string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	l := slog.New(h).With(slog.String("service", service))
	slog.SetDefault(l)
	return l
}

// Err is a slog attribute for an error.
func Err(err error) slog.Attr { return slog.Any("err", err) }
