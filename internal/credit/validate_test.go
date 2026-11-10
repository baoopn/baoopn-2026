package credit

import (
	"fmt"
	"strings"
	"testing"
)

func TestNormalizeOrigin(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"HTTPS://TLPortraitz.com/", "https://tlportraitz.com"},
		{"https://a.com:443", "https://a.com"},
		{"https://a.com:8443", "https://a.com:8443"},
		{"http://localhost:5173", "http://localhost:5173"},
		{"http://127.0.0.1", "http://127.0.0.1"},
		{"http://[::1]:80", "http://[::1]"},
		{"  https://xn--bcher-kva.de  ", "https://xn--bcher-kva.de"},
		{"https://a.com:0443", "https://a.com"},
		{"https://a.com:08443", "https://a.com:8443"},
		{"http://localhost:080", "http://localhost"},
		{"https://[2001:DB8::1]:8443", "https://[2001:db8::1]:8443"},
		{"https://my-site.co.uk", "https://my-site.co.uk"},
	} {
		if got, ok := normalizeOrigin(c.in); !ok || got != c.want {
			t.Errorf("normalizeOrigin(%q) = %q %v, want %q", c.in, got, ok, c.want)
		}
	}
	for _, in := range []string{
		"http://a.com", "https://a.com/x", "https://a.com?q", "https://a.com#f", "https://*.a.com",
		"https://u@a.com", "ftp://a.com", "https://bücher.de", "null", "", "a.com", "https://",
		"https://a.com:0", "https://a.com:99999", "https:a.com", "https://" + strings.Repeat("a", 200) + ".com",
		"https://a.com:", "https://a.com:00", "https://a.com:065536", "https://a_b.com", "https://a..com",
		"https://.a.com", "https://a.com.", "https://a%2ecom", "https://[fe80::1%25eth0]", "https://[::1",
		"https://a<b.com", `https://a"b.com`, "https://a b.com", "https://[1.2.3.4]", "https://a.com:x",
	} {
		if got, ok := normalizeOrigin(in); ok {
			t.Errorf("normalizeOrigin(%q) = %q, want rejected", in, got)
		}
	}
}

func valid() Input {
	return Input{Text: "Designed & built by", URL: "https://baoopn.com", Logo: "wordmark"}
}

func TestValidate(t *testing.T) {
	in := valid()
	in.Text = "  Made by  "
	in.URL = " https://baoopn.com/about "
	in.Origins = []string{"https://b.com", "HTTPS://A.com", "https://b.com/", "https://a.com:443"}
	d, problem := in.validate()
	if problem != "" || d.Text != "Made by" || d.URL != "https://baoopn.com/about" ||
		strings.Join(d.Origins, " ") != "https://b.com https://a.com" {
		t.Fatalf("validate = %+v %q", d, problem)
	}
	if d, _ := valid().validate(); d.Origins == nil {
		t.Error("nil origins, want []")
	}

	many := make([]string, 0, 51)
	for i := range 51 {
		many = append(many, fmt.Sprintf("https://s%d.com", i))
	}
	dupes := append(append([]string{}, many[:50]...), "https://s0.com")
	for name, c := range map[string]struct {
		change func(*Input)
		want   string
	}{
		"newline":      {func(i *Input) { i.Text = "a\nb" }, msgText},
		"empty text":   {func(i *Input) { i.Text = "   " }, msgText},
		"81 runes":     {func(i *Input) { i.Text = strings.Repeat("é", 81) }, msgText},
		"tab":          {func(i *Input) { i.Text = "a\tb" }, msgText},
		"line sep":     {func(i *Input) { i.Text = "a\u2028b" }, msgText},
		"para sep":     {func(i *Input) { i.Text = "a\u2029b" }, msgText},
		"bidi RLO":     {func(i *Input) { i.Text = "a\u202eb" }, msgText},
		"bidi LRE":     {func(i *Input) { i.Text = "a\u202ab" }, msgText},
		"bidi isolate": {func(i *Input) { i.Text = "a\u2066b\u2069" }, msgText},
		"zero width":   {func(i *Input) { i.Text = "a\u200bb" }, msgText},
		"zwj":          {func(i *Input) { i.Text = "a\u200db" }, msgText},
		"bom":          {func(i *Input) { i.Text = "\ufeffab" }, msgText},
		"http url":     {func(i *Input) { i.URL = "http://baoopn.com" }, msgURL},
		"no scheme":    {func(i *Input) { i.URL = "//x" }, msgURL},
		"javascript":   {func(i *Input) { i.URL = "javascript:alert(1)" }, msgURL},
		"userinfo":     {func(i *Input) { i.URL = "https://u:p@baoopn.com" }, msgURL},
		"long url":     {func(i *Input) { i.URL = "https://a.com/" + strings.Repeat("x", 500) }, msgURL},
		"logo":         {func(i *Input) { i.Logo = "Mark" }, msgLogo},
		"no logo":      {func(i *Input) { i.Logo = "" }, msgLogo},
		"51 origins":   {func(i *Input) { i.Origins = many }, msgOriginsMax},
		"text first":   {func(i *Input) { i.Text, i.URL = "", "x" }, msgText},
		"bad origin":   {func(i *Input) { i.Origins = []string{"https://a.com", "http://b.com"} }, `origins[1] "http://b.com" must be an origin`},
		"long raw":     {func(i *Input) { i.Origins = []string{strings.Repeat("y", 70)} }, `origins[0] "` + strings.Repeat("y", 60) + `" must`},
		"wildcard":     {func(i *Input) { i.Origins = []string{"https://*.a.com"} }, `origins[0] "https://*.a.com"`},
		"origin first": {func(i *Input) { i.Origins = append(append([]string{}, many...), "ftp://x") }, `origins[51] "ftp://x"`},
	} {
		in := valid()
		c.change(&in)
		if _, got := in.validate(); !strings.HasPrefix(got, c.want) {
			t.Errorf("%s: problem %q, want prefix %q", name, got, c.want)
		}
	}
	// 51 entries that dedupe to 50 are fine.
	in = valid()
	in.Origins = dupes
	if d, problem := in.validate(); problem != "" || len(d.Origins) != 50 {
		t.Errorf("dupes: %d %q", len(d.Origins), problem)
	}
}
