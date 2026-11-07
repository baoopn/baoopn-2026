package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/baoopn/baoopn-2026/internal/media"
)

// The link rewrite must not run unless every local file reached the bucket.
func TestMigrateThenRewrite(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cases := []struct {
		name string
		st   media.MigrateStats
		err  error
		want bool
	}{
		{"complete", media.MigrateStats{Uploaded: 2, Present: 1}, nil, true},
		{"nothing to do", media.MigrateStats{}, nil, true},
		{"a put failed", media.MigrateStats{Uploaded: 1, Failed: 1}, nil, false},
		{"stopped early", media.MigrateStats{Uploaded: 1}, context.DeadlineExceeded, false},
		{"db error", media.MigrateStats{}, errors.New("db down"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ran := false
			migrateThenRewrite(context.Background(),
				func(context.Context) (media.MigrateStats, error) { return c.st, c.err },
				func(context.Context) { ran = true }, log)
			if ran != c.want {
				t.Fatalf("rewrite ran = %v, want %v", ran, c.want)
			}
		})
	}
}
