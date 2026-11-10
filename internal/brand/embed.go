package brand

import "embed"

// defaultsFS holds the default brand files, one per slot (slotDef.File):
// defaults/ for baoopn and defaults/listening/ for the listening sub-brand.
//
//go:embed defaults
var defaultsFS embed.FS

// Slot names of the baoopn brand, in display order.
const (
	FaviconICO      = "favicon_ico"
	FaviconSVG      = "favicon_svg"
	AppleTouchIcon  = "apple_touch_icon"
	WordmarkPrimary = "wordmark_primary"
	WordmarkInverse = "wordmark_inverse"
	WordmarkNavy    = "wordmark_navy"
	WordmarkCream   = "wordmark_cream"
	MonogramPrimary = "monogram_primary"
	MonogramInverse = "monogram_inverse"
	MonogramNavy    = "monogram_navy"
	MonogramCream   = "monogram_cream"
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
	Kind    string // icon, logo or image
	Group   string // favicons, logos, icons, social or email
	Rule    string
	Preview string // light, dark or both
	Accept  []string
}

// Groups, in display order. Each brand table lists its slots group by group.
var groupOrder = []string{"favicons", "logos", "icons", "social", "email"}

// pngRule bounds a PNG slot's size in px and its aspect ratio (width /
// height, as MinAspect and MaxAspect in thousandths).
type pngRule struct {
	MinW, MaxW, MinH, MaxH int
	MinAspect, MaxAspect   int
}

// slots is the baoopn brand table.
var slots = []slotDef{
	{Slot: FaviconICO, File: "favicon.ico", Stem: "favicon", Ext: "ico", Type: typeICO, Max: 256 << 10,
		Label: "Favicon (ICO)", Kind: "icon", Group: "favicons", Rule: "ICO with 16×16, 32×32 and 48×48 images", Preview: "both",
		Accept: []string{"image/x-icon", "image/vnd.microsoft.icon", ".ico"}},
	{Slot: FaviconSVG, File: "favicon.svg", Stem: "favicon", Ext: "svg", Type: typeSVG, Max: 256 << 10, Square: true,
		Label: "Favicon (SVG)", Kind: "icon", Group: "favicons", Rule: "SVG with a square viewBox", Preview: "both",
		Accept: []string{"image/svg+xml"}},
	{Slot: AppleTouchIcon, File: "apple-touch-icon.png", Stem: "apple-touch-icon", Ext: "png", Type: typePNG, Max: 512 << 10,
		PNG:   pngRule{MinW: 180, MaxW: 180, MinH: 180, MaxH: 180},
		Label: "Apple touch icon", Kind: "icon", Group: "favicons", Rule: "PNG, exactly 180×180 px", Preview: "both",
		Accept: []string{"image/png"}},
	{Slot: WordmarkPrimary, File: "wordmark-primary.svg", Stem: "wordmark-primary", Ext: "svg", Type: typeSVG, Max: 256 << 10,
		Label: "Wordmark", Kind: "logo", Group: "logos", Rule: "SVG with a viewBox", Preview: "both",
		Accept: []string{"image/svg+xml"}},
	{Slot: WordmarkInverse, File: "wordmark-inverse.svg", Stem: "wordmark-inverse", Ext: "svg", Type: typeSVG, Max: 256 << 10,
		Label: "Wordmark (inverse, for dark backgrounds)", Kind: "logo", Group: "logos", Rule: "SVG with a viewBox", Preview: "dark",
		Accept: []string{"image/svg+xml"}},
	{Slot: WordmarkNavy, File: "wordmark-navy.svg", Stem: "wordmark-navy", Ext: "svg", Type: typeSVG, Max: 256 << 10,
		Label: "Wordmark (navy glyph, for light backgrounds)", Kind: "logo", Group: "logos", Rule: "SVG with a viewBox", Preview: "light",
		Accept: []string{"image/svg+xml"}},
	{Slot: WordmarkCream, File: "wordmark-cream.svg", Stem: "wordmark-cream", Ext: "svg", Type: typeSVG, Max: 256 << 10,
		Label: "Wordmark (cream glyph, for dark backgrounds)", Kind: "logo", Group: "logos", Rule: "SVG with a viewBox", Preview: "dark",
		Accept: []string{"image/svg+xml"}},
	{Slot: MonogramPrimary, File: "monogram-primary.svg", Stem: "monogram-primary", Ext: "svg", Type: typeSVG, Max: 256 << 10, Square: true,
		Label: "Monogram", Kind: "logo", Group: "logos", Rule: "SVG with a square viewBox", Preview: "both",
		Accept: []string{"image/svg+xml"}},
	{Slot: MonogramInverse, File: "monogram-inverse.svg", Stem: "monogram-inverse", Ext: "svg", Type: typeSVG, Max: 256 << 10, Square: true,
		Label: "Monogram (inverse, for dark backgrounds)", Kind: "logo", Group: "logos", Rule: "SVG with a square viewBox", Preview: "dark",
		Accept: []string{"image/svg+xml"}},
	{Slot: MonogramNavy, File: "monogram-navy.svg", Stem: "monogram-navy", Ext: "svg", Type: typeSVG, Max: 256 << 10,
		Label: "Monogram (navy glyph, for light backgrounds)", Kind: "logo", Group: "logos", Rule: "SVG with a viewBox", Preview: "light",
		Accept: []string{"image/svg+xml"}},
	{Slot: MonogramCream, File: "monogram-cream.svg", Stem: "monogram-cream", Ext: "svg", Type: typeSVG, Max: 256 << 10,
		Label: "Monogram (cream glyph, for dark backgrounds)", Kind: "logo", Group: "logos", Rule: "SVG with a viewBox", Preview: "dark",
		Accept: []string{"image/svg+xml"}},
	{Slot: EmailLogo, File: "email-logo.png", Stem: "email-logo", Ext: "png", Type: typePNG, Max: 256 << 10,
		PNG:   pngRule{MinW: 240, MaxW: 1200, MinH: 60, MaxH: 600, MinAspect: 1500, MaxAspect: 4000},
		Label: "Email logo", Kind: "logo", Group: "email", Rule: "PNG, 240–1200 × 60–600 px, landscape 1.5:1 to 4:1 (shown 48 px tall in emails)", Preview: "both",
		Accept: []string{"image/png"}},
}
