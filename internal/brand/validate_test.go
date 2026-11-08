package brand

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/baoopn/baoopn-2026/internal/media"
	"github.com/baoopn/baoopn-2026/internal/mediarefs"
)

func mustSlot(t *testing.T, name string) slotDef {
	t.Helper()
	d, ok := slotByName(name)
	if !ok {
		t.Fatalf("no slot %s", name)
	}
	return d
}

// pngN is an n×n PNG.
func pngN(n int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, n, n)))
	return b.Bytes()
}

// icoOf builds an ICO with one square PNG entry per size (256 → width byte 0).
func icoOf(sizes ...int) []byte {
	var hdr, body bytes.Buffer
	hdr.Write([]byte{0, 0, 1, 0})
	_ = binary.Write(&hdr, binary.LittleEndian, uint16(len(sizes)))
	off := 6 + 16*len(sizes)
	for _, n := range sizes {
		img := pngN(n)
		w := byte(n)
		if n >= 256 {
			w = 0
		}
		hdr.Write([]byte{w, w, 0, 0, 1, 0, 32, 0})
		_ = binary.Write(&hdr, binary.LittleEndian, uint32(len(img)))
		_ = binary.Write(&hdr, binary.LittleEndian, uint32(off+body.Len()))
		body.Write(img)
	}
	return append(hdr.Bytes(), body.Bytes()...)
}

func problemOf(t *testing.T, err error) *Problem {
	t.Helper()
	var p *Problem
	if !errors.As(err, &p) {
		t.Fatalf("err = %v, want a *Problem", err)
	}
	return p
}

func TestDefaultsPassValidation(t *testing.T) {
	for _, d := range slots {
		data, err := defaultFile(d)
		if err != nil {
			t.Fatalf("%s: %v", d.File, err)
		}
		info, err := validate(d, data)
		if err != nil {
			t.Errorf("%s: default %s fails: %v", d.Slot, d.File, err)
		}
		if info.Width < 1 || info.Height < 1 {
			t.Errorf("%s: info %+v", d.Slot, info)
		}
		key := keyFor(d, data)
		if !media.BrandKeyRe.MatchString(key) {
			t.Errorf("%s: key %q does not match BrandKeyRe", d.Slot, key)
		}
		if st := mediarefs.Stems(key); len(st) != 0 {
			t.Errorf("%s: key %q has media stems %v", d.Slot, key, st)
		}
		if st := mediarefs.Stems(`{"custom":{"x":{"key":"` + key + `"}}}`); len(st) != 0 {
			t.Errorf("%s: JSON with key has stems %v", d.Slot, st)
		}
	}
}

func TestICO(t *testing.T) {
	d := mustSlot(t, FaviconICO)
	info, err := validate(d, icoOf(16, 32, 48))
	if err != nil || info != (Info{48, 48}) {
		t.Fatalf("16/32/48: %+v %v", info, err)
	}
	if info, err = validate(d, icoOf(16, 32, 48, 256)); err != nil || info != (Info{256, 256}) {
		t.Fatalf("with 256: %+v %v", info, err)
	}
	p := problemOf(t, func() error { _, err := validate(d, icoOf(16, 32)); return err }())
	if p.Status != 422 || p.Message != "favicon_ico must contain 16×16, 32×32 and 48×48 images" {
		t.Errorf("missing 48: %+v", p)
	}
	good := icoOf(16, 32, 48)
	trunc := good[:6+16*2] // directory cut short
	if p := problemOf(t, func() error { _, err := validate(d, trunc); return err }()); p.Status != 422 {
		t.Errorf("truncated dir: %+v", p)
	}
	oob := append([]byte(nil), good...)
	binary.LittleEndian.PutUint32(oob[6+16*2+12:], uint32(len(oob))) // last entry starts at EOF
	if p := problemOf(t, func() error { _, err := validate(d, oob); return err }()); p.Status != 422 {
		t.Errorf("offset out of bounds: %+v", p)
	}
	zero := append([]byte(nil), good...)
	binary.LittleEndian.PutUint16(zero[4:], 0)
	if p := problemOf(t, func() error { _, err := validate(d, zero); return err }()); p.Status != 422 {
		t.Errorf("zero entries: %+v", p)
	}
	p = problemOf(t, func() error { _, err := validate(d, pngN(48)); return err }())
	if p.Status != http.StatusUnsupportedMediaType || p.Code != "unsupported_type" || p.Message != "favicon_ico needs an ICO file" {
		t.Errorf("png as ico: %+v", p)
	}
}

func TestAppleTouchPNG(t *testing.T) {
	d := mustSlot(t, AppleTouchIcon)
	if info, err := validate(d, pngN(180)); err != nil || info != (Info{180, 180}) {
		t.Fatalf("180: %+v %v", info, err)
	}
	p := problemOf(t, func() error { _, err := validate(d, pngN(192)); return err }())
	if p.Status != 422 || p.Code != "validation" || p.Message != "apple_touch_icon must be 180×180 (got 192×192)" {
		t.Errorf("192: %+v", p)
	}
	if p := problemOf(t, func() error { _, err := validate(d, icoOf(16, 32, 48)); return err }()); p.Status != 415 {
		t.Errorf("ico as png: %+v", p)
	}
	big := append(pngN(180), make([]byte, 600<<10)...)
	if p := problemOf(t, func() error { _, err := validate(d, big); return err }()); p.Status != 413 || p.Code != "too_large" {
		t.Errorf("too large: %+v", p)
	}
}

const svgHead = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 100 100">`

func TestSVGReject(t *testing.T) {
	sq := mustSlot(t, MonogramPrimary)
	wide := mustSlot(t, WordmarkPrimary)
	for name, c := range map[string]struct {
		d    slotDef
		body string
		want string
	}{
		"script":         {sq, svgHead + `<script>alert(1)</script></svg>`, "SVG contains a <script> element"},
		"script caps":    {sq, svgHead + `<SCRIPT>alert(1)</SCRIPT></svg>`, "<SCRIPT>"},
		"onload":         {sq, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1" onload="alert(1)"/>`, "event handler"},
		"foreignObject":  {sq, svgHead + `<foreignObject><div/></foreignObject></svg>`, "<foreignObject>"},
		"iframe":         {sq, svgHead + `<iframe/></svg>`, "<iframe>"},
		"external href":  {sq, svgHead + `<image href="https://evil.example/x.png"/></svg>`, "external resource"},
		"xlink external": {sq, svgHead + `<use xlink:href="other.svg#a"/></svg>`, "external resource"},
		"data svg href":  {sq, svgHead + `<image href="data:image/svg+xml;base64,AAAA"/></svg>`, "external resource"},
		"javascript":     {sq, svgHead + `<a href="#x"><set attributeName="fill" to="java&#x09;script:alert(1)"/></a></svg>`, "javascript:"},
		"doctype":        {sq, `<!DOCTYPE svg [<!ENTITY x "y">]>` + svgHead + `</svg>`, "DOCTYPE"},
		"stylesheet pi":  {sq, `<?xml-stylesheet href="https://evil.example/x.css"?>` + svgHead + `</svg>`, "processing instruction"},
		"@import":        {sq, svgHead + `<style>@import "https://evil.example/x.css";</style></svg>`, "@import"},
		"url http":       {sq, svgHead + `<style>rect{fill:url( "https://evil.example/x" )}</style></svg>`, "url()"},
		"url attr":       {sq, svgHead + `<rect fill="url(http://evil.example/p)"/></svg>`, "url()"},
		"cdata style":    {sq, svgHead + `<style><![CDATA[a{background:url(//evil)}]]></style></svg>`, "url()"},
		"no viewBox":     {sq, `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"/>`, "viewBox"},
		"bad viewBox":    {wide, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 0 10"/>`, "viewBox"},
		"non-square":     {sq, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 320 132"/>`, "square"},
		"not svg root":   {sq, `<html><svg viewBox="0 0 1 1"/></html>`, "root element"},
		"malformed":      {sq, svgHead + `<g></svg>`, "well-formed"},
		"split comment":  {sq, svgHead + `<style>rect{fill:ur<!-- -->l(https://evil/x)}</style></svg>`, "url("},
		"split cdata":    {sq, svgHead + `<style>u<![CDATA[rl(https://evil/x)]]></style></svg>`, "url("},
		"split import":   {sq, svgHead + `<style>@im<!---->port "https://evil/x.css";</style></svg>`, "@import"},
		"escape attr":    {sq, svgHead + `<rect style="fill:u\72l(https://evil/x)"/></svg>`, "CSS escape"},
		"escape import":  {sq, svgHead + `<style>@\69mport "https://evil/x.css";</style></svg>`, "CSS escape"},
		"image-set":      {sq, svgHead + `<rect style="fill:image-set(&quot;https://evil/x.png&quot; 1x)"/></svg>`, "image-set("},
		"image()":        {sq, svgHead + `<style>rect{fill:image("https://evil/x.png")}</style></svg>`, "image("},
		"set href":       {sq, svgHead + `<a><set attributeName="href" to="https://evil/"/></a></svg>`, "animates a link"},
		"animate xlink":  {sq, svgHead + `<image><animate attributeName="xlink:href" values="https://evil/x.png"/></image></svg>`, "animates a link"},
		"animate caps":   {sq, svgHead + `<a><animate attributeName=" HREF " values="#a;#b"/></a></svg>`, "animates a link"},
		"xhtml root": {sq, `<svg xmlns="http://www.w3.org/1999/xhtml" viewBox="0 0 1 1"><form action="https://evil/"/>` +
			`<meta http-equiv="refresh" content="0;url=https://evil/"/></svg>`, "SVG namespace"},
		"no namespace": {sq, `<svg viewBox="0 0 1 1"/>`, "SVG namespace"},
		"xhtml child": {sq, svgHead + `<h:meta xmlns:h="http://www.w3.org/1999/xhtml" http-equiv="refresh" content="0"/></svg>`,
			"another namespace"},
		"element in style url":    {sq, svgHead + `<style>rect{fill:ur<g>x</g>l(https://evil/x)}</style></svg>`, "only text"},
		"element in style import": {sq, svgHead + `<style>@im<g>x</g>port "https://evil/x.css";</style></svg>`, "only text"},
		"escape fill":             {sq, svgHead + `<rect fill="u\72l(https://evil/x)"/></svg>`, "CSS escape"},
		"escape set to":           {sq, svgHead + `<rect><set attributeName="fill" to="u\72l(https://evil/x)"/></rect></svg>`, "CSS escape"},
		"xml:base":                {sq, svgHead + `<g xml:base="https://evil/"><use href="#a"/></g></svg>`, "xml:base"},
	} {
		_, err := validate(c.d, []byte(c.body))
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		p := problemOf(t, err)
		if p.Status != 422 || p.Code != "validation" || !strings.Contains(p.Message, c.want) {
			t.Errorf("%s: %+v, want 422 containing %q", name, p, c.want)
		}
	}
	if p := problemOf(t, func() error { _, err := validate(sq, []byte("hello world")); return err }()); p.Status != 415 {
		t.Errorf("text: %+v", p)
	}
}

func TestSVGAccept(t *testing.T) {
	sq := mustSlot(t, FaviconSVG)
	for name, body := range map[string]string{
		"id href":   svgHead + `<defs><linearGradient id="g"/></defs><rect fill="url(#g)"/><use href="#g"/><use xlink:href="#g"/></svg>`,
		"url quote": svgHead + `<style>rect{fill:url('#g')}</style></svg>`,
		"data png":  svgHead + `<image href="data:image/png;base64,iVBORw0KGgo="/></svg>`,
		"rdf metadata": svgHead + `<metadata><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" ` +
			`xmlns:cc="http://creativecommons.org/ns#" xmlns:dc="http://purl.org/dc/elements/1.1/"><cc:Work><dc:title>x</dc:title>` +
			`</cc:Work></rdf:RDF></metadata><style>/* c */ rect{fill:url(#g)}</style><animate attributeName="opacity" values="0;1"/></svg>`,
		"inkscape": `<?xml version="1.0" encoding="UTF-8"?><!-- Inkscape -->` +
			`<svg xmlns="http://www.w3.org/2000/svg" xmlns:inkscape="http://www.inkscape.org/namespaces/inkscape" ` +
			`xmlns:sodipodi="http://sodipodi.sourceforge.net/DTD/sodipodi-0.dtd" viewBox="0,0,64,64" inkscape:version="1.4" ` +
			`sodipodi:docname="x.svg" inkscape:export-filename="C:\Users\me\icon.png"><sodipodi:namedview inkscape:zoom="1"/><path style="-inkscape-stroke:hairline" d="M0 0"/></svg>`,
	} {
		info, err := validate(sq, []byte(body))
		if err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if info.Width < 1 || info.Width != info.Height {
			t.Errorf("%s: info %+v", name, info)
		}
	}
	wide := mustSlot(t, WordmarkPrimary)
	if info, err := validate(wide, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 320.4 132.6"/>`)); err != nil || info != (Info{320, 133}) {
		t.Errorf("wordmark: %+v %v", info, err)
	}
}
