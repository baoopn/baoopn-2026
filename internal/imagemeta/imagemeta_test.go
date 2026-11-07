package imagemeta

import (
	"testing"

	"github.com/baoopn/baoopn-2026/internal/testimg"
)

func TestSniffRaster(t *testing.T) {
	for _, c := range []struct {
		name string
		head []byte
		want string
	}{
		{"png", testimg.PNG(testimg.Halves(2, 2)), PNG},
		{"jpeg", testimg.JPEG(testimg.Halves(2, 2)), JPEG},
		{"gif", testimg.GIF(1), GIF},
		{"tiff le", []byte("II*\x00\x08\x00\x00\x00"), TIFF},
		{"tiff be", []byte("MM\x00*\x00\x00\x00\x08"), TIFF},
		{"bmp", []byte("BM\x00\x00\x00\x00\x00\x00\x00\x00\x36\x00\x00\x00\x28\x00"), BMP},
		{"heic", []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"), HEIC},
		{"mif1+heic", []byte("\x00\x00\x00\x18ftypmif1\x00\x00\x00\x00mif1heic"), HEIC},
		{"avif", []byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00avifmif1miaf"), ""},
		{"mif1+avif", []byte("\x00\x00\x00\x18ftypmif1\x00\x00\x00\x00mif1avif"), ""},
		{"text", []byte("hello world"), ""},
	} {
		if got := SniffRaster(c.head); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestOrientation(t *testing.T) {
	img := testimg.Halves(4, 2)
	if o := Orientation(testimg.JPEGWithExif(img, 8, true), JPEG); o != 8 {
		t.Errorf("jpeg: %d", o)
	}
	if o := Orientation(testimg.JPEG(img), JPEG); o != 1 {
		t.Errorf("jpeg without exif: %d", o)
	}
	p := testimg.PNGChunk(testimg.PNG(img), "eXIf", testimg.ExifTIFF(3, false))
	if o := Orientation(p, PNG); o != 3 {
		t.Errorf("png: %d", o)
	}
	w := testimg.RIFF([2]string{"VP8X", "\x08\x00\x00\x00\x00\x00\x00\x00\x00\x00"},
		[2]string{"EXIF", "Exif\x00\x00" + string(testimg.ExifTIFF(6, false))})
	if o := Orientation(w, WebP); o != 6 {
		t.Errorf("webp: %d", o)
	}
	if o := Orientation(testimg.ExifTIFF(5, false), TIFF); o != 5 {
		t.Errorf("tiff: %d", o)
	}
	if o := Orientation(testimg.JPEGWithExif(img, 9, false), JPEG); o != 1 {
		t.Errorf("out of range: %d", o)
	}
}

func TestProbe(t *testing.T) {
	info, err := Probe(testimg.JPEGWithExif(testimg.Halves(40, 20), 6, false), JPEG)
	if err != nil || info.Width != 40 || info.Height != 20 || info.Orientation != 6 || info.Animated {
		t.Fatalf("%+v %v", info, err)
	}
	if w, h := info.Oriented(); w != 20 || h != 40 {
		t.Fatalf("oriented %dx%d", w, h)
	}
	if info, _ := Probe(testimg.GIF(1), GIF); info.Animated {
		t.Error("static gif animated")
	}
	if info, _ := Probe(testimg.GIF(3), GIF); !info.Animated {
		t.Error("animated gif not detected")
	}
	anim := testimg.RIFF([2]string{"VP8X", "\x02\x00\x00\x00\x07\x00\x00\x07\x00\x00"})
	if !webpAnimated(anim) {
		t.Error("animated webp not detected")
	}
	if _, err := Probe([]byte("\x89PNG\r\n\x1a\nbroken"), PNG); err == nil {
		t.Error("broken png accepted")
	}
}
