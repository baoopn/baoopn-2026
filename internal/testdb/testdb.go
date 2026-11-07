// Package testdb gives DB-backed tests an isolated, migrated schema.
//
// Tests are skipped unless TEST_DATABASE_URL is set. Locally, start
// `go run ./internal/testdb/cmd/pg` (embedded Postgres on :5434) and export
// TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5434/postgres?sslmode=disable
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/db"
)

// New returns a pool whose search_path is a fresh schema with all
// migrations applied. The schema is dropped when the test ends.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool := Empty(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}
	return pool
}

// Empty is New without migrations, for tests that migrate step by step.
func Empty(t testing.TB) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("testdb: connect: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatalf("testdb: create schema: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("testdb: parse url: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("testdb: pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = admin.Exec(c, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	return pool
}
