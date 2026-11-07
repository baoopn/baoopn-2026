// Package imageconv turns an uploaded raster image (JPEG, PNG, static GIF,
// BMP, TIFF, HEIC/HEIF or WebP) into a lossy WebP: decode, apply the EXIF
// orientation, optionally crop, downscale so the longest side fits, and
// encode without metadata. Only the media worker imports it, so its
// WebAssembly-based codecs (gen2brain/webp, gen2brain/heic) stay out of the
// API binary. Everything is pure Go and builds with CGO_ENABLED=0.
package imageconv

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"

	"github.com/gen2brain/heic"
	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"

	"github.com/baoopn/baoopn-2026/internal/imagemeta"
)

// Defaults for Options fields left at zero.
const (
	DefaultMaxDimension = 2400
	DefaultQuality      = 82
	DefaultMaxPixels    = 50_000_000
)

// Crop is a rectangle in original image pixels, after EXIF orientation.
type Crop struct{ X, Y, W, H int }

// Options tune a conversion.
type Options struct {
	MaxDimension int   // longest side of the result; never upscaled
	Quality      int   // lossy WebP quality, 1-100
	MaxPixels    int64 // decompression-bomb guard on the stored dimensions
	Crop         *Crop // nil: the whole image
}

// Result is the encoded WebP.
type Result struct {
	Data          []byte
	Width, Height int
}

// Permanent errors: retrying the same input cannot succeed.
var (
	ErrDecode   = errors.New("the image could not be decoded")
	ErrTooLarge = errors.New("the image has too many pixels")
	ErrCrop     = errors.New("the crop rectangle is outside the image")
)

// Convert converts data, an image of content type ctype (imagemeta constants).
func Convert(data []byte, ctype string, o Options) (Result, error) {
	if o.MaxDimension <= 0 {
		o.MaxDimension = DefaultMaxDimension
	}
	if o.Quality <= 0 || o.Quality > 100 {
		o.Quality = DefaultQuality
	}
	if o.MaxPixels <= 0 {
		o.MaxPixels = DefaultMaxPixels
	}
	if !imagemeta.Raster(ctype) {
		return Result{}, fmt.Errorf("%w: %s is not a convertible image type", ErrDecode, ctype)
	}

	info, err := probe(data, ctype)
	if err != nil {
		return Result{}, err
	}
	if info.Pixels() > o.MaxPixels {
		return Result{}, fmt.Errorf("%w: %d×%d is over the %d pixel limit", ErrTooLarge, info.Width, info.Height, o.MaxPixels)
	}
	w, h := info.Oriented()
	if o.Crop != nil {
		if err := CheckCrop(*o.Crop, w, h); err != nil {
			return Result{}, err
		}
	}

	img, err := decode(data, ctype)
	if err != nil {
		return Result{}, err
	}
	if b := img.Bounds(); b.Dx()*b.Dy() <= 0 || int64(b.Dx())*int64(b.Dy()) > o.MaxPixels {
		return Result{}, fmt.Errorf("%w: decoded size %d×%d", ErrTooLarge, b.Dx(), b.Dy())
	}
	img = Orient(img, info.Orientation)
	if o.Crop != nil {
		c := *o.Crop
		// Check again against the real decoded size (HEIC headers may lack it).
		b := img.Bounds()
		if err := CheckCrop(c, b.Dx(), b.Dy()); err != nil {
			return Result{}, err
		}
		img = subImage(img, image.Rect(b.Min.X+c.X, b.Min.Y+c.Y, b.Min.X+c.X+c.W, b.Min.Y+c.Y+c.H))
	}
	img = Fit(img, o.MaxDimension)

	var buf bytes.Buffer
	if err := webp.Encode(&buf, img, webp.Options{Quality: o.Quality, Method: webp.DefaultMethod}); err != nil {
		return Result{}, fmt.Errorf("webp encode: %w", err)
	}
	b := img.Bounds()
	return Result{Data: buf.Bytes(), Width: b.Dx(), Height: b.Dy()}, nil
}

// CheckCrop validates a crop rectangle against a w×h image.
func CheckCrop(c Crop, w, h int) error {
	if c.X < 0 || c.Y < 0 || c.W < 1 || c.H < 1 || c.X > w-c.W || c.Y > h-c.H {
		return fmt.Errorf("%w: %d,%d %d×%d does not fit in %d×%d", ErrCrop, c.X, c.Y, c.W, c.H, w, h)
	}
	return nil
}

func probe(data []byte, ctype string) (imagemeta.Info, error) {
	info, err := imagemeta.Probe(data, ctype)
	if err != nil {
		return info, fmt.Errorf("%w: %v", ErrDecode, err)
	}
	if ctype == imagemeta.HEIC && !info.SizeKnown {
		cfg, err := heic.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return info, fmt.Errorf("%w: %v", ErrDecode, err)
		}
		info.Width, info.Height, info.SizeKnown = cfg.Width, cfg.Height, true
	}
	return info, nil
}

func decode(data []byte, ctype string) (img image.Image, err error) {
	defer func() {
		if p := recover(); p != nil { // a codec bug on hostile input must not kill the worker
			img, err = nil, fmt.Errorf("%w: decoder panic: %v", ErrDecode, p)
		}
	}()
	if ctype == imagemeta.HEIC {
		img, err = heic.Decode(bytes.NewReader(data))
	} else {
		// png, jpeg, gif (first frame), bmp, tiff and webp are registered by
		// imagemeta (pure Go decoders).
		img, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecode, err)
	}
	return img, nil
}

// subImage crops without copying when the image type allows it.
func subImage(img image.Image, r image.Rectangle) image.Image {
	if s, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return s.SubImage(r)
	}
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), img, r.Min, draw.Src)
	return dst
}

// Fit downscales img with Catmull-Rom so its longest side is at most max.
// It never upscales.
func Fit(img image.Image, max int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return img
	}
	nw, nh := max, max
	if w >= h {
		nh = int(float64(h)*float64(max)/float64(w) + 0.5)
	} else {
		nw = int(float64(w)*float64(max)/float64(h) + 0.5)
	}
	nw, nh = maxInt(nw, 1), maxInt(nh, 1)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Orient applies an EXIF orientation (2-8); 1 or anything else returns img.
func Orient(img image.Image, o int) image.Image {
	if o < 2 || o > 8 {
		return img
	}
	b := img.Bounds()
	src, ok := img.(*image.RGBA)
	if !ok || b.Min != (image.Point{}) {
		src = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	}
	W, H := b.Dx(), b.Dy()
	w, h := W, H
	if o >= 5 {
		w, h = H, W
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		row := dst.Pix[y*dst.Stride : y*dst.Stride+4*w]
		for x := range w {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = W-1-x, y
			case 3:
				sx, sy = W-1-x, H-1-y
			case 4:
				sx, sy = x, H-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, H-1-x
			case 7:
				sx, sy = W-1-y, H-1-x
			case 8:
				sx, sy = W-1-y, x
			}
			si := sy*src.Stride + 4*sx
			copy(row[4*x:4*x+4], src.Pix[si:si+4])
		}
	}
	return dst
}
