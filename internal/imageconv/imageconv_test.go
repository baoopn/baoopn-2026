package imageconv

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"os"
	"testing"

	"golang.org/x/image/webp"

	"github.com/baoopn/baoopn-2026/internal/imagemeta"
	"github.com/baoopn/baoopn-2026/internal/testimg"
)

func decodeWebP(t *testing.T, r Result) image.Image {
	t.Helper()
	if len(r.Data) < 12 || string(r.Data[:4]) != "RIFF" || string(r.Data[8:12]) != "WEBP" {
		t.Fatalf("not a WebP: % x", r.Data[:min(16, len(r.Data))])
	}
	if bytes.Contains(r.Data, []byte("EXIF")) || bytes.Contains(r.Data, []byte("XMP ")) {
		t.Fatal("metadata chunk in output")
	}
	img, err := webp.Decode(bytes.NewReader(r.Data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != r.Width || b.Dy() != r.Height {
		t.Fatalf("result says %dx%d, image is %v", r.Width, r.Height, b)
	}
	return img
}

// isRed / isBlue tolerate lossy compression.
func isRed(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	return r > 0xB000 && g < 0x5000 && b < 0x5000
}

func isBlue(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	return b > 0xB000 && r < 0x5000 && g < 0x5000
}

func TestConvertPNG(t *testing.T) {
	res, err := Convert(testimg.PNG(testimg.Halves(64, 32)), imagemeta.PNG, Options{})
	if err != nil {
		t.Fatal(err)
	}
	img := decodeWebP(t, res)
	if res.Width != 64 || res.Height != 32 || !isRed(img, 8, 16) || !isBlue(img, 56, 16) {
		t.Fatalf("got %dx%d", res.Width, res.Height)
	}
}

// Orientation 6 (rotate 90° clockwise): the red left half ends up on top.
func TestConvertJPEGOrientation(t *testing.T) {
	src := testimg.JPEGWithExif(testimg.Halves(64, 32), 6, true)
	if o := imagemeta.Orientation(src, imagemeta.JPEG); o != 6 {
		t.Fatalf("fixture orientation %d", o)
	}
	res, err := Convert(src, imagemeta.JPEG, Options{})
	if err != nil {
		t.Fatal(err)
	}
	img := decodeWebP(t, res)
	if res.Width != 32 || res.Height != 64 {
		t.Fatalf("got %dx%d, want 32x64", res.Width, res.Height)
	}
	if !isRed(img, 16, 8) || !isBlue(img, 16, 56) {
		t.Fatal("not rotated clockwise")
	}
	if bytes.Contains(res.Data, []byte("GPSDATA")) || bytes.Contains(res.Data, []byte("XMPSECRET")) {
		t.Fatal("metadata leaked")
	}
}

func TestOrientAll(t *testing.T) {
	// 3×2 source with distinct pixels; check where the top-left one lands.
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	tl := src.RGBAAt(0, 0)
	want := map[int]image.Point{1: {0, 0}, 2: {2, 0}, 3: {2, 1}, 4: {0, 1}, 5: {0, 0}, 6: {1, 0}, 7: {1, 2}, 8: {0, 2}}
	for o, p := range want {
		out := Orient(src, o).(*image.RGBA)
		w, h := out.Bounds().Dx(), out.Bounds().Dy()
		if (o >= 5) != (w == 2 && h == 3) {
			t.Errorf("orientation %d: size %dx%d", o, w, h)
		}
		if out.RGBAAt(p.X, p.Y) != tl {
			t.Errorf("orientation %d: top-left pixel not at %v", o, p)
		}
	}
}

// testdata/tiny.heic is test_exif.heic from github.com/gen2brain/heic v0.7.2
// (MIT License, Copyright (c) 2024 gen2brain).
func TestConvertHEIC(t *testing.T) {
	data, err := os.ReadFile("testdata/tiny.heic")
	if err != nil {
		t.Skipf("HEIC fixture missing: %v", err)
	}
	if got := imagemeta.SniffRaster(data[:64]); got != imagemeta.HEIC {
		t.Fatalf("sniffed %q", got)
	}
	res, err := Convert(data, imagemeta.HEIC, Options{})
	if err != nil {
		t.Fatal(err)
	}
	decodeWebP(t, res)
	// 640×480 stored with a 90° irot: the decoder applies it.
	if res.Width != 480 || res.Height != 640 {
		t.Fatalf("got %dx%d", res.Width, res.Height)
	}
}

func TestCrop(t *testing.T) {
	// Crop coordinates are after orientation: the oriented image is 32×64.
	src := testimg.JPEGWithExif(testimg.Halves(64, 32), 6, false)
	res, err := Convert(src, imagemeta.JPEG, Options{Crop: &Crop{X: 0, Y: 32, W: 32, H: 32}})
	if err != nil {
		t.Fatal(err)
	}
	img := decodeWebP(t, res)
	if res.Width != 32 || res.Height != 32 || !isBlue(img, 16, 16) {
		t.Fatalf("crop: %dx%d", res.Width, res.Height)
	}
	for _, c := range []Crop{{0, 0, 64, 32}, {-1, 0, 4, 4}, {0, 0, 0, 4}, {30, 0, 4, 4}, {0, 61, 4, 4}} {
		if _, err := Convert(src, imagemeta.JPEG, Options{Crop: &c}); !errors.Is(err, ErrCrop) {
			t.Errorf("crop %+v: err %v", c, err)
		}
	}
}

func TestDownscaleNeverUpscale(t *testing.T) {
	src := testimg.PNG(testimg.Halves(300, 100))
	res, err := Convert(src, imagemeta.PNG, Options{MaxDimension: 120})
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 120 || res.Height != 40 {
		t.Fatalf("got %dx%d", res.Width, res.Height)
	}
	res, err = Convert(src, imagemeta.PNG, Options{MaxDimension: 2400})
	if err != nil || res.Width != 300 || res.Height != 100 {
		t.Fatalf("upscaled? %dx%d %v", res.Width, res.Height, err)
	}
	tall := testimg.PNG(testimg.Halves(10, 500))
	if res, _ := Convert(tall, imagemeta.PNG, Options{MaxDimension: 100}); res.Width != 2 || res.Height != 100 {
		t.Fatalf("tall: %dx%d", res.Width, res.Height)
	}
}

func TestStaticGIF(t *testing.T) {
	res, err := Convert(testimg.GIF(1), imagemeta.GIF, Options{})
	if err != nil || res.Width != 8 || res.Height != 8 {
		t.Fatalf("gif: %v %dx%d", err, res.Width, res.Height)
	}
}

// A PNG header claiming 100000×100000 is refused before any pixel is decoded.
func TestBombGuard(t *testing.T) {
	p := testimg.PNGSetSize(testimg.PNG(testimg.Halves(4, 4)), 100000, 100000)
	if _, err := Convert(p, imagemeta.PNG, Options{}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err %v", err)
	}
	if _, err := Convert(testimg.PNG(testimg.Halves(100, 100)), imagemeta.PNG, Options{MaxPixels: 9999}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limit: %v", err)
	}
}

func TestUndecodable(t *testing.T) {
	bad := append(testimg.PNG(testimg.Halves(8, 8))[:40], make([]byte, 20)...)
	if _, err := Convert(bad, imagemeta.PNG, Options{}); !errors.Is(err, ErrDecode) {
		t.Fatalf("err %v", err)
	}
	if _, err := Convert([]byte("<svg/>"), "image/svg+xml", Options{}); !errors.Is(err, ErrDecode) {
		t.Fatalf("svg: %v", err)
	}
}

// Semi-transparent pixels keep straight (non-premultiplied) color: a white
// logo edge at half alpha stays white, not grey. The downscale goes through
// Fit, which yields *image.RGBA (premultiplied), the path that used to halo.
func TestConvertKeepsStraightAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := 32; x < 64; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 128})
		}
	}
	res, err := Convert(testimg.PNG(src), imagemeta.PNG, Options{MaxDimension: 32})
	if err != nil {
		t.Fatal(err)
	}
	img := decodeWebP(t, res)
	if _, _, _, a := img.At(4, 16).RGBA(); a != 0 {
		t.Fatalf("transparent pixel has alpha %d", a>>8)
	}
	half := color.NRGBAModel.Convert(img.At(26, 16)).(color.NRGBA)
	if half.A < 116 || half.A > 140 || half.R < 235 || half.G < 235 || half.B < 235 {
		t.Fatalf("half-alpha white pixel is %+v", half)
	}
}
