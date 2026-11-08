package brand

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/png"
	"net/http"
)

// Problem is an upload the API rejects: HTTP status, error code and message.
type Problem struct {
	Status  int
	Code    string
	Message string
}

func (p *Problem) Error() string { return p.Message }

func invalid(format string, args ...any) *Problem {
	return &Problem{Status: http.StatusUnprocessableEntity, Code: "validation", Message: fmt.Sprintf(format, args...)}
}

// Info is what validation learns about a file.
type Info struct{ Width, Height int }

var pngSig = []byte("\x89PNG\r\n\x1a\n")

// detect sniffs the type from the bytes (never the file name): ICO, PNG,
// SVG (text starting with '<' that has an <svg tag) or "".
func detect(data []byte) string {
	switch {
	case len(data) >= 4 && bytes.Equal(data[:4], []byte{0, 0, 1, 0}):
		return typeICO
	case bytes.HasPrefix(data, pngSig):
		return typePNG
	}
	t := bytes.TrimLeft(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), " \t\r\n")
	if bytes.HasPrefix(t, []byte("<")) && bytes.Contains(bytes.ToLower(t), []byte("<svg")) {
		return typeSVG
	}
	return ""
}

var typeNames = map[string]string{typeICO: "an ICO", typePNG: "a PNG", typeSVG: "an SVG"}

// validate checks data for slot d. It never rewrites the file.
func validate(d slotDef, data []byte) (Info, error) {
	if len(data) > d.Max {
		return Info{}, &Problem{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
			Message: fmt.Sprintf("%s must be at most %d KiB", d.Slot, d.Max>>10)}
	}
	if detect(data) != d.Type {
		return Info{}, &Problem{Status: http.StatusUnsupportedMediaType, Code: "unsupported_type",
			Message: fmt.Sprintf("%s needs %s file", d.Slot, typeNames[d.Type])}
	}
	switch d.Type {
	case typeICO:
		return validateICO(d, data)
	case typePNG:
		return validatePNG(d, data)
	default:
		return validateSVG(d, data)
	}
}

func validateICO(d slotDef, data []byte) (Info, error) {
	if len(data) < 6 {
		return Info{}, invalid("%s: the ICO header is truncated", d.Slot)
	}
	n := int(binary.LittleEndian.Uint16(data[4:6]))
	if n < 1 || n > 16 {
		return Info{}, invalid("%s must hold 1 to 16 images (got %d)", d.Slot, n)
	}
	dirEnd := 6 + 16*n
	if len(data) < dirEnd {
		return Info{}, invalid("%s: the ICO directory is truncated", d.Slot)
	}
	have := map[int]bool{}
	var best Info
	for i := range n {
		e := data[6+16*i : 6+16*(i+1)]
		w, h := int(e[0]), int(e[1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		size := uint64(binary.LittleEndian.Uint32(e[8:12]))
		off := uint64(binary.LittleEndian.Uint32(e[12:16]))
		if size == 0 || off < uint64(dirEnd) || off+size > uint64(len(data)) {
			return Info{}, invalid("%s: image %d lies outside the file", d.Slot, i+1)
		}
		if w == h {
			have[w] = true
		}
		if w*h > best.Width*best.Height {
			best = Info{Width: w, Height: h}
		}
	}
	if !have[16] || !have[32] || !have[48] {
		return Info{}, invalid("%s must contain 16×16, 32×32 and 48×48 images", d.Slot)
	}
	return best, nil
}

func validatePNG(d slotDef, data []byte) (Info, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Info{}, invalid("%s: the PNG header could not be read", d.Slot)
	}
	if cfg.Width != 180 || cfg.Height != 180 {
		return Info{}, invalid("%s must be 180×180 (got %d×%d)", d.Slot, cfg.Width, cfg.Height)
	}
	return Info{Width: cfg.Width, Height: cfg.Height}, nil
}
