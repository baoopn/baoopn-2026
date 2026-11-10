package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Brand files (internal/brand) live under brand/ in the bucket or MEDIA_DIR.
// They have no media row and are content-addressed by a 16-hex hash, so the
// orphan cleanup (which only looks at root names with a 24-hex stem) and
// mediarefs never see them.

// BrandKeyRe matches a brand file key: brand/<stem>.<16 hex>.<ext>, or
// brand/listening/<stem>.<16 hex>.<ext> for the listening sub-brand. No
// other subfolder is allowed.
var BrandKeyRe = regexp.MustCompile(`^brand/(listening/)?[a-z][a-z0-9-]{0,39}\.[0-9a-f]{16}\.(ico|png|svg)$`)

// ErrBadBrandKey is returned for a key that does not match BrandKeyRe.
var ErrBadBrandKey = errors.New("media: invalid brand file key")

// brandTypeByExt are the content types brand files are served with.
var brandTypeByExt = map[string]string{
	".ico": "image/x-icon",
	".png": "image/png",
	".svg": "image/svg+xml",
}

func brandPath(dir, key string) string { return filepath.Join(dir, filepath.FromSlash(key)) }

// PutBrandFile stores data under key (bucket: immutable cache headers, SVG
// as an attachment; disk: MEDIA_DIR/brand or MEDIA_DIR/brand/listening,
// written atomically).
func (s *Store) PutBrandFile(ctx context.Context, key, ctype string, data []byte) error {
	if !BrandKeyRe.MatchString(key) {
		return ErrBadBrandKey
	}
	if s.s3 != nil {
		_, err := s.s3.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:             aws.String(s.s3.bucket),
			Key:                aws.String(key),
			Body:               bytes.NewReader(data),
			ContentLength:      aws.Int64(int64(len(data))),
			ContentType:        aws.String(ctype),
			CacheControl:       aws.String(immutableCache),
			ContentDisposition: putDisposition(ctype),
		})
		if err != nil {
			return fmt.Errorf("media: put brand object: %w", err)
		}
		return nil
	}
	dst := brandPath(s.dir, key)
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("media: brand dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("media: brand temp file: %w", err)
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("media: write brand file: %w", errors.Join(werr, cerr))
	}
	_ = os.Chmod(tmpName, 0o644)
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("media: rename brand file: %w", err)
	}
	return nil
}

// BrandFileExists reports whether key is stored.
func (s *Store) BrandFileExists(ctx context.Context, key string) (bool, error) {
	if !BrandKeyRe.MatchString(key) {
		return false, ErrBadBrandKey
	}
	if s.s3 != nil {
		return s.s3.exists(ctx, key)
	}
	st, err := os.Stat(brandPath(s.dir, key))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return st.Mode().IsRegular(), nil
}

// RemoveBrandFile deletes key; a missing file is not an error.
func (s *Store) RemoveBrandFile(ctx context.Context, key string) error {
	if !BrandKeyRe.MatchString(key) {
		return ErrBadBrandKey
	}
	if s.s3 != nil {
		if err := s.s3.remove(ctx, key); err != nil && !isNotFound(err) {
			return err
		}
		return nil
	}
	if err := os.Remove(brandPath(s.dir, key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
