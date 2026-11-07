package media

import (
	"bytes"
	"net/http"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/baoopn/baoopn-2026/internal/imagemeta"
)

// extByType maps the stored content types to file extensions.
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

// sniff detects an allowed content type from the first bytes of a file. ext
// is the stored extension for types kept as they are ("" for BMP, TIFF and
// HEIC, which are only ever stored converted).
func sniff(head []byte, original string) (ctype, ext string, ok bool) {
	if rt := imagemeta.SniffRaster(head); rt != "" {
		return rt, extByType[rt], true
	}
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
