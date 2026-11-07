package imagestrip

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"testing"

	"github.com/baoopn/baoopn-2026/internal/testimg"
)

// seg builds a marker segment with a length field.
func seg(m byte, body ...byte) []byte {
	s := []byte{0xFF, m, 0, 0}
	binary.BigEndian.PutUint16(s[2:], uint16(len(body)+2))
	return append(s, body...)
}

func cat(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

// mp4 is a fake motion-photo trailer with a GPS-looking payload.
var mp4 = cat([]byte{0, 0, 0, 0x18}, []byte("ftypmp42"), make([]byte, 12),
	[]byte{0, 0, 0, 0x14}, []byte("moov"), []byte("GPS:52.37,4.89"))

func TestJPEGDropsTrailer(t *testing.T) {
	base := testimg.JPEG(testimg.Halves(16, 8))
	out, err := JPEG(cat(base, mp4), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, base) {
		t.Errorf("output is not the bare JPEG: %d bytes, want %d", len(out), len(base))
	}
	if bytes.Contains(out, []byte("ftyp")) || bytes.Contains(out, []byte("GPS:")) {
		t.Error("trailer kept")
	}
}

func TestJPEGDropsMPF(t *testing.T) {
	base := testimg.JPEG(testimg.Halves(16, 8))
	mpf := seg(0xE2, []byte("MPF\x00MM\x00\x2a\x00\x00\x00\x08")...)
	src := cat(base[:2], mpf, base[2:], testimg.JPEG(testimg.Halves(8, 8)))
	out, err := JPEG(src, 1)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("MPF\x00")) {
		t.Error("MPF segment kept")
	}
	if c := bytes.Count(out, []byte{0xFF, 0xD8}); c != 1 {
		t.Errorf("%d SOI markers, want 1", c)
	}
	if !bytes.Equal(out, base) {
		t.Error("output is not the first image alone")
	}
}

func TestJPEGEntropyEscapes(t *testing.T) {
	head := cat([]byte{0xFF, 0xD8}, seg(0xDA, 1, 1, 0, 0, 63, 0))
	data := []byte{0x12, 0xFF, 0x00, 0x34, 0xFF, 0xD3, 0x56, 0xFF, 0xD7, 0x78}
	src := cat(head, data, []byte{0xFF, 0xFF, 0xFF, 0xD9}, mp4)
	out, err := JPEG(src, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := cat(head, data, []byte{0xFF, 0xD9}); !bytes.Equal(out, want) {
		t.Errorf("got  % x\nwant % x", out, want)
	}
}

// progressive is a hand-built 8x8 grey progressive JPEG: a DC scan at
// Al=1, an AC scan, a COM, then a DC refinement scan.
func progressive(t *testing.T) []byte {
	t.Helper()
	q := make([]byte, 65) // 8-bit table 0, all ones
	for i := 1; i < len(q); i++ {
		q[i] = 1
	}
	// Two 1-bit codes: symbol 0 is "0", symbol 1 is "1".
	huff := func(class byte) []byte {
		b := []byte{class << 4, 2}
		b = append(b, make([]byte, 15)...)
		return append(b, 0, 1)
	}
	return cat([]byte{0xFF, 0xD8},
		seg(0xDB, q...),
		seg(0xC2, 8, 0, 8, 0, 8, 1, 1, 0x11, 0),
		seg(0xC4, huff(0)...),
		seg(0xDA, 1, 1, 0x00, 0, 0, 0x01), []byte{0x7F}, // DC first, diff 0
		seg(0xC4, huff(1)...),
		seg(0xDA, 1, 1, 0x00, 1, 63, 0x00), []byte{0x7F}, // AC: EOB
		seg(0xFE, []byte("secret")...),
		seg(0xDA, 1, 1, 0x00, 0, 0, 0x10), []byte{0x7F}, // DC refine: bit 0
		[]byte{0xFF, 0xD9})
}

func TestJPEGProgressive(t *testing.T) {
	src := progressive(t)
	if _, err := jpeg.Decode(bytes.NewReader(src)); err != nil {
		t.Fatalf("fixture does not decode: %v", err)
	}
	out, err := JPEG(cat(src, mp4), 1)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("secret")) {
		t.Error("COM between scans kept")
	}
	if c := bytes.Count(out, []byte{0xFF, 0xDA}); c != 3 {
		t.Errorf("%d scans, want 3", c)
	}
	if c := bytes.Count(out, []byte{0xFF, 0xC4}); c != 2 {
		t.Errorf("%d DHT, want 2", c)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("stripped progressive does not decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 8 || b.Dy() != 8 {
		t.Errorf("bounds %v", b)
	}
}

func TestJPEGTruncated(t *testing.T) {
	base := testimg.JPEG(testimg.Halves(16, 8))
	for _, cut := range []int{2, 3, 20} {
		out, err := JPEG(base[:len(base)-cut], 1)
		if err != nil {
			t.Fatalf("cut %d: %v", cut, err)
		}
		if !bytes.HasSuffix(out, []byte{0xFF, 0xD9}) || bytes.HasSuffix(out, []byte{0xFF, 0xFF, 0xD9}) {
			t.Errorf("cut %d: ends % x", cut, out[len(out)-4:])
		}
	}
	// A table cut short after the first scan is still malformed.
	src := cat([]byte{0xFF, 0xD8}, seg(0xDA, 1, 1, 0, 0, 63, 0), []byte{0x12, 0xFF, 0xC4, 0, 40, 1})
	if _, err := JPEG(src, 1); err != ErrMalformed {
		t.Errorf("short DHT: %v", err)
	}
	src = cat([]byte{0xFF, 0xD8}, seg(0xDA, 1, 1, 0, 0, 63, 0), []byte{0x12, 0xFF, 0x01})
	if _, err := JPEG(src, 1); err != ErrMalformed {
		t.Errorf("unknown marker: %v", err)
	}
}

func TestJPEGRoundTrip(t *testing.T) {
	src := cat(testimg.JPEGWithExif(testimg.Halves(24, 16), 6, true), mp4)
	out, err := JPEG(src, 6)
	if err != nil {
		t.Fatal(err)
	}
	a, err := jpeg.Decode(bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	b, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	ya, yb := a.(*image.YCbCr), b.(*image.YCbCr)
	if !bytes.Equal(ya.Y, yb.Y) || !bytes.Equal(ya.Cb, yb.Cb) || !bytes.Equal(ya.Cr, yb.Cr) {
		t.Error("pixels changed")
	}
	if bytes.Contains(out, []byte("GPS")) || bytes.Contains(out, []byte("ftyp")) {
		t.Error("metadata kept")
	}
}
