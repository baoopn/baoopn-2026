// Package testimg builds small image fixtures for tests: JPEGs carrying an
// EXIF block (orientation, GPS), PNGs with text chunks, GIFs and WebP
// containers. It is imported by tests only.
package testimg

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
)

// Halves is a w×h image whose left half is red and right half blue.
func Halves(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{R: 255, A: 255}
			if x >= w/2 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// PNG encodes img.
func PNG(img image.Image) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// JPEG encodes img at quality 95.
func JPEG(img image.Image) []byte {
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: 95})
	return b.Bytes()
}

// ExifTIFF is a big-endian TIFF block with Orientation and, when gps is
// true, a GPS IFD holding a latitude reference ("N") and the text "GPSDATA".
func ExifTIFF(orientation int, gps bool) []byte {
	be := binary.BigEndian
	var t []byte
	t = append(t, 'M', 'M', 0, 42, 0, 0, 0, 8)
	n := 1
	if gps {
		n = 2
	}
	t = be.AppendUint16(t, uint16(n))
	// Orientation SHORT
	t = append(t, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, byte(orientation), 0, 0)
	gpsIFD := 8 + 2 + 12*n + 4
	if gps {
		t = append(t, 0x88, 0x25, 0, 4, 0, 0, 0, 1)
		t = be.AppendUint32(t, uint32(gpsIFD))
	}
	t = append(t, 0, 0, 0, 0)
	if gps {
		t = be.AppendUint16(t, 1)
		t = append(t, 0, 1, 0, 2, 0, 0, 0, 2, 'N', 0, 0, 0) // GPSLatitudeRef "N"
		t = append(t, 0, 0, 0, 0)
		t = append(t, []byte("GPSDATA")...)
	}
	return t
}

// WithAPP1 inserts an APP1 segment with body right after the SOI marker.
func WithAPP1(jpg, body []byte) []byte {
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(body)+2))
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	out = append(out, body...)
	return append(out, jpg[2:]...)
}

// JPEGWithExif is JPEG(img) with an Exif APP1 (orientation, optional GPS)
// and an XMP APP1.
func JPEGWithExif(img image.Image, orientation int, gps bool) []byte {
	j := JPEG(img)
	j = WithAPP1(j, []byte("http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta>XMPSECRET</x:xmpmeta>"))
	return WithAPP1(j, append([]byte("Exif\x00\x00"), ExifTIFF(orientation, gps)...))
}

// PNGChunk inserts a chunk (with a valid CRC) right after IHDR.
func PNGChunk(p []byte, typ string, data []byte) []byte {
	ihdrEnd := 8 + 12 + int(binary.BigEndian.Uint32(p[8:]))
	c := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	c = append(c, typ...)
	c = append(c, data...)
	c = binary.BigEndian.AppendUint32(c, crc(append([]byte(typ), data...)))
	out := append([]byte{}, p[:ihdrEnd]...)
	out = append(out, c...)
	return append(out, p[ihdrEnd:]...)
}

func crc(b []byte) uint32 {
	var table [256]uint32
	for i := range table {
		c := uint32(i)
		for range 8 {
			if c&1 != 0 {
				c = 0xEDB88320 ^ (c >> 1)
			} else {
				c >>= 1
			}
		}
		table[i] = c
	}
	c := ^uint32(0)
	for _, x := range b {
		c = table[byte(c)^x] ^ (c >> 8)
	}
	return ^c
}

// GIF encodes frames (one: static; more: animated) of 8×8 paletted images.
func GIF(frames int) []byte {
	g := &gif.GIF{}
	pal := color.Palette{color.Black, color.White}
	for i := range frames {
		img := image.NewPaletted(image.Rect(0, 0, 8, 8), pal)
		img.SetColorIndex(i%8, 0, 1)
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 10)
	}
	var b bytes.Buffer
	_ = gif.EncodeAll(&b, g)
	return b.Bytes()
}

// RIFF builds a WebP container from (fourcc, data) chunks.
func RIFF(chunks ...[2]string) []byte {
	var body []byte
	body = append(body, "WEBP"...)
	for _, c := range chunks {
		body = append(body, c[0]...)
		body = binary.LittleEndian.AppendUint32(body, uint32(len(c[1])))
		body = append(body, c[1]...)
		if len(c[1])%2 == 1 {
			body = append(body, 0)
		}
	}
	out := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)
	return append(out, body...)
}

// PNGSetSize rewrites the IHDR dimensions (and its CRC) of an encoded PNG.
func PNGSetSize(p []byte, w, h uint32) []byte {
	out := append([]byte{}, p...)
	binary.BigEndian.PutUint32(out[16:], w)
	binary.BigEndian.PutUint32(out[20:], h)
	binary.BigEndian.PutUint32(out[29:], crc(out[12:29]))
	return out
}
