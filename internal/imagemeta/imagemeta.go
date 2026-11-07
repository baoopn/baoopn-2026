// Package imagemeta reads what the media pipeline needs to know about an
// image before decoding it: its type, its dimensions (from the header only),
// its EXIF orientation and whether it is animated. It is pure Go with no
// WebAssembly codecs, so both the API and the media worker import it.
package imagemeta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"net/http"
	"strings"

	// Header decoders for image.DecodeConfig (pure Go).
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// Content types of the raster formats the worker converts to WebP.
const (
	PNG  = "image/png"
	JPEG = "image/jpeg"
	GIF  = "image/gif"
	WebP = "image/webp"
	BMP  = "image/bmp"
	TIFF = "image/tiff"
	HEIC = "image/heic"
)

// Raster reports whether ctype is a format the worker converts to WebP.
func Raster(ctype string) bool {
	switch ctype {
	case PNG, JPEG, GIF, WebP, BMP, TIFF, HEIC:
		return true
	}
	return false
}

// SniffRaster detects a convertible raster type from the first bytes of a
// file ("" when it is none of them).
func SniffRaster(head []byte) string {
	switch {
	case len(head) >= 4 && (string(head[:4]) == "II*\x00" || string(head[:4]) == "MM\x00*"):
		return TIFF
	case isHEIC(head):
		return HEIC
	}
	ct := http.DetectContentType(head)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	if Raster(ct) {
		return ct
	}
	return ""
}

// heicBrands are ISOBMFF brands of HEVC-coded HEIF files.
var heicBrands = map[string]bool{"heic": true, "heix": true, "heim": true, "heis": true,
	"hevc": true, "hevx": true, "hevm": true, "hevs": true}

// isHEIC checks the ftyp box: a HEVC major brand, or the generic mif1/msf1
// brand with a HEVC compatible brand (and no AVIF one).
func isHEIC(head []byte) bool {
	if len(head) < 16 || string(head[4:8]) != "ftyp" {
		return false
	}
	major := string(head[8:12])
	if heicBrands[major] {
		return true
	}
	if major != "mif1" && major != "msf1" {
		return false
	}
	size := int(binary.BigEndian.Uint32(head[:4]))
	if size > len(head) {
		size = len(head)
	}
	hevc := false
	for i := 16; i+4 <= size; i += 4 {
		b := string(head[i : i+4])
		if b == "avif" || b == "avis" {
			return false
		}
		if heicBrands[b] {
			hevc = true
		}
	}
	return hevc
}

// Info describes an image without decoding its pixels.
type Info struct {
	// Width and Height are the stored dimensions, before orientation.
	Width, Height int
	// Orientation is the EXIF orientation (1-8) the decoder does NOT apply;
	// always 1 for HEIC (the decoder applies its transforms).
	Orientation int
	// Animated is true for a GIF with several frames or an animated WebP.
	Animated bool
	// SizeKnown is false when the header gave no dimensions (some HEIC files).
	SizeKnown bool
	// quarterTurn: a HEIC irot of 90° or 270° (applied by the decoder).
	quarterTurn bool
}

// Pixels is Width × Height.
func (i Info) Pixels() int64 { return int64(i.Width) * int64(i.Height) }

// Oriented returns the dimensions after the orientation is applied.
func (i Info) Oriented() (w, h int) {
	if i.quarterTurn || i.Orientation >= 5 && i.Orientation <= 8 {
		return i.Height, i.Width
	}
	return i.Width, i.Height
}

// ErrUndecodable means the header could not be parsed.
var ErrUndecodable = errors.New("imagemeta: image header could not be read")

// Probe reads the header of data, an image of type ctype.
func Probe(data []byte, ctype string) (Info, error) {
	info := Info{Orientation: 1}
	if ctype == HEIC {
		info.Width, info.Height, info.SizeKnown = heicSize(data)
		info.quarterTurn = heicQuarterTurn(data)
		return info, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return info, ErrUndecodable
	}
	info.Width, info.Height, info.SizeKnown = cfg.Width, cfg.Height, true
	info.Orientation = Orientation(data, ctype)
	switch ctype {
	case GIF:
		info.Animated = gifFrames(data) > 1
	case WebP:
		info.Animated = webpAnimated(data)
	}
	return info, nil
}

// heicSize returns the largest ispe (image spatial extents) property in the
// file: the primary image (or its grid), not a thumbnail.
func heicSize(data []byte) (w, h int, ok bool) {
	limit := min(len(data), 4<<20)
	var best int64
	for off := 0; ; {
		i := bytes.Index(data[off:limit], []byte("ispe"))
		if i < 0 {
			break
		}
		p := off + i
		off = p + 4
		if p < 4 || p+16 > limit {
			continue
		}
		if binary.BigEndian.Uint32(data[p-4:]) != 20 {
			continue
		}
		bw := int(binary.BigEndian.Uint32(data[p+8:]))
		bh := int(binary.BigEndian.Uint32(data[p+12:]))
		if bw > 0 && bh > 0 && int64(bw)*int64(bh) > best {
			best, w, h, ok = int64(bw)*int64(bh), bw, bh, true
		}
	}
	return w, h, ok
}

// heicQuarterTurn reports an irot (image rotation) property of 90° or 270°.
func heicQuarterTurn(data []byte) bool {
	limit := min(len(data), 4<<20)
	i := bytes.Index(data[:limit], []byte("irot"))
	if i < 4 || i+5 > limit || binary.BigEndian.Uint32(data[i-4:]) != 9 {
		return false
	}
	return data[i+4]&0x03%2 == 1
}
