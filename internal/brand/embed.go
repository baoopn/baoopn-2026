package brand

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
)

// defaultsFS holds the default brand files, one per slot (slots.File).
//
//go:embed defaults
var defaultsFS embed.FS

// Slot names, in display order.
const (
	FaviconICO      = "favicon_ico"
	FaviconSVG      = "favicon_svg"
	AppleTouchIcon  = "apple_touch_icon"
	WordmarkPrimary = "wordmark_primary"
	MonogramPrimary = "monogram_primary"
	MonogramInverse = "monogram_inverse"
	EmailLogo       = "email_logo"
)

const (
	typeICO = "image/x-icon"
	typePNG = "image/png"
	typeSVG = "image/svg+xml"
)

// slotDef describes one replaceable brand asset.
type slotDef struct {
	Slot    string
	File    string // default file in defaults/
	Stem    string // key stem: brand/<stem>.<h16>.<ext>
	Ext     string // ico, png or svg
	Type    string // content type
	Max     int    // upload limit in bytes
	Square  bool   // SVG viewBox must be square
	PNG     pngRule
	Label   string
	Kind    string // icon or logo
	Rule    string
	Preview string // light, dark or both
	Accept  []string
}

// pngRule bounds a PNG slot's size in px and its aspect ratio (width /
// height, as MinAspect and MaxAspect in thousandths).
type pngRule struct {
	MinW, MaxW, MinH, MaxH int
	MinAspect, MaxAspect   int
}

var slots = []slotDef{
	{Slot: FaviconICO, File: "favicon.ico", Stem: "favicon", Ext: "ico", Type: typeICO, Max: 256 << 10,
		Label: "Favicon (ICO)", Kind: "icon", Rule: "ICO with 16×16, 32×32 and 48×48 images", Preview: "both",
		Accept: []string{"image/x-icon", "image/vnd.microsoft.icon", ".ico"}},
	{Slot: FaviconSVG, File: "favicon.svg", Stem: "favicon", Ext: "svg", Type: typeSVG, Max: 256 << 10, Square: true,
		Label: "Favicon (SVG)", Kind: "icon", Rule: "SVG with a square viewBox", Preview: "both",
		Accept: []string{"image/svg+xml"}},
	{Slot: AppleTouchIcon, File: "apple-touch-icon.png", Stem: "apple-touch-icon", Ext: "png", Type: typePNG, Max: 512 << 10,
		PNG:   pngRule{MinW: 180, MaxW: 180, MinH: 180, MaxH: 180},
		Label: "Apple touch icon", Kind: "icon", Rule: "PNG, exactly 180×180 px", Preview: "both",
		Accept: []string{"image/png"}},
	{Slot: WordmarkPrimary, File: "wordmark-primary.svg", Stem: "wordmark-primary", Ext: "svg", Type: typeSVG, Max: 256 << 10,
		Label: "Wordmark", Kind: "logo", Rule: "SVG with a viewBox", Preview: "both",
		Accept: []string{"image/svg+xml"}},
	{Slot: MonogramPrimary, File: "monogram-primary.svg", Stem: "monogram-primary", Ext: "svg", Type: typeSVG, Max: 256 << 10, Square: true,
		Label: "Monogram", Kind: "logo", Rule: "SVG with a square viewBox", Preview: "both",
		Accept: []string{"image/svg+xml"}},
	{Slot: MonogramInverse, File: "monogram-inverse.svg", Stem: "monogram-inverse", Ext: "svg", Type: typeSVG, Max: 256 << 10, Square: true,
		Label: "Monogram (inverse, for dark backgrounds)", Kind: "logo", Rule: "SVG with a square viewBox", Preview: "dark",
		Accept: []string{"image/svg+xml"}},
	{Slot: EmailLogo, File: "email-logo.png", Stem: "email-logo", Ext: "png", Type: typePNG, Max: 256 << 10,
		PNG:   pngRule{MinW: 240, MaxW: 1200, MinH: 60, MaxH: 600, MinAspect: 1500, MaxAspect: 4000},
		Label: "Email logo", Kind: "logo", Rule: "PNG, 240–1200 px wide, landscape (shown 48 px tall in emails)", Preview: "both",
		Accept: []string{"image/png"}},
}

func slotByName(name string) (slotDef, bool) {
	for _, d := range slots {
		if d.Slot == name {
			return d, true
		}
	}
	return slotDef{}, false
}

// keyFor is the content-addressed key of data in slot d.
func keyFor(d slotDef, data []byte) string {
	sum := sha256.Sum256(data)
	return "brand/" + d.Stem + "." + hex.EncodeToString(sum[:])[:16] + "." + d.Ext
}

// defaultFile returns the embedded default bytes of slot d.
func defaultFile(d slotDef) ([]byte, error) { return defaultsFS.ReadFile("defaults/" + d.File) }
