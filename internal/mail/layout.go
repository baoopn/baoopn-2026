package mail

import (
	"bytes"
	"embed"
	htmltemplate "html/template"
	"net/url"
	"regexp"
	"strings"
	texttemplate "text/template"
)

// Brand is the site an email speaks for: Name is the host of the public URL
// ("baoopn.com"), URL the origin without a trailing slash. LogoURL is an
// optional absolute http(s) URL of a PNG logo shown in place of the text
// badge, LogoWidth × LogoHeight its size in px (0 when unknown).
type Brand struct {
	Name, URL             string
	LogoURL               string
	LogoWidth, LogoHeight int
}

// logoHeight is how tall the logo is shown, in CSS px.
const logoHeight = 48

// Logo is the header logo as the layout shows it.
type Logo struct {
	Src, Alt      string
	Width, Height int
}

// logo returns the header logo, or nil (the text badge) when LogoURL is not
// an absolute http(s) URL. Width follows the file's aspect ratio at
// logoHeight, kept within the 1.5:1 to 4:1 the upload rule allows.
func (b Brand) logo() *Logo {
	p, err := url.Parse(b.LogoURL)
	if b.LogoURL == "" || err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" || p.User != nil ||
		strings.ContainsAny(b.LogoURL, " \t\r\n\"'<>\\`") {
		return nil
	}
	w := 116 // size unknown: the default logo (640×264) at logoHeight
	if b.LogoWidth > 0 && b.LogoHeight > 0 {
		w = (logoHeight*b.LogoWidth + b.LogoHeight/2) / b.LogoHeight
	}
	return &Logo{Src: b.LogoURL, Alt: b.Name, Width: min(max(w, logoHeight*3/2), logoHeight*4), Height: logoHeight}
}

// BrandOf derives the brand from the site's public URL.
func BrandOf(publicURL string) Brand {
	u := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	b := Brand{URL: u}
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		b.Name = strings.TrimPrefix(p.Hostname(), "www.")
	}
	return b
}

// Field is one "Label: Value" row; Mono shows the value in a monospace font.
type Field struct {
	Label, Value string
	Mono         bool
}

// BlockKind says how a Block is shown.
type BlockKind int

const (
	// Para is a paragraph of plain text; line breaks are kept.
	Para BlockKind = iota
	// Quote is quoted mail ("> " lines), shown with a muted left border.
	Quote
	// Fields is a list of labelled values under an optional Label heading.
	Fields
	// Excerpt is someone's message, shown in a tinted box.
	Excerpt
	// Button links to a page on the site (Label, URL).
	Button
)

// Block is one piece of an email body. Text is always plain text.
type Block struct {
	Kind       BlockKind
	Text       string
	Fields     []Field
	Label, URL string
}

// Doc is one email to render as matching text and HTML parts.
type Doc struct {
	Brand Brand
	// Subject is the HTML <title>.
	Subject                  string
	Preheader, Kicker, Title string
	Blocks                   []Block
	Signature, Footer        string
	// Personal is a person's reply: minimal chrome, and a text part that is
	// only the blocks (joined by single line breaks) + sigSep + Signature.
	Personal bool
}

// SigSep starts the signature under a message.
const SigSep = "\n\n-- \n"

//go:embed templates/layout.html.tmpl templates/layout.txt.tmpl
var layoutFS embed.FS

// The constant chrome html/template would otherwise strip (comments) or
// re-escape. None of these carries data.
const (
	msoOpen      = `<!--[if mso]><table role="presentation" width="600" cellpadding="0" cellspacing="0" border="0" align="center"><tr><td><![endif]-->`
	msoClose     = `<!--[if mso]></td></tr></table><![endif]-->`
	preheaderPad = "&#847;&zwnj;&nbsp;"
	// msoFonts keeps Outlook desktop off Times when a stack's fonts are missing.
	msoFonts = `<!--[if mso]><style>body,table,td,div,a{font-family:Arial,Helvetica,sans-serif !important;}</style><![endif]-->`
)

var (
	htmlLayout = htmltemplate.Must(htmltemplate.New("layout.html.tmpl").Option("missingkey=error").Funcs(htmltemplate.FuncMap{
		"lines":        htmlLines,
		"quoteLines":   quoteLines,
		"sigLines":     sigLines,
		"buttonURL":    func(d Doc, b Block) string { return d.Brand.link(b.URL) },
		"homeURL":      func(b Brand) string { return b.home() },
		"logo":         func(b Brand) *Logo { return b.logo() },
		"msoOpen":      func() htmltemplate.HTML { return msoOpen },
		"msoClose":     func() htmltemplate.HTML { return msoClose },
		"msoFonts":     func() htmltemplate.HTML { return msoFonts },
		"defang":       defang,
		"preheaderPad": func() htmltemplate.HTML { return htmltemplate.HTML(strings.Repeat(preheaderPad, 40)) },
	}).ParseFS(layoutFS, "templates/layout.html.tmpl"))

	textLayout = texttemplate.Must(texttemplate.New("layout.txt.tmpl").Option("missingkey=error").Funcs(texttemplate.FuncMap{
		"sep":       func(i int, personal bool) string { return blockSep(i, personal) },
		"textBlock": func(d Doc, b Block) string { return textBlock(d, b) },
		"sigSep":    func() string { return SigSep },
	}).ParseFS(layoutFS, "templates/layout.txt.tmpl"))
)

// Render returns the text and HTML parts of d. All dynamic values in the HTML
// are escaped by html/template; none is ever trusted as markup, CSS or URL.
func (d Doc) Render() (text, html string, err error) {
	var t, h bytes.Buffer
	if err := textLayout.Execute(&t, d); err != nil {
		return "", "", err
	}
	if err := htmlLayout.Execute(&h, d); err != nil {
		return "", "", err
	}
	return t.String(), h.String(), nil
}

// link returns u when it is a page on the brand's own http(s) site, else "".
func (b Brand) link(u string) string {
	if b.home() == "" || !strings.HasPrefix(u, b.URL+"/") {
		return ""
	}
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.User != nil {
		return ""
	}
	return u
}

// home is the brand URL when it is an http(s) origin, else "".
func (b Brand) home() string {
	p, err := url.Parse(b.URL)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" || p.User != nil {
		return ""
	}
	return b.URL
}

func splitLines(s string) []string { return strings.Split(s, "\n") }

// htmlLines is s as lines for HTML, without leading or trailing empty lines
// (the space between blocks comes from the layout).
func htmlLines(s string) []string { return splitLines(strings.Trim(s, "\n")) }

// quoteLines is a quote's lines without their "> " markers, for HTML.
func quoteLines(s string) []string {
	ls := htmlLines(s)
	for i, l := range ls {
		l = strings.TrimPrefix(l, ">")
		ls[i] = strings.TrimPrefix(l, " ")
	}
	return ls
}

// SigLine is one line of a signature for HTML. A line that is exactly an
// http(s) URL has URL set and Text is the URL without its scheme, shown as our
// own styled link (so mail apps do not restyle it); any other line is plain
// text with URL "".
type SigLine struct{ Text, URL string }

// sigLines splits a signature into lines, finding the URL lines.
func sigLines(s string) []SigLine {
	var out []SigLine
	for _, l := range htmlLines(s) {
		if u, show := sigURL(strings.TrimSpace(l)); u != "" {
			out = append(out, SigLine{Text: show, URL: u})
			continue
		}
		out = append(out, SigLine{Text: l})
	}
	return out
}

// sigURL returns l and how to show it when l is a whole absolute http(s)
// URL, else "", "".
func sigURL(l string) (string, string) {
	if l == "" || strings.ContainsAny(l, " \t\"'<>\\`") {
		return "", ""
	}
	p, err := url.Parse(l)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" || p.User != nil || p.Opaque != "" {
		return "", ""
	}
	show := strings.TrimSuffix(l[strings.Index(l, "://")+3:], "/")
	if show == "" {
		return "", ""
	}
	return l, show
}

func blockSep(i int, personal bool) string {
	switch {
	case i == 0:
		return ""
	case personal:
		return "\n"
	}
	return "\n\n"
}

// textBlock is one block of the text part.
func textBlock(d Doc, b Block) string {
	switch b.Kind {
	case Quote:
		if d.Personal {
			return b.Text
		}
		ls := splitLines(b.Text)
		for i, l := range ls {
			if !strings.HasPrefix(l, ">") {
				ls[i] = "> " + l
			}
		}
		return strings.Join(ls, "\n")
	case Fields:
		var ls []string
		if b.Label != "" {
			ls = append(ls, b.Label)
		}
		for _, f := range b.Fields {
			ls = append(ls, f.Label+": "+f.Value)
		}
		return strings.Join(ls, "\n")
	case Button:
		u := d.Brand.link(b.URL)
		if u == "" {
			return b.Label
		}
		return b.Label + ": " + u
	}
	return b.Text
}

// Piece is part of a line of HTML text: Sep pieces ("." "@" ":") are shown in
// their own span, which stops mail clients from turning a URL or address a
// stranger typed into a live link. Copying the text still gives it whole.
type Piece struct {
	Text string
	Sep  bool
}

// defang splits line into pieces, breaking up every URL-, domain- or
// address-like word at its separators. Other words are one piece.
func defang(line string) []Piece {
	var out []Piece
	add := func(text string, sep bool) {
		if text == "" {
			return
		}
		if n := len(out); n > 0 && !sep && !out[n-1].Sep {
			out[n-1].Text += text
			return
		}
		out = append(out, Piece{Text: text, Sep: sep})
	}
	start := 0
	for i := 0; i <= len(line); i++ {
		if i < len(line) && line[i] != ' ' && line[i] != '\t' {
			continue
		}
		word := line[start:i]
		if linkLike(word) {
			from := 0
			for j, r := range word {
				if strings.ContainsRune(".@:．。｡＠", r) {
					add(word[from:j], false)
					add(string(r), true)
					from = j + len(string(r))
				}
			}
			add(word[from:], false)
		} else {
			add(word, false)
		}
		if i < len(line) {
			add(line[i:i+1], false)
		}
		start = i + 1
	}
	return out
}

// domainish is a label, a dot (ASCII, full-width or ideographic) and two or
// more letters.
var domainish = regexp.MustCompile(`(?i)[\p{L}\p{N}-][.．。｡]\p{L}{2,}`)

func linkLike(word string) bool {
	l := strings.ToLower(word)
	return strings.ContainsAny(word, "@＠") || strings.Contains(l, "://") || strings.Contains(l, "www") ||
		strings.HasPrefix(l, "mailto:") || domainish.MatchString(word)
}
