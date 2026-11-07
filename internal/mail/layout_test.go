package mail

import (
	"regexp"
	"strings"
	"testing"
)

var testBrand = BrandOf("https://example.test/")

func render(t *testing.T, d Doc) (string, string) {
	t.Helper()
	text, html, err := d.Render()
	if err != nil {
		t.Fatal(err)
	}
	return text, html
}

var (
	tagRe   = regexp.MustCompile(`<\s*/?\s*([a-zA-Z][a-zA-Z0-9]*)`)
	hrefRe  = regexp.MustCompile(`href="([^"]*)"`)
	inTagRe = regexp.MustCompile(`<[a-zA-Z][^>]*>`)
)

var allowedTags = map[string]bool{"html": true, "head": true, "meta": true, "title": true, "style": true,
	"body": true, "div": true, "table": true, "tr": true, "td": true, "span": true, "a": true, "br": true, "strong": true}

// checkSafe asserts html has only allowed tags, no script or handlers, and
// links only to the brand.
func checkSafe(t *testing.T, b Brand, html string) {
	t.Helper()
	body := html
	// The MSO conditionals are constant chrome, not data.
	body = strings.ReplaceAll(body, msoOpen, "")
	body = strings.ReplaceAll(body, msoClose, "")
	for _, m := range tagRe.FindAllStringSubmatch(body, -1) {
		if !allowedTags[strings.ToLower(m[1])] {
			t.Errorf("tag <%s> not allowed", m[1])
		}
	}
	for _, bad := range []string{"<script", "<img"} {
		if strings.Contains(strings.ToLower(body), bad) {
			t.Errorf("html contains %q", bad)
		}
	}
	// Escaped text may spell them out; no real tag may carry them.
	for _, tag := range inTagRe.FindAllString(body, -1) {
		if l := strings.ToLower(tag); strings.Contains(l, "onerror") || strings.Contains(l, "javascript:") {
			t.Errorf("tag carries a handler or script url: %s", tag)
		}
	}
	for _, m := range hrefRe.FindAllStringSubmatch(html, -1) {
		if !strings.HasPrefix(m[1], b.URL) {
			t.Errorf("href %q is not on %s", m[1], b.URL)
		}
	}
}

func TestBrandOf(t *testing.T) {
	if b := BrandOf("https://www.example.test/"); b.Name != "example.test" || b.URL != "https://www.example.test" {
		t.Fatalf("%+v", b)
	}
	if b := BrandOf("http://localhost:5173"); b.Name != "localhost" || b.URL != "http://localhost:5173" {
		t.Fatalf("%+v", b)
	}
}

func TestLayoutSanity(t *testing.T) {
	_, html := render(t, Doc{Brand: testBrand, Subject: "Hi", Preheader: "pre", Kicker: "New", Title: "T",
		Blocks: []Block{{Kind: Para, Text: "hello"}}})
	for _, want := range []string{`<meta name="color-scheme" content="light dark">`, "prefers-color-scheme: dark",
		`role="presentation"`, "<!--[if mso]>", "<![endif]-->", "[data-ogsc]", `width="600"`, "max-width:600px",
		"&#847;&zwnj;&nbsp;", "<title>Hi</title>", "#0b0537", ">example.test<"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
	if i, j := strings.Index(html, "<body"), strings.Index(html, ">pre"); i < 0 || j < i || strings.Contains(html[i:j], "<table") {
		t.Error("the preheader is not the first thing in the body")
	}
}

func TestLayoutEscapes(t *testing.T) {
	evil := `<img src=x onerror=alert(1)></td></table>{{.}} javascript:alert(1) "><script>x</script>`
	d := Doc{Brand: testBrand, Subject: evil, Preheader: evil, Kicker: evil, Title: evil,
		Blocks: []Block{
			{Kind: Para, Text: evil}, {Kind: Quote, Text: "> " + evil}, {Kind: Excerpt, Text: evil},
			{Kind: Fields, Label: evil, Fields: []Field{{Label: evil, Value: evil, Mono: true}}},
			{Kind: Button, Label: evil, URL: "https://example.test/admin/inbox/1"},
		},
		Signature: evil, Footer: evil}
	_, html := render(t, d)
	checkSafe(t, testBrand, html)
	if !strings.Contains(html, "&lt;img src=x onerror=alert(1)&gt;") {
		t.Error("the text is not shown escaped")
	}
}

func TestButtonOnlyLinksToTheSite(t *testing.T) {
	for _, u := range []string{"javascript:alert(1)", "https://evil.test/admin", "https://example.test.evil.test/x",
		"https://example.test", "//evil.test/x", "https://user@example.test/x", "data:text/html,x"} {
		text, html := render(t, Doc{Brand: testBrand, Blocks: []Block{{Kind: Button, Label: "Open", URL: u}}})
		if strings.Contains(html, ">Open</a>") || strings.Contains(text, u) {
			t.Errorf("%q: the button was kept", u)
		}
		checkSafe(t, testBrand, html)
	}
	text, html := render(t, Doc{Brand: testBrand, Blocks: []Block{{Kind: Button, Label: "Open", URL: "https://example.test/admin/inbox/7"}}})
	if !strings.Contains(html, `href="https://example.test/admin/inbox/7"`) || text != "Open: https://example.test/admin/inbox/7" {
		t.Fatalf("good button dropped: %q", text)
	}
	// A brand that is not an http(s) site gets no links at all.
	_, html = render(t, Doc{Brand: BrandOf("javascript:alert(1)//"), Blocks: []Block{{Kind: Button, Label: "Open", URL: "javascript:alert(1)/x"}}})
	if strings.Contains(html, "href=") {
		t.Fatal("link on a non-http brand")
	}
}

func TestTextPart(t *testing.T) {
	text, _ := render(t, Doc{Brand: testBrand, Kicker: "K", Title: "T",
		Blocks: []Block{
			{Kind: Para, Text: "Hello\nthere"},
			{Kind: Quote, Text: "> kept\nadded"},
			{Kind: Fields, Label: "Your message", Fields: []Field{{Label: "A", Value: "1"}, {Label: "B", Value: "2"}}},
			{Kind: Excerpt, Text: "excerpt"},
			{Kind: Button, Label: "Open", URL: "https://example.test/x"},
		},
		Signature: "Sig", Footer: "Foot"})
	want := "Hello\nthere\n\n> kept\n> added\n\nYour message\nA: 1\nB: 2\n\nexcerpt\n\nOpen: https://example.test/x" +
		SigSep + "Sig\n\nFoot"
	if text != want {
		t.Fatalf("text:\n%q\nwant\n%q", text, want)
	}
}

func TestPersonalDoc(t *testing.T) {
	d := Doc{Brand: testBrand, Personal: true, Footer: "not shown",
		Blocks:    []Block{{Kind: Para, Text: "Thanks!\n"}, {Kind: Quote, Text: "> old\n>> older"}, {Kind: Para, Text: "More."}},
		Signature: "Name\nhttps://example.test"}
	text, html := render(t, d)
	if text != "Thanks!\n\n> old\n>> older\nMore."+SigSep+"Name\nhttps://example.test" {
		t.Fatalf("text %q", text)
	}
	if !strings.Contains(html, "border-left:3px solid #e3d7fb") || !strings.Contains(html, ">old<br>> older<") && !strings.Contains(html, ">old<br>&gt; older<") {
		t.Fatalf("no quote cell: %s", html)
	}
	if strings.Contains(html, "not shown") {
		t.Fatal("a personal letter has no footer text")
	}
	checkSafe(t, testBrand, html)
}

func TestLayoutSize(t *testing.T) {
	long := strings.Repeat("A long reply line with words. ", 340)[:10000]
	_, html := render(t, Doc{Brand: testBrand, Personal: true, Blocks: []Block{{Kind: Para, Text: long}}, Signature: "S"})
	if len(html) > 100*1024 {
		t.Fatalf("reply html is %d bytes", len(html))
	}
	_, html = render(t, Doc{Brand: testBrand, Blocks: []Block{{Kind: Excerpt, Text: strings.ReplaceAll(long[:5000], ". ", ".\n")}}})
	if len(html) > 100*1024 {
		t.Fatalf("notification html is %d bytes", len(html))
	}
}

func TestRenderedDocBuildsBothParts(t *testing.T) {
	text, html := render(t, Doc{Brand: testBrand, Subject: "Hi", Blocks: []Block{{Kind: Para, Text: "Hello"}}})
	s := NewSMTP(Config{From: "Site <site@example.test>"})
	l, err := s.letterOf(Message{To: "a@example.test", Subject: "Re: hi\r\nBcc: evil@example.test", Text: text, HTML: html})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := build(l)
	if err != nil {
		t.Fatal(err)
	}
	h := headersOf(t, string(raw))
	if h.Get("Bcc") != "" || h.Get("From") != `"Site" <site@example.test>` || !strings.HasPrefix(h.Get("Message-ID"), "<") {
		t.Fatalf("headers: %v", h)
	}
	if s := string(raw); !strings.Contains(s, "text/plain; charset=utf-8") || !strings.Contains(s, "text/html; charset=utf-8") {
		t.Fatal("both parts must be present")
	}
}

func TestLongWordsWrap(t *testing.T) {
	word := strings.Repeat("x", 3001)
	url := "https://docs.google.com/document/d/" + strings.Repeat("A1b2C3d4", 13) + "/edit"
	cells := map[string]string{"TITLE": "", "PARA": "", "EXCERPT": "", "QUOTE": "", "FIELD": "", "MONO": ""}
	for k := range cells {
		cells[k] = k + word + " " + url
	}
	_, html := render(t, Doc{Brand: testBrand, Title: cells["TITLE"], Blocks: []Block{
		{Kind: Para, Text: cells["PARA"]}, {Kind: Excerpt, Text: cells["EXCERPT"]}, {Kind: Quote, Text: "> " + cells["QUOTE"]},
		{Kind: Fields, Fields: []Field{{Label: "Name", Value: cells["FIELD"]}, {Label: "Email", Value: cells["MONO"], Mono: true}}},
	}})
	for k := range cells {
		i := strings.Index(html, k+"xxx")
		if i < 0 {
			t.Fatalf("%s not rendered", k)
		}
		open := max(strings.LastIndex(html[:i], "<td "), strings.LastIndex(html[:i], "<div "))
		if tag := html[open:i]; !strings.Contains(tag, "overflow-wrap:anywhere;word-break:break-word;word-wrap:break-word") {
			t.Errorf("%s cell does not wrap long words: %.200s", k, tag)
		}
	}
	if strings.Count(html, "table-layout:fixed") < 2 {
		t.Fatal("the fields and quote tables are not fixed")
	}
}

func TestDefang(t *testing.T) {
	join := func(ps []Piece) (string, string) {
		var plain, marked strings.Builder
		for _, p := range ps {
			plain.WriteString(p.Text)
			if p.Sep {
				marked.WriteString("[" + p.Text + "]")
			} else {
				marked.WriteString(p.Text)
			}
		}
		return plain.String(), marked.String()
	}
	for in, want := range map[string]string{
		"Hello there. Mr. Kim":          "Hello there. Mr. Kim",
		"see https://evil.example/x ok": "see https[:]//evil[.]example/x ok",
		"mail a@b.co":                   "mail a[@]b[.]co",
		"www.x.test":                    "www[.]x[.]test",
		"exam．com":                      "exam[．]com",
		"":                              "",
	} {
		plain, marked := join(defang(in))
		if plain != in || marked != want {
			t.Errorf("defang(%q) = %q, want %q", in, marked, want)
		}
	}
}

// TestBrandNameIsOurLink: mail apps turn a bare domain into their own blue
// link, so wherever the brand name shows in the HTML it is the text of a span
// inside our own styled link to the site, never a bare text node.
func TestBrandNameIsOurLink(t *testing.T) {
	for _, personal := range []bool{false, true} {
		text, html := render(t, Doc{Brand: testBrand, Personal: personal, Subject: "S", Footer: "F",
			Blocks: []Block{{Kind: Para, Text: "Hello"}}})
		badge := `<a class="brand-a" href="https://example.test" style="color:#fff5eb;text-decoration:none;">` +
			`<span class="brand-a" style="color:#fff5eb;text-decoration:none;">example.test</span></a>`
		if !strings.Contains(html, badge) {
			t.Fatalf("personal=%v: no styled badge link in %s", personal, html)
		}
		footer := `<a class="soft" href="https://example.test" style="color:#6b6585;text-decoration:none;">` +
			`<span class="soft" style="color:#6b6585;text-decoration:none;">example.test</span></a>`
		if !strings.Contains(html, footer) {
			t.Fatalf("personal=%v: no styled footer link", personal)
		}
		body := html[strings.Index(html, "<body"):]
		for _, m := range textNodeRe.FindAllStringSubmatchIndex(body, -1) {
			node := body[m[2]:m[3]]
			if !strings.Contains(node, "example.test") {
				continue
			}
			open := body[strings.LastIndex(body[:m[0]+1], "<") : m[0]+1]
			if !strings.HasPrefix(open, `<span class="brand-a"`) && !strings.HasPrefix(open, `<span class="soft"`) {
				t.Errorf("personal=%v: bare brand text after %s", personal, open)
			}
		}
		if strings.Contains(text, "<a") {
			t.Fatal("the text part has no links")
		}
	}
	// A brand that is no http(s) site is split so it does not read as a domain.
	_, html := render(t, Doc{Brand: Brand{Name: "evil.example"}, Blocks: []Block{{Kind: Para, Text: "x"}}})
	if strings.Contains(html, ">evil.example<") || strings.Contains(html, "href=") {
		t.Fatal("bare brand without a site")
	}
}

// textNodeRe is a text node between two tags.
var textNodeRe = regexp.MustCompile(`>([^<]+)<`)
