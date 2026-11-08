package brand

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Elements that run code or embed other documents.
var badElements = map[string]bool{
	"script": true, "foreignobject": true, "iframe": true, "embed": true, "object": true,
	"audio": true, "video": true, "canvas": true, "handler": true, "listener": true,
}

var dataImageRe = regexp.MustCompile(`^data:image/(png|jpeg|webp|gif);base64,`)

const (
	svgNS = "http://www.w3.org/2000/svg"
	xmlNS = "http://www.w3.org/XML/1998/namespace"
)

// Namespaces elements may use besides SVG: editor and metadata only.
var okNamespaces = map[string]bool{
	svgNS: true,
	"http://www.inkscape.org/namespaces/inkscape":        true,
	"http://sodipodi.sourceforge.net/DTD/sodipodi-0.dtd": true,
	"http://www.w3.org/1999/02/22-rdf-syntax-ns#":        true,
	"http://creativecommons.org/ns#":                     true,
	"http://web.resource.org/cc/":                        true,
	"http://purl.org/dc/elements/1.1/":                   true,
}

// validateSVG rejects (never rewrites) an SVG that could run script or load
// anything: see the SVG rules in docs/api.md.
func validateSVG(d slotDef, data []byte) (Info, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	var info Info
	depth, roots := 0, 0
	inStyle := 0
	var style strings.Builder // all text of the open <style>, across comments and CDATA
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Info{}, invalid("%s: the SVG is not well-formed XML", d.Slot)
		}
		switch t := tok.(type) {
		case xml.ProcInst:
			if t.Target != "xml" {
				return Info{}, invalid("SVG contains a processing instruction (<?%s?>)", t.Target)
			}
		case xml.Directive:
			return Info{}, invalid("SVG contains a DOCTYPE or ENTITY declaration")
		case xml.StartElement:
			local := strings.ToLower(t.Name.Local)
			if depth == 0 {
				roots++
				if roots > 1 || t.Name.Local != "svg" || t.Name.Space != svgNS {
					return Info{}, invalid("%s: the root element must be <svg> in the SVG namespace", d.Slot)
				}
				vb, ok := viewBox(t.Attr)
				if !ok {
					return Info{}, invalid("%s: the SVG needs a valid viewBox", d.Slot)
				}
				if d.Square && vb[2] != vb[3] {
					return Info{}, invalid("%s: the viewBox must be square (got %g×%g)", d.Slot, vb[2], vb[3])
				}
				info = Info{Width: max(1, int(math.Round(vb[2]))), Height: max(1, int(math.Round(vb[3])))}
			}
			if !okNamespaces[t.Name.Space] {
				return Info{}, invalid("SVG contains an element from another namespace (<%s>)", t.Name.Local)
			}
			if badElements[local] {
				return Info{}, invalid("SVG contains a <%s> element", t.Name.Local)
			}
			for _, a := range t.Attr {
				if err := checkAttr(a); err != nil {
					return Info{}, err
				}
			}
			if local == "style" {
				if inStyle == 0 {
					style.Reset()
				}
				inStyle++
			}
			depth++
		case xml.EndElement:
			depth--
			if strings.ToLower(t.Name.Local) == "style" && inStyle > 0 {
				inStyle--
				if inStyle == 0 {
					if msg := dangerousCSS(style.String()); msg != "" {
						return Info{}, invalid("SVG style %s", msg)
					}
				}
			}
		case xml.CharData:
			if inStyle > 0 {
				style.Write(t)
			}
		}
	}
	if roots == 0 {
		return Info{}, invalid("%s: the file has no <svg> element", d.Slot)
	}
	return info, nil
}

func checkAttr(a xml.Attr) error {
	local := strings.ToLower(a.Name.Local)
	if strings.HasPrefix(local, "on") {
		return invalid("SVG contains an event handler attribute (%s)", a.Name.Local)
	}
	if local == "base" && (a.Name.Space == xmlNS || a.Name.Space == "xml") {
		return invalid("SVG contains an xml:base attribute")
	}
	if local == "href" {
		v := strings.TrimSpace(a.Value)
		if !strings.HasPrefix(v, "#") && !dataImageRe.MatchString(v) {
			return invalid("SVG links to an external resource (href)")
		}
	}
	if local == "attributename" {
		// set/animate* could rewrite a link after the checks above.
		v := strings.ToLower(strings.TrimSpace(a.Value))
		if v == "href" || strings.HasSuffix(v, ":href") {
			return invalid("SVG animates a link (attributeName=%q)", a.Value)
		}
	}
	msg := dangerous(a.Value)
	if local == "style" && a.Name.Space == "" {
		msg = dangerousCSS(a.Value)
	}
	if msg != "" {
		return invalid("SVG attribute %s %s", a.Name.Local, msg)
	}
	return nil
}

// dangerousCSS is dangerous plus no CSS escapes, which could hide the rest.
func dangerousCSS(s string) string {
	if strings.Contains(s, "\\") {
		return "contains a CSS escape (backslash)"
	}
	return dangerous(s)
}

// dangerous returns why a CSS or attribute text is refused, or "".
func dangerous(s string) string {
	t := strings.ToLower(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s))
	switch {
	case strings.Contains(t, "javascript:"):
		return "contains a javascript: URL"
	case strings.Contains(t, "@import"):
		return "contains @import"
	}
	for _, fn := range []string{"url(", "image(", "image-set("} {
		for rest := t; ; {
			i := strings.Index(rest, fn)
			if i < 0 {
				break
			}
			rest = rest[i+len(fn):]
			if u := strings.TrimLeft(rest, `"'`); !strings.HasPrefix(u, "#") {
				return "references an external resource (" + fn + ")"
			}
		}
	}
	return ""
}

// viewBox parses the root's viewBox: four numbers, width and height > 0.
func viewBox(attrs []xml.Attr) ([4]float64, bool) {
	var vb [4]float64
	for _, a := range attrs {
		if a.Name.Local != "viewBox" || a.Name.Space != "" {
			continue
		}
		parts := strings.FieldsFunc(a.Value, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
		if len(parts) != 4 {
			return vb, false
		}
		for i, p := range parts {
			f, err := strconv.ParseFloat(p, 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return vb, false
			}
			vb[i] = f
		}
		return vb, vb[2] > 0 && vb[3] > 0
	}
	return vb, false
}
