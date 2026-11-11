package imagestrip

import (
	"bytes"
	"encoding/binary"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/baoopn/baoopn-2026/internal/imagemeta"
	"github.com/baoopn/baoopn-2026/internal/testimg"
)

func TestJPEGWithGPS(t *testing.T) {
	src := testimg.JPEGWithExif(testimg.Halves(16, 8), 6, true)
	if !bytes.Contains(src, []byte("GPSDATA")) || !bytes.Contains(src, []byte("XMPSECRET")) {
		t.Fatal("fixture has no metadata")
	}
	out, err := Strip(src, "image/jpeg", imagemeta.Orientation(src, imagemeta.JPEG))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"GPSDATA", "XMPSECRET", "http://ns.adobe.com"} {
		if bytes.Contains(out, []byte(s)) {
			t.Errorf("%q still present", s)
		}
	}
	if o := imagemeta.Orientation(out, imagemeta.JPEG); o != 6 {
		t.Errorf("orientation lost: %d", o)
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("stripped jpeg does not decode: %v", err)
	}
	// Upright photos keep no EXIF at all.
	plain, err := JPEG(testimg.JPEGWithExif(testimg.Halves(16, 8), 1, true), 1)
	if err != nil || bytes.Contains(plain, []byte("Exif")) {
		t.Fatalf("orientation 1: %v", err)
	}
	if _, err := JPEG([]byte("not a jpeg"), 1); err == nil {
		t.Error("garbage accepted")
	}
}

func TestPNG(t *testing.T) {
	p := testimg.PNG(testimg.Halves(8, 8))
	p = testimg.PNGChunk(p, "tEXt", []byte("Comment\x00SECRETTEXT"))
	p = testimg.PNGChunk(p, "eXIf", testimg.ExifTIFF(1, true))
	p = testimg.PNGChunk(p, "iTXt", []byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00XMPSECRET"))
	p = testimg.PNGChunk(p, "caBX", []byte("C2PAMANIFEST")) // C2PA content credentials
	p = testimg.PNGChunk(p, "prVt", []byte("UNKNOWNCHUNK")) // unknown: dropped too
	p = testimg.PNGChunk(p, "pHYs", make([]byte, 9))        // kept
	out, err := Strip(p, "image/png", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"SECRETTEXT", "GPSDATA", "XMPSECRET", "eXIf", "tEXt", "iTXt", "caBX",
		"C2PAMANIFEST", "UNKNOWNCHUNK"} {
		if bytes.Contains(out, []byte(s)) {
			t.Errorf("%q still present", s)
		}
	}
	if !bytes.Contains(out, []byte("pHYs")) {
		t.Error("pHYs dropped")
	}
	if _, err := png.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("stripped png does not decode: %v", err)
	}
}

func TestWebP(t *testing.T) {
	w := testimg.RIFF([2]string{"VP8X", "\x0c\x00\x00\x00\x07\x00\x00\x07\x00\x00"},
		[2]string{"VP8 ", "fakeframe"}, [2]string{"EXIF", "GPSDATA"}, [2]string{"XMP ", "XMPSECRET"})
	out, err := Strip(w, "image/webp", 1)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("GPSDATA")) || bytes.Contains(out, []byte("XMPSECRET")) ||
		!bytes.Contains(out, []byte("fakeframe")) {
		t.Fatalf("% x", out)
	}
	if out[20]&0x0c != 0 {
		t.Error("VP8X metadata flags still set")
	}
	if int(out[4])+8 != len(out) {
		t.Errorf("RIFF size %d for %d bytes", out[4], len(out))
	}
}

// Only known image chunks survive, and bytes past the RIFF size are dropped.
func TestWebPAllowlist(t *testing.T) {
	w := testimg.RIFF([2]string{"VP8X", "\x2c\x00\x00\x00\x07\x00\x00\x07\x00\x00"},
		[2]string{"ICCP", "iccprofile"}, [2]string{"VP8 ", "fakeframe"},
		[2]string{"C2PA", "C2PAMANIFEST"}, [2]string{"zzzz", "UNKNOWNCHUNK"})
	w = append(w, "TRAILINGJUNK"...)
	out, err := Strip(w, "image/webp", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"C2PAMANIFEST", "UNKNOWNCHUNK", "TRAILINGJUNK"} {
		if bytes.Contains(out, []byte(s)) {
			t.Errorf("%q still present", s)
		}
	}
	if !bytes.Contains(out, []byte("iccprofile")) || !bytes.Contains(out, []byte("fakeframe")) {
		t.Fatalf("image chunks dropped: % x", out)
	}
	if out[20] != 0x20 {
		t.Errorf("VP8X flags = %#x, want only ICC", out[20])
	}
	if int(binary.LittleEndian.Uint32(out[4:]))+8 != len(out) {
		t.Errorf("RIFF size %d for %d bytes", out[4], len(out))
	}
	if _, err := WebP([]byte("RIFF\x02\x00\x00\x00WEBP")); err == nil {
		t.Error("RIFF size below the header accepted")
	}
}

func TestGIF(t *testing.T) {
	g := testimg.GIF(2)
	// Insert a comment extension before the trailer.
	g = append(append(g[:len(g)-1:len(g)-1], 0x21, 0xFE, 6, 'S', 'E', 'C', 'R', 'E', 'T', 0), 0x3B)
	out, err := Strip(g, "image/gif", 1)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("SECRET")) || !bytes.Contains(out, []byte("NETSCAPE2.0")) {
		t.Fatal("comment kept or loop extension dropped")
	}
	if a, err := gif.DecodeAll(bytes.NewReader(out)); err != nil || len(a.Image) != 2 {
		t.Fatalf("stripped gif: %v", err)
	}
}

func TestBrowserSafe(t *testing.T) {
	for ct, want := range map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true,
		"image/webp": true, "image/heic": false, "image/tiff": false, "image/bmp": false} {
		if BrowserSafe(ct) != want {
			t.Errorf("%s", ct)
		}
	}
}
