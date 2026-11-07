// Package media stores uploaded files in MEDIA_DIR, records them in the media
// table (deduplicated by content hash) and serves them at /media/<filename>.
package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/logging"
)

// Deps are the media store's dependencies.
type Deps struct {
	DB       *pgxpool.Pool
	Log      *slog.Logger
	Dir      string
	MaxBytes int64
}

// Store saves, lists, deletes and serves media files.
type Store struct {
	db       *pgxpool.Pool
	log      *slog.Logger
	dir      string
	maxBytes int64
}

// Media is one stored file (types.ts Media).
type Media struct {
	ID          int64     `json:"id"`
	URL         string    `json:"url"`
	Filename    string    `json:"filename"`
	Original    string    `json:"original"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
}

var (
	// ErrTooLarge is returned when the file exceeds MaxBytes.
	ErrTooLarge = errors.New("media: file too large")
	// ErrUnsupported is returned when the content type is not allowed.
	ErrUnsupported = errors.New("media: unsupported file type")
)

const defaultMaxBytes = 10 << 20

// New creates the media directory (0o755) and returns the store.
func New(d Deps) (*Store, error) {
	if d.Dir == "" {
		return nil, errors.New("media: Dir is required")
	}
	if err := os.MkdirAll(d.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("media: create dir: %w", err)
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.MaxBytes <= 0 {
		d.MaxBytes = defaultMaxBytes
	}
	return &Store{db: d.DB, log: d.Log, dir: d.Dir, maxBytes: d.MaxBytes}, nil
}

// extByType maps the allowed content types to file extensions.
var extByType = map[string]string{
	"image/png":       ".png",
	"image/jpeg":      ".jpg",
	"image/gif":       ".gif",
	"image/webp":      ".webp",
	"image/avif":      ".avif",
	"application/pdf": ".pdf",
	"image/svg+xml":   ".svg",
}

// typeByExt is the inverse of extByType, used when serving.
var typeByExt = func() map[string]string {
	m := make(map[string]string, len(extByType))
	for t, e := range extByType {
		m[e] = t
	}
	return m
}()

const sniffLen = 4096

// sniff detects an allowed content type from the first bytes of a file.
func sniff(head []byte, original string) (ctype, ext string, ok bool) {
	ct := http.DetectContentType(head)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(ct)
	if ct != "image/svg+xml" {
		if e, found := extByType[ct]; found {
			return ct, e, true
		}
	}
	if len(head) >= 12 {
		if b := string(head[4:12]); b == "ftypavif" || b == "ftypavis" {
			return "image/avif", ".avif", true
		}
	}
	if (ct == "text/xml" || ct == "text/plain") && strings.HasSuffix(strings.ToLower(original), ".svg") {
		h := head
		if len(h) > sniffLen {
			h = h[:sniffLen]
		}
		if bytes.Contains(bytes.ToLower(h), []byte("<svg")) {
			return "image/svg+xml", ".svg", true
		}
	}
	return "", "", false
}

// cleanOriginal keeps the base name, without control characters, ≤ 200 runes.
func cleanOriginal(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	if r := []rune(name); len(r) > 200 {
		name = string(r[:200])
	}
	return strings.TrimSpace(name)
}

// headWriter keeps the first sniffLen bytes written to it.
type headWriter struct{ buf []byte }

func (h *headWriter) Write(p []byte) (int, error) {
	if room := sniffLen - len(h.buf); room > 0 {
		h.buf = append(h.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// Save stores the contents of r. An identical file (same sha256) already in
// the table is reused instead of written again.
func (s *Store) Save(ctx context.Context, original string, r io.Reader) (Media, error) {
	tmp, err := os.CreateTemp(s.dir, ".up-*")
	if err != nil {
		return Media{}, fmt.Errorf("media: temp file: %w", err)
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()

	hash := sha256.New()
	head := &headWriter{}
	n, err := io.Copy(io.MultiWriter(tmp, hash, head), io.LimitReader(r, s.maxBytes+1))
	if err != nil {
		return Media{}, fmt.Errorf("media: write: %w", err)
	}
	if n > s.maxBytes {
		return Media{}, ErrTooLarge
	}
	original = cleanOriginal(original)
	ctype, ext, ok := sniff(head.buf, original)
	if !ok {
		return Media{}, ErrUnsupported
	}
	sum := hex.EncodeToString(hash.Sum(nil))

	existing, err := s.bySHA(ctx, sum)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Media{}, fmt.Errorf("media: dedupe lookup: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return Media{}, fmt.Errorf("media: close: %w", err)
	}
	rnd := make([]byte, 12)
	_, _ = rand.Read(rnd)
	filename := hex.EncodeToString(rnd) + ext
	final := filepath.Join(s.dir, filename)
	if err := os.Rename(tmpName, final); err != nil {
		return Media{}, fmt.Errorf("media: rename: %w", err)
	}
	keep = true
	_ = os.Chmod(final, 0o644)

	m := Media{Filename: filename, Original: original, ContentType: ctype, SizeBytes: n}
	err = s.db.QueryRow(ctx, `
		INSERT INTO media (filename, original, content_type, size_bytes, sha256)
		VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		filename, original, ctype, n, sum).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		_ = os.Remove(final)
		return Media{}, fmt.Errorf("media: insert: %w", err)
	}
	return finish(m), nil
}

func (s *Store) bySHA(ctx context.Context, sum string) (Media, error) {
	var m Media
	err := s.db.QueryRow(ctx, `
		SELECT id, filename, original, content_type, size_bytes, created_at
		FROM media WHERE sha256 = $1 LIMIT 1`, sum).
		Scan(&m.ID, &m.Filename, &m.Original, &m.ContentType, &m.SizeBytes, &m.CreatedAt)
	if err != nil {
		return Media{}, err
	}
	return finish(m), nil
}

func finish(m Media) Media {
	m.URL = "/media/" + m.Filename
	m.CreatedAt = m.CreatedAt.UTC()
	return m
}

// List returns one page of media, newest first, and the total count.
func (s *Store) List(ctx context.Context, page, limit int) ([]Media, int64, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, filename, original, content_type, size_bytes, created_at, count(*) OVER ()
		FROM media ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`,
		limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Media{}
	var total int64
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID, &m.Filename, &m.Original, &m.ContentType, &m.SizeBytes, &m.CreatedAt, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, finish(m))
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(out) == 0 && page > 1 {
		// Past the last page the window count is absent; report the real total.
		if err := s.db.QueryRow(ctx, `SELECT count(*) FROM media`).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

// Delete removes the row and its file. It reports false when no row matched.
func (s *Store) Delete(ctx context.Context, id int64) (bool, error) {
	var filename string
	err := s.db.QueryRow(ctx, `DELETE FROM media WHERE id = $1 RETURNING filename`, id).Scan(&filename)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := os.Remove(filepath.Join(s.dir, filename)); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.log.Warn("media: remove file", slog.String("filename", filename), logging.Err(err))
	}
	return true, nil
}
