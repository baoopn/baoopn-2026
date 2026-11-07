package migrations_test

import (
	"context"
	"testing"

	"github.com/baoopn/baoopn-2026/internal/testdb"
)

// TestContactAcksMigration applies 00008 on a fresh schema, checks the
// thread link is cleared when a thread goes, and that Down drops the table.
func TestContactAcksMigration(t *testing.T) {
	pool := testdb.Empty(t)
	ctx := context.Background()
	p := provider(t, pool)
	if _, err := p.UpTo(ctx, 8); err != nil {
		t.Fatal(err)
	}
	var tid int64
	if err := pool.QueryRow(ctx, `INSERT INTO threads (name, email, subject, reply_token)
		VALUES ('Sam', 'sam@example.com', 'Hi', 'tok') RETURNING id`).Scan(&tid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO contact_acks (thread_id, address_key) VALUES ($1, 'sam@example.com')`, tid); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM threads WHERE id = $1`, tid); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM contact_acks WHERE thread_id IS NULL`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("ack kept without its thread: %d %v", n, err)
	}
	if _, err := p.DownTo(ctx, 7); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('contact_acks') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("table after Down: %v %v", exists, err)
	}
	if _, err := p.UpTo(ctx, 8); err != nil {
		t.Fatal(err)
	}
}
