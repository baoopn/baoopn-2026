// Package mediarefs finds references to stored media files in content.
//
// A stored file is named <stem>.<ext>, the stem being 24 random lowercase
// hex characters. Content refers to it by URL in many forms (/media/<file>,
// MEDIA_PUBLIC_URL/<file>, an absolute site URL), and the media fallback can
// change the extension, so references are matched by stem only.
//
// Content writers call Touch in their write transaction, with the old and the
// new text, so a file that was just used (or just removed from a post and
// about to be pasted into another) starts a fresh grace period. The orphan
// cleanup takes LockKey exclusively while it re-checks and deletes rows;
// Touch takes it shared, so a save and a cleanup never interleave.
package mediarefs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// LockKey is the advisory lock for "media references".
const LockKey int64 = 0x6d656469615f7266 // "media_rf"

// StemLen is the length of a stored file's stem.
const StemLen = 24

// Tables are the tables that may reference media. Each is scanned whole
// (every column, as JSON), so new columns are covered without code changes.
// The schema guard test fails when a table is added and not classified here
// or in Ignored.
var Tables = []string{"posts", "post_parts", "projects", "settings"}

// Ignored are the tables that never hold content references.
var Ignored = []string{"media", "media_jobs", "threads", "thread_messages", "inbound_rejects",
	"page_view_stats", "page_visitors", "page_visitor_hours", "goose_db_version"}

func isHex(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' }

func isAnyHex(c byte) bool { return isHex(c) || c >= 'A' && c <= 'F' }

// Stems returns every bounded run of exactly 24 lowercase hex characters in s:
// the bytes before and after are not hex digits of either case (or are the
// ends of s). A 32-hex token or a 64-hex sha256 never matches. Duplicates
// are kept out.
//
// An escape right before the stem also bounds it, although it ends in hex
// digits: a percent escape ("%2F", double-encoded "%252F" in share and proxy
// URLs) or a JSON escape ("\f", "\b", "/"). Matching more only ever
// keeps a file.
func Stems(s string) []string {
	var out []string
	var seen map[string]bool
	for i := 0; i < len(s); {
		if !isAnyHex(s[i]) {
			i++
			continue
		}
		j := i
		for j < len(s) && isAnyHex(s[j]) {
			j++
		}
		// The stem must end the run; it may start it, or follow an escape tail.
		if k := j - StemLen; k >= i && (k == i || escapeTail(s, i, k)) && lowerHex(s[k:j]) {
			st := s[k:j]
			if !seen[st] {
				if seen == nil {
					seen = map[string]bool{}
				}
				seen[st] = true
				out = append(out, st)
			}
		}
		i = j
	}
	return out
}

// escapeTail reports whether s[i:k], the hex digits that start a hex run and
// come before a possible stem at k, are the end of an escape.
func escapeTail(s string, i, k int) bool {
	n := k - i
	switch {
	case i >= 1 && s[i-1] == '%': // %2F, %252F, %25252F
		return n%2 == 0 && n <= 6
	case i >= 1 && s[i-1] == '\\': // \f, \b
		return n == 1
	case i >= 2 && s[i-1] == 'u' && s[i-2] == '\\': // /
		return n == 4
	}
	return false
}

func lowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isHex(s[i]) {
			return false
		}
	}
	return true
}

// Querier is a pgx.Tx, a pool or a connection.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Scan returns the set of stems referenced anywhere in Tables.
func Scan(ctx context.Context, q Querier) (map[string]bool, error) {
	where, err := ScanWhere(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(where))
	for st := range where {
		out[st] = true
	}
	return out, nil
}

// ScanWhere is Scan that also names the first table each stem was found in.
func ScanWhere(ctx context.Context, q Querier) (map[string]string, error) {
	out := map[string]string{}
	for _, t := range Tables {
		// t is one of the constants above, never input.
		rows, err := q.Query(ctx, `SELECT row_to_json(t)::text FROM `+t+` t`)
		if err != nil {
			return nil, fmt.Errorf("mediarefs: scan %s: %w", t, err)
		}
		for rows.Next() {
			var text string
			if err := rows.Scan(&text); err != nil {
				rows.Close()
				return nil, fmt.Errorf("mediarefs: scan %s: %w", t, err)
			}
			for _, st := range Stems(text) {
				if _, ok := out[st]; !ok {
					out[st] = t
				}
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("mediarefs: scan %s: %w", t, err)
		}
	}
	return out, nil
}

// Touch takes the shared references lock for the rest of tx and bumps
// media.touched_at for every stem in texts (pass the old and the new text).
func Touch(ctx context.Context, tx Querier, texts ...string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, LockKey); err != nil {
		return fmt.Errorf("mediarefs: lock: %w", err)
	}
	var stems []string
	seen := map[string]bool{}
	for _, t := range texts {
		for _, st := range Stems(t) {
			if !seen[st] {
				seen[st] = true
				stems = append(stems, st)
			}
		}
	}
	if len(stems) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE media SET touched_at = now() WHERE stem = ANY($1)`, stems); err != nil {
		return fmt.Errorf("mediarefs: touch: %w", err)
	}
	return nil
}
