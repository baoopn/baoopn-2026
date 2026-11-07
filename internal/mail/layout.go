package mail

import (
	"bytes"
	"embed"
	htmltemplate "html/template"
	"net/url"
	"strings"
	texttemplate "text/template"
)

// Brand is the site an email speaks for: Name is the host of the public URL
// ("baoopn.com"), URL the origin without a trailing slash.
type Brand struct{ Name, URL string }

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
)

var (
	htmlLayout = htmltemplate.Must(htmltemplate.New("layout.html.tmpl").Option("missingkey=error").Funcs(htmltemplate.FuncMap{
		"lines":        htmlLines,
		"quoteLines":   quoteLines,
		"buttonURL":    func(d Doc, b Block) string { return d.Brand.link(b.URL) },
		"homeURL":      func(b Brand) string { return b.home() },
		"msoOpen":      func() htmltemplate.HTML { return msoOpen },
		"msoClose":     func() htmltemplate.HTML { return msoClose },
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
