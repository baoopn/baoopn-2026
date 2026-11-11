// Package markdown renders Markdown (GFM + footnotes) to HTML with chroma
// syntax highlighting (CSS classes), heading anchors and a table of contents.
//
// Nothing here sanitizes HTML. Trusted renders (admin-authored posts, profile,
// project bodies) keep raw HTML exactly as written. Untrusted renders (GitHub
// READMEs) drop raw HTML and dangerous link schemes, which is goldmark's
// default without html.WithUnsafe.
package markdown

import (
	"bytes"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

// TocItem is one heading in the table of contents.
type TocItem struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Level int    `json:"level"`
}

// Result is a rendered document.
type Result struct {
	HTML           string    `json:"html"`
	TOC            []TocItem `json:"toc"`
	ReadingMinutes int       `json:"reading_minutes"`
}

// Options tune rendering.
type Options struct {
	// Trusted allows raw HTML in the source (admin-authored content only).
	Trusted bool
	// LinkBase / ImageBase resolve relative links and images (GitHub READMEs).
	LinkBase, ImageBase string
}

func newMD(trusted bool) goldmark.Markdown {
	rendererOpts := []renderer.Option{html.WithXHTML()}
	if trusted {
		rendererOpts = append(rendererOpts, html.WithUnsafe())
	}
	return goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.Typographer,
			highlighting.NewHighlighting(
				highlighting.WithFormatOptions(chromahtml.WithClasses(true), chromahtml.TabWidth(4)),
				highlighting.WithGuessLanguage(false),
			),
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(rendererOpts...),
	)
}

var (
	trustedMD   = newMD(true)
	untrustedMD = newMD(false)
)

// Render converts Markdown source to HTML.
func Render(src string, opt Options) (Result, error) {
	md := untrustedMD
	if opt.Trusted {
		md = trustedMD
	}
	source := []byte(src)
	doc := md.Parser().Parse(text.NewReader(source))
	var toc []TocItem
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Heading:
			id, _ := v.AttributeString("id")
			if idb, ok := id.([]byte); ok && v.Level >= 2 && v.Level <= 4 {
				toc = append(toc, TocItem{ID: string(idb), Text: plainText(v, source), Level: v.Level})
			}
		case *ast.Link:
			v.Destination = []byte(resolve(string(v.Destination), opt.LinkBase))
		case *ast.Image:
			v.Destination = []byte(resolve(string(v.Destination), opt.ImageBase))
		}
		return ast.WalkContinue, nil
	})
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, source, doc); err != nil {
		return Result{}, err
	}
	out := buf.String()
	if opt.ImageBase != "" {
		out = rewriteRawImgs(out, opt.ImageBase)
	}
	if toc == nil {
		toc = []TocItem{}
	}
	return Result{HTML: out, TOC: toc, ReadingMinutes: ReadingMinutes(src)}, nil
}

func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			if t, ok := c.(*ast.Text); ok {
				b.Write(t.Segment.Value(src))
			} else if s, ok := c.(*ast.String); ok {
				b.Write(s.Value)
			}
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(b.String())
}

// resolve makes a relative URL absolute against base (no-op without base).
func resolve(dest, base string) string {
	if base == "" || dest == "" || strings.HasPrefix(dest, "#") {
		return dest
	}
	u, err := url.Parse(dest)
	if err != nil || u.IsAbs() || strings.HasPrefix(dest, "//") || strings.HasPrefix(dest, "mailto:") {
		return dest
	}
	b, err := url.Parse(strings.TrimRight(base, "/") + "/")
	if err != nil {
		return dest
	}
	if strings.HasPrefix(dest, "/") {
		// Repo-root-relative: keep the base path prefix (owner/repo/branch).
		b.Path = path.Join(b.Path, dest)
		return b.String()
	}
	return b.ResolveReference(u).String()
}

var imgSrc = regexp.MustCompile(`(<img[^>]*\ssrc=")([^"]+)(")`)

// rewriteRawImgs resolves relative <img src> in the rendered output against
// base. Raw HTML only survives a trusted render, so for untrusted READMEs this
// touches only the <img> tags goldmark emitted itself.
func rewriteRawImgs(h, base string) string {
	return imgSrc.ReplaceAllStringFunc(h, func(m string) string {
		p := imgSrc.FindStringSubmatch(m)
		return p[1] + resolve(p[2], base) + p[3]
	})
}

// ReadingMinutes estimates reading time at ~220 words per minute.
func ReadingMinutes(src string) int {
	words := len(strings.Fields(src))
	m := (words + 219) / 220
	return max(m, 1)
}

// Excerpt returns the first paragraph-ish text of Markdown, up to n runes.
func Excerpt(src string, n int) string {
	var parts []string
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			if len(parts) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "```") || strings.HasPrefix(t, "![") ||
			strings.HasPrefix(t, "|") || strings.HasPrefix(t, "<") {
			if len(parts) > 0 {
				break
			}
			continue
		}
		parts = append(parts, t)
	}
	s := stripInline.ReplaceAllString(strings.Join(parts, " "), "$1")
	s = strings.NewReplacer("**", "", "__", "", "`", "", "*", "").Replace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)[:n]
	if i := strings.LastIndexByte(string(r), ' '); i > n/2 {
		return string(r)[:i] + "…"
	}
	return string(r) + "…"
}

var stripInline = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)

// Slugify makes a URL slug from a title.
func Slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 80 {
		out = strings.Trim(out[:80], "-")
	}
	return out
}
