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
	return Input{URL: "https://baoopn.com", Logo: "wordmark", Variants: map[string]VariantInput{
		LogoWordmark: {Prefix: "Designed & built by", Style: "color"},
		LogoMark:     {Prefix: "by", Suffix: "baoopn", Style: "mono"},
	}}
}

func ptr(s string) *string { return &s }

// setVariant changes one field of one variant of in.
func setVariant(in *Input, kind string, f func(*VariantInput)) {
	v := in.Variants[kind]
	f(&v)
	in.Variants[kind] = v
}

func TestValidate(t *testing.T) {
	in := valid()
	in.URL = " https://baoopn.com/about "
	in.Origins = []string{"https://b.com", "HTTPS://A.com", "https://b.com/", "https://a.com:443"}
	setVariant(&in, LogoWordmark, func(v *VariantInput) { v.Prefix, v.Suffix, v.Style = "  Made by  ", "  x ", "" })
	d, problem := in.validate()
	if problem != "" || d.URL != "https://baoopn.com/about" ||
		d.Variants[LogoWordmark] != (Variant{"Made by", "x", StyleColor}) ||
		d.Variants[LogoMark] != (Variant{"by", "baoopn", StyleMono}) ||
		strings.Join(d.Origins, " ") != "https://b.com https://a.com" {
		t.Fatalf("validate = %+v %q", d, problem)
	}
	if d, _ := valid().validate(); d.Origins == nil {
		t.Error("nil origins, want []")
	}
	// Empty prefix and suffix are fine.
	in = valid()
	setVariant(&in, LogoMark, func(v *VariantInput) { v.Prefix, v.Suffix = "", "" })
	if d, problem := in.validate(); problem != "" || d.Variants[LogoMark] != (Variant{"", "", StyleMono}) {
		t.Errorf("empty lines = %+v %q", d, problem)
	}
	// text is ignored when variants are there.
	in = valid()
	in.Text = ptr("a\nb")
	if d, problem := in.validate(); problem != "" || d.Variants[LogoWordmark].Prefix != "Designed & built by" {
		t.Errorf("text with variants = %+v %q", d, problem)
	}
	// The old body: text is the prefix of both variants, in color.
	old := Input{Text: ptr(" by "), URL: "https://baoopn.com", Logo: "mark"}
	if d, problem := old.validate(); problem != "" || d.Logo != LogoMark ||
		d.Variants[LogoWordmark] != (Variant{"by", "", StyleColor}) || d.Variants[LogoMark] != (Variant{"by", "", StyleColor}) {
		t.Errorf("old body = %+v %q", d, problem)
	}
	old.Text = nil
	if d, problem := old.validate(); problem != "" || d.Variants[LogoMark] != (Variant{defaultPrefix, "", StyleColor}) {
		t.Errorf("no text, no variants = %+v %q", d, problem)
	}

	many := make([]string, 0, 51)
	for i := range 51 {
		many = append(many, fmt.Sprintf("https://s%d.com", i))
	}
	dupes := append(append([]string{}, many[:50]...), "https://s0.com")
	prefix := func(kind, s string) func(*Input) {
		return func(i *Input) { setVariant(i, kind, func(v *VariantInput) { v.Prefix = s }) }
	}
	msgWP := "variants.wordmark.prefix must be at most 80 characters on one line"
	for name, c := range map[string]struct {
		change func(*Input)
		want   string
	}{
		"newline":        {prefix(LogoWordmark, "a\nb"), msgWP},
		"81 runes":       {prefix(LogoWordmark, strings.Repeat("é", 81)), msgWP},
		"tab":            {prefix(LogoWordmark, "a\tb"), msgWP},
		"line sep":       {prefix(LogoWordmark, "a\u2028b"), msgWP},
		"para sep":       {prefix(LogoWordmark, "a\u2029b"), msgWP},
		"bidi RLO":       {prefix(LogoWordmark, "a\u202eb"), msgWP},
		"bidi LRE":       {prefix(LogoWordmark, "a\u202ab"), msgWP},
		"bidi isolate":   {prefix(LogoWordmark, "a\u2066b\u2069"), msgWP},
		"zero width":     {prefix(LogoWordmark, "a\u200bb"), msgWP},
		"zwj":            {prefix(LogoWordmark, "a\u200db"), msgWP},
		"bom":            {prefix(LogoWordmark, "\ufeffab"), msgWP},
		"mark prefix":    {prefix(LogoMark, "a\nb"), "variants.mark.prefix must be at most 80 characters on one line"},
		"mark suffix":    {func(i *Input) { setVariant(i, LogoMark, func(v *VariantInput) { v.Suffix = "\u200b" }) }, "variants.mark.suffix must be at most 80 characters on one line"},
		"wordmark style": {func(i *Input) { setVariant(i, LogoWordmark, func(v *VariantInput) { v.Style = "big" }) }, "variants.wordmark.style must be color or mono"},
		"mark style":     {func(i *Input) { setVariant(i, LogoMark, func(v *VariantInput) { v.Style = "MONO" }) }, "variants.mark.style must be color or mono"},
		"wordmark first": {func(i *Input) { prefix(LogoMark, "a\nb")(i); prefix(LogoWordmark, "a\nb")(i) }, msgWP},
		"prefix first": {func(i *Input) {
			setVariant(i, LogoWordmark, func(v *VariantInput) { v.Prefix, v.Suffix, v.Style = "a\nb", "a\nb", "big" })
		}, msgWP},
		"no mark":       {func(i *Input) { delete(i.Variants, LogoMark) }, msgVariants},
		"empty map":     {func(i *Input) { i.Variants = map[string]VariantInput{} }, msgVariants},
		"extra key":     {func(i *Input) { i.Variants["big"] = VariantInput{} }, msgVariants},
		"swapped key":   {func(i *Input) { delete(i.Variants, LogoMark); i.Variants["Mark"] = VariantInput{} }, msgVariants},
		"old bad text":  {func(i *Input) { i.Variants, i.Text = nil, ptr("a\nb") }, msgText},
		"old long text": {func(i *Input) { i.Variants, i.Text = nil, ptr(strings.Repeat("x", 81)) }, msgText},
		"http url":      {func(i *Input) { i.URL = "http://baoopn.com" }, msgURL},
		"no scheme":     {func(i *Input) { i.URL = "//x" }, msgURL},
		"javascript":    {func(i *Input) { i.URL = "javascript:alert(1)" }, msgURL},
		"userinfo":      {func(i *Input) { i.URL = "https://u:p@baoopn.com" }, msgURL},
		"long url":      {func(i *Input) { i.URL = "https://a.com/" + strings.Repeat("x", 500) }, msgURL},
		"logo":          {func(i *Input) { i.Logo = "Mark" }, msgLogo},
		"no logo":       {func(i *Input) { i.Logo = "" }, msgLogo},
		"url first":     {func(i *Input) { i.URL, i.Logo = "x", "" }, msgURL},
		"logo first":    {func(i *Input) { i.Logo = ""; delete(i.Variants, LogoMark) }, msgLogo},
		"variants 1st":  {func(i *Input) { prefix(LogoMark, "a\nb")(i); i.Origins = []string{"ftp://x"} }, "variants.mark.prefix"},
		"51 origins":    {func(i *Input) { i.Origins = many }, msgOriginsMax},
		"bad origin":    {func(i *Input) { i.Origins = []string{"https://a.com", "http://b.com"} }, `origins[1] "http://b.com" must be an origin`},
		"long raw":      {func(i *Input) { i.Origins = []string{strings.Repeat("y", 70)} }, `origins[0] "` + strings.Repeat("y", 60) + `" must`},
		"wildcard":      {func(i *Input) { i.Origins = []string{"https://*.a.com"} }, `origins[0] "https://*.a.com"`},
		"origin first":  {func(i *Input) { i.Origins = append(append([]string{}, many...), "ftp://x") }, `origins[51] "ftp://x"`},
	} {
		in := valid()
		c.change(&in)
		if _, got := in.validate(); got == "" || !strings.HasPrefix(got, c.want) {
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

func TestValidLine(t *testing.T) {
	for in, want := range map[string]string{"": "", "  ": "", " by ": "by", strings.Repeat("é", 80): strings.Repeat("é", 80)} {
		if got, ok := validLine(in); !ok || got != want {
			t.Errorf("validLine(%q) = %q %v", in, got, ok)
		}
	}
	for _, in := range []string{strings.Repeat("é", 81), "a\nb", "a\u2028b", "a\u202eb", "\u200b", "a\xffb"} {
		if got, ok := validLine(in); ok {
			t.Errorf("validLine(%q) = %q, want rejected", in, got)
		}
	}
}
