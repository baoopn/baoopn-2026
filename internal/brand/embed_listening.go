package brand

import (
	"fmt"
	"strings"
)

// The listening sub-brand (listening.baoopn.com). Its defaults live in
// defaults/listening/ and come byte for byte from the design kit in
// design/brand/listening/.

// Rule texts shared by both brands.
const (
	ruleICO       = "ICO with 16×16, 32×32 and 48×48 images"
	ruleSVGSquare = "SVG with a square viewBox"
	ruleSVG       = "SVG with a viewBox"
)

var (
	acceptICO = []string{"image/x-icon", "image/vnd.microsoft.icon", ".ico"}
	acceptPNG = []string{"image/png"}
	acceptSVG = []string{"image/svg+xml"}
)

// listeningIcons are the UI icon names: slot icon_<name>_<colour>, file
// icon-<name with dashes>-<colour>.svg. The value is the label prefix.
var listeningIcons = []struct{ name, label string }{
	{"headphones", "Headphones"},
	{"now_playing", "Now playing (animated)"},
	{"now_playing_static", "Now playing (static, reduced motion)"},
	{"paused", "Paused"},
	{"playlist", "Playlist"},
	{"track", "Track"},
}

// exactPNG is a PNG slot that must be exactly w×h px.
func exactPNG(slot, file, label, kind, group string, w, h int, note string) slotDef {
	rule := fmt.Sprintf("PNG, exactly %d×%d px", w, h)
	if note != "" {
		rule += "; " + note
	}
	return slotDef{Slot: slot, File: file, Stem: strings.TrimSuffix(file, ".png"), Ext: "png", Type: typePNG,
		Max: 512 << 10, PNG: pngRule{MinW: w, MaxW: w, MinH: h, MaxH: h},
		Label: label, Kind: kind, Group: group, Rule: rule, Preview: "both", Accept: acceptPNG}
}

// svgSlot is an SVG slot; square slots need a square viewBox.
func svgSlot(slot, file, label, kind, group, preview string, square bool) slotDef {
	rule := ruleSVG
	if square {
		rule = ruleSVGSquare
	}
	return slotDef{Slot: slot, File: file, Stem: strings.TrimSuffix(file, ".svg"), Ext: "svg", Type: typeSVG,
		Max: 256 << 10, Square: square, Label: label, Kind: kind, Group: group, Rule: rule, Preview: preview,
		Accept: acceptSVG}
}

// listeningSlots is the listening brand table, in display order.
var listeningSlots = buildListeningSlots()

func buildListeningSlots() []slotDef {
	out := []slotDef{
		{Slot: FaviconICO, File: "favicon.ico", Stem: "favicon", Ext: "ico", Type: typeICO, Max: 256 << 10,
			Label: "Favicon (ICO)", Kind: "icon", Group: "favicons", Rule: ruleICO, Preview: "both", Accept: acceptICO},
		svgSlot(FaviconSVG, "favicon.svg", "Favicon (SVG)", "icon", "favicons", "both", true),
		exactPNG(AppleTouchIcon, "apple-touch-icon.png", "Apple touch icon", "icon", "favicons", 180, 180, ""),
		exactPNG("favicon_16", "favicon-16.png", "Favicon 16 px (PNG)", "icon", "favicons", 16, 16, ""),
		exactPNG("favicon_32", "favicon-32.png", "Favicon 32 px (PNG)", "icon", "favicons", 32, 32, ""),
		exactPNG("favicon_48", "favicon-48.png", "Favicon 48 px (PNG)", "icon", "favicons", 48, 48, ""),
		exactPNG("icon_192", "icon-192.png", "App icon 192 px", "icon", "favicons", 192, 192, ""),
		exactPNG("icon_512", "icon-512.png", "App icon 512 px", "icon", "favicons", 512, 512, ""),
		exactPNG("icon_maskable_512", "icon-maskable-512.png", "Maskable app icon 512 px", "icon", "favicons", 512, 512,
			"keep the mark inside the central 80% circle"),
		svgSlot("mark_primary", "mark-primary.svg", "Mark", "logo", "logos", "both", true),
		svgSlot("mark_inverse", "mark-inverse.svg", "Mark (inverse, for dark backgrounds)", "logo", "logos", "dark", true),
		svgSlot("mark_navy", "mark-navy.svg", "Mark (navy glyph, for light backgrounds)", "logo", "logos", "light", false),
		svgSlot("mark_cream", "mark-cream.svg", "Mark (cream glyph, for dark backgrounds)", "logo", "logos", "dark", false),
		svgSlot(WordmarkPrimary, "wordmark-primary.svg", "Wordmark", "logo", "logos", "both", false),
		svgSlot(WordmarkInverse, "wordmark-inverse.svg", "Wordmark (inverse, for dark backgrounds)", "logo", "logos", "dark", false),
		svgSlot(WordmarkNavy, "wordmark-navy.svg", "Wordmark (navy glyph, for light backgrounds)", "logo", "logos", "light", false),
		svgSlot(WordmarkCream, "wordmark-cream.svg", "Wordmark (cream glyph, for dark backgrounds)", "logo", "logos", "dark", false),
	}
	for _, ic := range listeningIcons {
		file := "icon-" + strings.ReplaceAll(ic.name, "_", "-")
		out = append(out,
			svgSlot("icon_"+ic.name+"_navy", file+"-navy.svg", ic.label+" icon (navy)", "icon", "icons", "light", true),
			svgSlot("icon_"+ic.name+"_cream", file+"-cream.svg", ic.label+" icon (cream)", "icon", "icons", "dark", true))
	}
	og := exactPNG("og_image", "og-image.png", "Social share image", "image", "social", 1200, 630, "")
	return append(out, og,
		slotDef{Slot: EmailLogo, File: "email-logo.png", Stem: "email-logo", Ext: "png", Type: typePNG, Max: 512 << 10,
			PNG:   pngRule{MinW: 240, MaxW: 1200, MinH: 60, MaxH: 600, MinAspect: 1500, MaxAspect: 4000},
			Label: "Email logo", Kind: "logo", Group: "email",
			Rule:    "PNG, 240–1200 × 60–600 px, landscape 1.5:1 to 4:1 (shown 160 px wide in emails)",
			Preview: "both", Accept: acceptPNG})
}
