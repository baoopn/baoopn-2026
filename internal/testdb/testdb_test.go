package testdb

import (
	"context"
	"testing"
)

func TestMigrationsApply(t *testing.T) {
	pool := New(t)
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema()`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n < 11 {
		t.Fatalf("expected migrated tables, got %d", n)
	}
}
