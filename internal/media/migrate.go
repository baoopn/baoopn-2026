package media

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/baoopn/baoopn-2026/internal/logging"
)

// MigrateStats counts what MigrateToBucket did.
type MigrateStats struct {
	Uploaded, Present, NoLocal, Failed int
}

// MigrateToBucket copies every file in the media table that is in Dir but
// not yet in the bucket (HeadObject check), so it is safe to run on every
// start. Local files are kept. It does nothing without a bucket and never
// returns an error for a single file; those are logged and counted.
func (s *Store) MigrateToBucket(ctx context.Context) (MigrateStats, error) {
	var st MigrateStats
	if s.s3 == nil {
		return st, nil
	}
	rows, err := s.db.Query(ctx, `SELECT filename, content_type FROM media ORDER BY id`)
	if err != nil {
		return st, err
	}
	type file struct{ name, ctype string }
	var files []file
	for rows.Next() {
		var f file
		if err := rows.Scan(&f.name, &f.ctype); err != nil {
			rows.Close()
			return st, err
		}
		files = append(files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return st, err
	}

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return st, err
		}
		if !fileRe.MatchString(f.name) {
			continue
		}
		log := s.log.With(slog.String("filename", f.name))
		info, err := os.Stat(filepath.Join(s.dir, f.name))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				log.Warn("media migrate: stat", logging.Err(err))
				st.Failed++
			} else {
				st.NoLocal++
			}
			continue
		}
		ok, err := s.s3.exists(ctx, f.name)
		if err != nil {
			log.Warn("media migrate: head object", logging.Err(err))
			st.Failed++
			continue
		}
		if ok {
			st.Present++
			continue
		}
		if err := s.s3.put(ctx, f.name, f.ctype, filepath.Join(s.dir, f.name), info.Size()); err != nil {
			log.Warn("media migrate: upload", logging.Err(err))
			st.Failed++
			continue
		}
		log.Info("media migrate: uploaded")
		st.Uploaded++
	}
	return st, nil
}
