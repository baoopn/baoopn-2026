// Package settings stores JSON documents in the settings table.
package settings

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/mediarefs"
)

// Get loads key into dst. It reports false (and leaves dst untouched) when
// the key does not exist.
func Get(ctx context.Context, db *pgxpool.Pool, key string, dst any) (bool, error) {
	var raw []byte
	err := db.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(raw, dst)
}

// Put stores v under key. Media named in the old or the new value get a
// fresh grace period (mediarefs.Touch), in the same transaction.
func Put(ctx context.Context, db *pgxpool.Pool, key string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var old string
		err := tx.QueryRow(ctx, `SELECT value::text FROM settings WHERE key = $1 FOR UPDATE`, key).Scan(&old)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := mediarefs.Touch(ctx, tx, old, string(raw)); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, key, raw)
		return err
	})
}

// Flag reports whether a one-off flag (e.g. a seed import) has been set.
func Flag(ctx context.Context, db *pgxpool.Pool, key string) (bool, error) {
	var v bool
	ok, err := Get(ctx, db, key, &v)
	return ok && v, err
}

// SetFlag marks a one-off flag as done.
func SetFlag(ctx context.Context, db *pgxpool.Pool, key string) error {
	return Put(ctx, db, key, true)
}
