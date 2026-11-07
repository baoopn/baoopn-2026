// Package imagestrip removes metadata (EXIF with GPS, XMP, IPTC, comments,
// text chunks) from browser-safe images without decoding their pixels. The
// API uses it to publish an original when WebP conversion is impossible.
package imagestrip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrMalformed means the file structure could not be walked.
var ErrMalformed = errors.New("imagestrip: malformed file")

// BrowserSafe reports whether browsers display ctype without conversion.
func BrowserSafe(ctype string) bool {
	switch ctype {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	}
	return false
}

// Strip returns data without metadata. orientation (1-8) is the EXIF
// orientation of a JPEG: when it is not 1 a minimal EXIF block holding only
// that tag is kept, so the photo is not shown sideways.
func Strip(data []byte, ctype string, orientation int) ([]byte, error) {
	switch ctype {
	case "image/jpeg":
		return JPEG(data, orientation)
	case "image/png":
		return PNG(data)
	case "image/webp":
		return WebP(data)
	case "image/gif":
		return GIF(data)
	}
	return nil, fmt.Errorf("imagestrip: %s is not supported", ctype)
}

// JPEG drops APP1 (EXIF, XMP), APP13 (IPTC/Photoshop), APP2 segments other
// than the ICC profile, APP3-APP12/APP15 and comments. JFIF (APP0), the ICC
// profile and Adobe (APP14) are kept: they affect how colours decode. APP2
// MPF is dropped with the other APP2 segments. From the first scan on, only
// scans and the tables between them are kept, and the output ends at EOI:
// anything after it (motion-photo video, MPF images, gain maps) is dropped.
func JPEG(d []byte, orientation int) ([]byte, error) {
	if len(d) < 4 || d[0] != 0xFF || d[1] != 0xD8 {
		return nil, ErrMalformed
	}
	out := make([]byte, 0, len(d))
	out = append(out, 0xFF, 0xD8)
	inserted := orientation < 2 || orientation > 8
	for i := 2; ; {
		if i+4 > len(d) || d[i] != 0xFF {
			return nil, ErrMalformed
		}
		m := d[i+1]
		if m == 0xFF {
			i++
			continue
		}
		if m == 0xDA || m == 0xD9 {
			if !inserted {
				out = append(out, orientationAPP1(orientation)...)
			}
			if m == 0xD9 {
				return append(out, 0xFF, 0xD9), nil
			}
			return jpegScans(out, d, i)
		}
		n := int(binary.BigEndian.Uint16(d[i+2:]))
		if n < 2 || i+2+n > len(d) {
			return nil, ErrMalformed
		}
		seg := d[i : i+2+n]
		body := seg[4:]
		keep := true
		switch {
		case m == 0xE1, m == 0xED, m == 0xFE:
			keep = false
		case m == 0xE2:
			keep = bytes.HasPrefix(body, []byte("ICC_PROFILE\x00"))
		case m >= 0xE3 && m <= 0xEF && m != 0xEE:
			keep = false
		}
		if keep {
			// The orientation block goes right after JFIF (APP0), before anything else.
			if !inserted && m != 0xE0 {
				out = append(out, orientationAPP1(orientation)...)
				inserted = true
			}
			out = append(out, seg...)
		}
		i += 2 + n
	}
}

// orientationAPP1 is an Exif APP1 segment whose IFD0 holds only Orientation.
func orientationAPP1(o int) []byte {
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, // header, IFD0 at 8
		0, 1, // one entry
		0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, byte(o), 0, 0, // Orientation SHORT 1
		0, 0, 0, 0} // no next IFD
	body := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(body)+2))
	return append(seg, body...)
}

// pngDrop are the ancillary chunks that carry metadata.
var pngDrop = map[string]bool{"eXIf": true, "tEXt": true, "iTXt": true, "zTXt": true, "tIME": true}

// PNG drops eXIf, tEXt, iTXt, zTXt and tIME chunks.
func PNG(d []byte) ([]byte, error) {
	if len(d) < 8 || string(d[:8]) != "\x89PNG\r\n\x1a\n" {
		return nil, ErrMalformed
	}
	out := append(make([]byte, 0, len(d)), d[:8]...)
	for i := 8; ; {
		if i+12 > len(d) {
			return nil, ErrMalformed
		}
		n := int(binary.BigEndian.Uint32(d[i:]))
		if n < 0 || n > len(d)-i-12 {
			return nil, ErrMalformed
		}
		typ := string(d[i+4 : i+8])
		if !pngDrop[typ] {
			out = append(out, d[i:i+12+n]...)
		}
		i += 12 + n
		if typ == "IEND" {
			return out, nil
		}
	}
}

// WebP drops the EXIF and XMP chunks and clears their VP8X flags.
func WebP(d []byte) ([]byte, error) {
	if len(d) < 12 || string(d[:4]) != "RIFF" || string(d[8:12]) != "WEBP" {
		return nil, ErrMalformed
	}
	out := append(make([]byte, 0, len(d)), d[:12]...)
	for i := 12; i < len(d); {
		if i+8 > len(d) {
			return nil, ErrMalformed
		}
		n := int(binary.LittleEndian.Uint32(d[i+4:]))
		end := i + 8 + n + n&1
		if n < 0 || i+8+n > len(d) {
			return nil, ErrMalformed
		}
		end = min(end, len(d))
		switch string(d[i : i+4]) {
		case "EXIF", "XMP ":
		case "VP8X":
			c := append([]byte{}, d[i:end]...)
			if n >= 1 {
				c[8] &^= 0x08 | 0x04 // EXIF and XMP present flags
			}
			out = append(out, c...)
		default:
			out = append(out, d[i:end]...)
		}
		i = end
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	return out, nil
}

// GIF drops comment extensions and application extensions other than the
// looping ones (NETSCAPE2.0, ANIMEXTS1.0); XMP lives in one of those.
func GIF(d []byte) ([]byte, error) {
	if len(d) < 13 || string(d[:3]) != "GIF" {
		return nil, ErrMalformed
	}
	i := 13
	if d[10]&0x80 != 0 {
		i += 3 << (int(d[10]&0x07) + 1)
	}
	if i > len(d) {
		return nil, ErrMalformed
	}
	out := append(make([]byte, 0, len(d)), d[:i]...)
	sub := func(i int) (int, error) {
		for i < len(d) {
			n := int(d[i])
			i += 1 + n
			if n == 0 {
				return i, nil
			}
		}
		return 0, ErrMalformed
	}
	for i < len(d) {
		switch d[i] {
		case 0x21:
			if i+2 > len(d) {
				return nil, ErrMalformed
			}
			end, err := sub(i + 2)
			if err != nil {
				return nil, err
			}
			label := d[i+1]
			keep := label != 0xFE
			if label == 0xFF {
				app := d[i+2 : end]
				keep = len(app) >= 12 && (string(app[1:12]) == "NETSCAPE2.0" || string(app[1:12]) == "ANIMEXTS1.0")
			}
			if keep {
				out = append(out, d[i:end]...)
			}
			i = end
		case 0x2C:
			if i+10 > len(d) {
				return nil, ErrMalformed
			}
			start := i
			flags := d[i+9]
			i += 10
			if flags&0x80 != 0 {
				i += 3 << (int(flags&0x07) + 1)
			}
			end, err := sub(i + 1)
			if err != nil {
				return nil, err
			}
			out = append(out, d[start:end]...)
			i = end
		case 0x3B:
			return append(out, 0x3B), nil
		default:
			return nil, ErrMalformed
		}
	}
	return nil, ErrMalformed
}
