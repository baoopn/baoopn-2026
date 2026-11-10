// Package credit serves the "Designed & built by" line that client sites
// show in their footers, and stores its settings document "credit".
//
// The line is prefix + linked logo + suffix. Each logo variant (wordmark,
// mark) has its own prefix, suffix and style; a client site picks the
// variant with ?logo=.
//
// GET /api/v1/credit answers cross-origin reads for the origins in the
// document only; the rest of the API stays same-origin.
package credit

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/brand"
	"github.com/baoopn/baoopn-2026/internal/settings"
)

// Key is the settings key of the credit document.
const Key = "credit"

// Limits of the document (AdminCredit.limits in types.ts).
const (
	TextMax    = 80 // per line: prefix and suffix each
	URLMax     = 500
	OriginsMax = 50
	originMax  = 200
)

// Logo kinds.
const (
	LogoWordmark = "wordmark"
	LogoMark     = "mark"
)

// Logo styles: color is the filled tile, mono the glyph only.
const (
	StyleColor = "color"
	StyleMono  = "mono"
)

const defaultPrefix = "Designed & built by"

// kinds lists the logo kinds in display order.
var kinds = []string{LogoWordmark, LogoMark}

// Brand gives the current brand files with absolute URLs.
type Brand interface {
	AbsoluteAsset(ctx context.Context, slot, siteURL string) (brand.Asset, error)
}

// Deps are the module's dependencies.
type Deps struct {
	DB        *pgxpool.Pool
	Log       *slog.Logger
	Brand     Brand
	PublicURL string
	// Now is the clock (tests); nil means time.Now.
	Now func() time.Time
}

// Module serves /api/v1/credit and /api/admin/credit.
type Module struct {
	d    Deps
	mu   sync.Mutex // serialises saves of the settings document
	snap atomic.Pointer[Doc]
	// read and write reach the settings document (tests replace them).
	read  func(ctx context.Context) ([]byte, error)
	write func(ctx context.Context, d Doc) error
}

// New builds the module.
func New(d Deps) *Module {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	m := &Module{d: d}
	m.read = func(ctx context.Context) ([]byte, error) {
		var raw json.RawMessage
		_, err := settings.Get(ctx, m.d.DB, Key, &raw)
		return raw, err
	}
	m.write = func(ctx context.Context, d Doc) error { return settings.Put(ctx, m.d.DB, Key, d) }
	return m
}

// Variant is the line for one logo kind (CreditVariant in types.ts).
type Variant struct {
	Prefix string `json:"prefix"` // 0..TextMax characters, one line
	Suffix string `json:"suffix"` // 0..TextMax characters, one line
	Style  string `json:"style"`  // "color" | "mono"
}

// Doc is the settings document "credit".
type Doc struct {
	URL      string             `json:"url"`
	Logo     string             `json:"logo"`     // default variant: "wordmark" | "mark"
	Variants map[string]Variant `json:"variants"` // keys "wordmark" and "mark"
	Origins  []string           `json:"origins"`  // normalised, never nil
	// UpdatedAt is nil until the first save.
	UpdatedAt *time.Time `json:"updated_at"`
}

// variant returns d's variant for kind. A missing variant reads as the
// default line, and an empty style as color.
func (d Doc) variant(kind string) Variant {
	v, ok := d.Variants[kind]
	if !ok {
		v = defaultVariant(defaultPrefix)
	}
	if v.Style == "" {
		v.Style = StyleColor
	}
	return v
}

func defaultVariant(prefix string) Variant { return Variant{Prefix: prefix, Style: StyleColor} }

func defaultVariants(prefix string) map[string]Variant {
	return map[string]Variant{LogoWordmark: defaultVariant(prefix), LogoMark: defaultVariant(prefix)}
}

// Image is CreditImage in types.ts.
type Image struct {
	URL         string `json:"url"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ContentType string `json:"content_type"`
}

// Logo is CreditLogo in types.ts.
type Logo struct {
	Kind  string `json:"kind"`
	Style string `json:"style"`
	Light Image  `json:"light"`
	Dark  Image  `json:"dark"`
}

// Credit is Credit in types.ts (the public view of one variant).
type Credit struct {
	Text      string     `json:"text"` // deprecated: always the same as Prefix
	Prefix    string     `json:"prefix"`
	Suffix    string     `json:"suffix"`
	URL       string     `json:"url"`
	Alt       string     `json:"alt"`
	Logo      Logo       `json:"logo"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// Limits is AdminCredit.limits.
type Limits struct {
	PrefixMax  int `json:"prefix_max"`
	SuffixMax  int `json:"suffix_max"`
	URLMax     int `json:"url_max"`
	OriginsMax int `json:"origins_max"`
}

// AdminCredit is AdminCredit in types.ts.
type AdminCredit struct {
	URL       string             `json:"url"`
	Logo      string             `json:"logo"`
	Variants  map[string]Variant `json:"variants"`
	Origins   []string           `json:"origins"`
	UpdatedAt *time.Time         `json:"updated_at"`
	Endpoint  string             `json:"endpoint"`
	// Logos is logos[kind][style], all four, for the previews.
	Logos  map[string]map[string]Logo `json:"logos"`
	Limits Limits                     `json:"limits"`
}

// VariantInput is one variant in a PUT body.
type VariantInput struct {
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
	Style  string `json:"style"`
}

// Input is CreditInput in types.ts.
type Input struct {
	URL      string                  `json:"url"`
	Logo     string                  `json:"logo"`
	Variants map[string]VariantInput `json:"variants"`
	// Text is the old single-line body. It is used only when Variants is
	// absent, as the prefix of both variants (deprecated).
	Text    *string  `json:"text"`
	Origins []string `json:"origins"`
}

// slots maps a logo kind and style to its light and dark brand slots.
var slots = map[string]map[string][2]string{
	LogoWordmark: {
		StyleColor: {brand.WordmarkPrimary, brand.WordmarkInverse},
		StyleMono:  {brand.WordmarkNavy, brand.WordmarkCream},
	},
	LogoMark: {
		StyleColor: {brand.MonogramPrimary, brand.MonogramInverse},
		StyleMono:  {brand.MonogramNavy, brand.MonogramCream},
	},
}

// siteOrigin is PublicURL as scheme://host[:port].
func (m *Module) siteOrigin() string {
	raw := strings.TrimSpace(m.d.PublicURL)
	p, err := url.Parse(raw)
	if err != nil || p.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	scheme := strings.ToLower(p.Scheme)
	host := strings.ToLower(p.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := p.Port()
	if n, err := strconv.Atoi(port); err == nil {
		port = strconv.Itoa(n)
	}
	return scheme + "://" + hostPort(scheme, host, port)
}

// alt is the site's host name without a leading "www.".
func (m *Module) alt() string {
	p, err := url.Parse(m.siteOrigin())
	if err != nil || p.Hostname() == "" {
		return m.siteOrigin()
	}
	return strings.TrimPrefix(p.Hostname(), "www.")
}

func (m *Module) endpoint() string { return m.siteOrigin() + "/api/v1/credit" }

// defaults is the document when nothing is stored.
func (m *Module) defaults() Doc {
	return Doc{URL: m.siteOrigin(), Logo: LogoWordmark, Variants: defaultVariants(defaultPrefix), Origins: []string{}}
}

// parseDoc reads a stored document leniently: a missing or broken field
// gets its default, and a bad origin is dropped. A document from before
// the variants has a top-level "text"; it becomes the prefix of each
// variant that is missing.
func parseDoc(raw []byte, def Doc) Doc {
	d := def
	d.Variants = defaultVariants(defaultPrefix)
	var m map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		return d
	}
	var s string
	prefix := defaultPrefix
	if json.Unmarshal(m["text"], &s) == nil {
		if v, ok := validLine(s); ok {
			prefix = v
		}
	}
	var vars map[string]json.RawMessage
	_ = json.Unmarshal(m["variants"], &vars)
	for _, kind := range kinds {
		d.Variants[kind] = parseVariant(vars[kind], prefix)
	}
	s = ""
	if json.Unmarshal(m["url"], &s) == nil {
		if v, ok := validURL(s); ok {
			d.URL = v
		}
	}
	s = ""
	if json.Unmarshal(m["logo"], &s) == nil && validLogo(s) {
		d.Logo = s
	}
	var list []any
	if json.Unmarshal(m["origins"], &list) == nil {
		var strs []string
		for _, v := range list {
			if o, ok := v.(string); ok {
				if n, ok := normalizeOrigin(o); ok {
					strs = append(strs, n)
				}
			}
		}
		d.Origins = dedupe(strs)
		if len(d.Origins) > OriginsMax {
			d.Origins = d.Origins[:OriginsMax]
		}
	}
	var at time.Time
	if json.Unmarshal(m["updated_at"], &at) == nil && !at.IsZero() {
		at = at.UTC()
		d.UpdatedAt = &at
	}
	return d
}

// parseVariant reads one stored variant. A missing variant gives
// legacyPrefix, no suffix and color; a variant that is there gives the
// default line for a missing or broken prefix, and each other missing or
// broken field gets its default.
func parseVariant(raw json.RawMessage, legacyPrefix string) Variant {
	var m map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil || m == nil {
		return defaultVariant(legacyPrefix)
	}
	v := defaultVariant(defaultPrefix)
	var s string
	if json.Unmarshal(m["prefix"], &s) == nil {
		if p, ok := validLine(s); ok {
			v.Prefix = p
		}
	}
	s = ""
	if json.Unmarshal(m["suffix"], &s) == nil {
		if p, ok := validLine(s); ok {
			v.Suffix = p
		}
	}
	s = ""
	if json.Unmarshal(m["style"], &s) == nil && validStyle(s) {
		v.Style = s
	}
	return v
}

// load returns the snapshot, reading the database on first use.
func (m *Module) load(ctx context.Context) (Doc, error) {
	if d := m.snap.Load(); d != nil {
		return *d, nil
	}
	raw, err := m.read(ctx)
	if err != nil {
		return Doc{}, err
	}
	d := parseDoc(raw, m.defaults())
	// A save that finished meanwhile wins over this older read.
	if !m.snap.CompareAndSwap(nil, &d) {
		return *m.snap.Load(), nil
	}
	return d, nil
}

// save stores doc and replaces the snapshot.
func (m *Module) save(ctx context.Context, d Doc) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.write(ctx, d); err != nil {
		return err
	}
	m.snap.Store(&d)
	return nil
}

func (m *Module) image(ctx context.Context, slot string) (Image, error) {
	a, err := m.d.Brand.AbsoluteAsset(ctx, slot, m.d.PublicURL)
	if err != nil {
		return Image{}, err
	}
	return Image{URL: a.URL, Width: a.Width, Height: a.Height, ContentType: a.ContentType}, nil
}

// logo resolves kind and style to their light and dark files.
func (m *Module) logo(ctx context.Context, kind, style string) (Logo, error) {
	s := slots[kind][style]
	light, err := m.image(ctx, s[0])
	if err != nil {
		return Logo{}, err
	}
	dark, err := m.image(ctx, s[1])
	if err != nil {
		return Logo{}, err
	}
	return Logo{Kind: kind, Style: style, Light: light, Dark: dark}, nil
}

// public builds the public view of d's variant kind. A valid style
// overrides the variant's stored style; "" keeps it.
func (m *Module) public(ctx context.Context, d Doc, kind, style string) (Credit, error) {
	v := d.variant(kind)
	if validStyle(style) {
		v.Style = style
	}
	l, err := m.logo(ctx, kind, v.Style)
	if err != nil {
		return Credit{}, err
	}
	return Credit{Text: v.Prefix, Prefix: v.Prefix, Suffix: v.Suffix, URL: d.URL, Alt: m.alt(), Logo: l,
		UpdatedAt: d.UpdatedAt}, nil
}

// admin builds the admin view of d.
func (m *Module) admin(ctx context.Context, d Doc) (AdminCredit, error) {
	variants := make(map[string]Variant, len(kinds))
	logos := make(map[string]map[string]Logo, len(kinds))
	for _, kind := range kinds {
		variants[kind] = d.variant(kind)
		logos[kind] = make(map[string]Logo, 2)
		for _, style := range []string{StyleColor, StyleMono} {
			l, err := m.logo(ctx, kind, style)
			if err != nil {
				return AdminCredit{}, err
			}
			logos[kind][style] = l
		}
	}
	origins := d.Origins
	if origins == nil {
		origins = []string{}
	}
	return AdminCredit{URL: d.URL, Logo: d.Logo, Variants: variants, Origins: origins, UpdatedAt: d.UpdatedAt,
		Endpoint: m.endpoint(), Logos: logos,
		Limits: Limits{PrefixMax: TextMax, SuffixMax: TextMax, URLMax: URLMax, OriginsMax: OriginsMax}}, nil
}
