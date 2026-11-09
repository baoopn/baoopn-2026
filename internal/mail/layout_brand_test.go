package mail

import (
	"strings"
	"testing"
)

// TestSignatureLinks: a signature line that is exactly an http(s) URL is our
// own styled link shown without the scheme; every other line stays escaped
// text, so javascript:, data: and the like are never links.
func TestSignatureLinks(t *testing.T) {
	sig := "Sam Owner\nhttps://example.test/\nhttps://example.test/blog?a=1&b=2\njavascript:alert(1)\n" +
		"https://example.test evil\ndata:text/html,x\nftp://example.test\n//example.test/x\nhttps://u@example.test/\n<b>x</b>"
	_, html := render(t, Doc{Brand: testBrand, Personal: true, Blocks: []Block{{Kind: Para, Text: "Hi"}}, Signature: sig})
	checkSafe(t, testBrand, html)
	link := func(href, text string) string {
		return `<a class="sig-a" href="` + href + `" style="color:#0b0537;font-weight:600;text-decoration:none;">` +
			`<span class="sig-a" style="color:#0b0537;font-weight:600;text-decoration:none;">` + text + `</span></a>`
	}
	for _, want := range []string{
		`font-size:15px;font-weight:600;color:#3b3552;">Sam Owner</div>`,
		link("https://example.test/", "example.test"),
		link("https://example.test/blog?a=1&amp;b=2", "example.test/blog?a=1&amp;b=2"),
		`>javascript:alert(1)</div>`, `>https://example.test evil</div>`, `>data:text/html,x</div>`,
		`>ftp://example.test</div>`, `>//example.test/x</div>`, `>https://u@example.test/</div>`,
		`>&lt;b&gt;x&lt;/b&gt;</div>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s", want)
		}
	}
	if n := strings.Count(html, `class="sig-a" href=`); n != 2 {
		t.Errorf("%d signature links, want 2", n)
	}
	for _, l := range []string{"https://example.test", "http://x.test/a", "HTTPS://x.test"} {
		if ls := sigLines(l); len(ls) != 1 || ls[0].URL != l {
			t.Errorf("sigLines(%q) = %+v", l, ls)
		}
	}
	for _, l := range []string{"https://", "https:///x", "mailto:a@example.test", "https://x.test/\"onmouseover=x"} {
		if ls := sigLines(l); ls[0].URL != "" {
			t.Errorf("%q linked: %+v", l, ls)
		}
	}
}

const testLogo = "https://media.example.test/brand/email-logo.0123456789abcdef.png"

func TestEmailLogo(t *testing.T) {
	for _, personal := range []bool{false, true} {
		b := testBrand
		b.LogoURL, b.LogoWidth, b.LogoHeight = testLogo, 480, 198
		_, html := render(t, Doc{Brand: b, Personal: personal, Preheader: "Hello there", Kicker: "K", Title: "T",
			Blocks: []Block{{Kind: Para, Text: "Hello there"}}, Footer: "F"})
		checkSafe(t, b, html)
		img := `<a href="https://example.test" style="display:inline-block;text-decoration:none;border:0;"><img class="logo" src="` +
			testLogo + `" width="116" height="48" alt="example.test" style="display:block;`
		if !strings.Contains(html, img) || strings.Contains(html, `class="brand"`) {
			t.Fatalf("personal=%v: no linked logo in place of the badge: %s", personal, html)
		}
		if i, j := strings.Index(html, ">Hello there"), strings.Index(html, "<img"); i < 0 || i > j {
			t.Errorf("personal=%v: the preheader is not before the logo", personal)
		}
	}
	// Width follows the aspect ratio at 48 px, kept within 1.5:1–4:1; an
	// unknown size uses the default logo's 116.
	for _, c := range []struct{ w, h, want int }{{1200, 300, 192}, {240, 160, 72}, {100, 100, 72}, {5000, 100, 192}, {0, 0, 116}} {
		b := testBrand
		b.LogoURL, b.LogoWidth, b.LogoHeight = testLogo, c.w, c.h
		if l := b.logo(); l == nil || l.Width != c.want || l.Height != 48 {
			t.Errorf("%dx%d: %+v, want width %d", c.w, c.h, l, c.want)
		}
	}
}

// TestEmailLogoFallback: no logo, or one that is not an absolute http(s)
// URL, shows the text badge and no image.
func TestEmailLogoFallback(t *testing.T) {
	for _, u := range []string{"", "javascript:alert(1)", "data:image/png;base64,AAAA", "//media.example.test/x.png",
		"/media/brand/x.png", "https://u:p@media.example.test/x.png", "ftp://media.example.test/x.png",
		`https://media.example.test/x.png" onerror="alert(1)`, "https://media.example.test/a b.png"} {
		b := testBrand
		b.LogoURL = u
		_, html := render(t, Doc{Brand: b, Blocks: []Block{{Kind: Para, Text: "x"}}})
		if strings.Contains(strings.ToLower(html), "<img") || !strings.Contains(html, `class="brand"`) {
			t.Errorf("%q: logo shown or badge missing", u)
		}
		checkSafe(t, b, html)
	}
	// A logo without an http(s) home is shown, unlinked.
	b := Brand{Name: "example.test", LogoURL: testLogo}
	_, html := render(t, Doc{Brand: b, Blocks: []Block{{Kind: Para, Text: "x"}}})
	if !strings.Contains(html, `<img class="logo" src="`+testLogo+`"`) || strings.Contains(html, "href=") {
		t.Errorf("logo without a home: %s", html)
	}
}
