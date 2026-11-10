package main

import (
	"strings"
	"testing"
)

var (
	creditKind    = enum("wordmark", "mark")
	creditStyle   = enum("color", "mono")
	creditImage   = obj(fields{"url": str, "width": num, "height": num, "content_type": str})
	creditLogo    = obj(fields{"kind": creditKind, "style": creditStyle, "light": creditImage, "dark": creditImage})
	creditVariant = obj(fields{"prefix": str, "suffix": str, "style": creditStyle})
	creditShape   = obj(fields{"text": str, "prefix": str, "suffix": str, "url": str, "alt": str, "logo": creditLogo,
		"updated_at": null(ts)})
	creditStyles = obj(fields{"color": creditLogo, "mono": creditLogo})
	adminCredit  = obj(fields{"url": str, "logo": creditKind,
		"variants": obj(fields{"wordmark": creditVariant, "mark": creditVariant}), "origins": arr(str),
		"updated_at": null(ts), "endpoint": str,
		"logos":  obj(fields{"wordmark": creditStyles, "mark": creditStyles}),
		"limits": obj(fields{"prefix_max": num, "suffix_max": num, "url_max": num, "origins_max": num})})
)

func creditVariants() map[string]any {
	return map[string]any{
		"wordmark": map[string]any{"prefix": "Designed & built by", "suffix": "", "style": "color"},
		"mark":     map[string]any{"prefix": "by", "suffix": "baoopn", "style": "mono"},
	}
}

func creditInput(origins ...string) map[string]any {
	if origins == nil {
		origins = []string{}
	}
	return map[string]any{"url": "https://baoopn.com", "logo": "mark", "variants": creditVariants(), "origins": origins}
}

func TestContractCredit(t *testing.T) {
	c := newHarness(t)

	w := c.raw("GET", "/api/v1/credit", nil, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=300" || w.Header().Get("Vary") != "Origin" ||
		w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("GET /credit = %d %v", w.Code, w.Header())
	}
	pub := decodeData(t, w, creditShape)
	if field(pub, "text") != "Designed & built by" || field(pub, "prefix") != "Designed & built by" ||
		field(pub, "suffix") != "" || field(pub, "url") != "https://baoopn.com" ||
		field(pub, "alt") != "baoopn.com" || field(pub, "updated_at") != nil {
		t.Errorf("default credit = %v", pub)
	}
	logo := field(pub, "logo")
	light, _ := field(field(logo, "light"), "url").(string)
	if field(logo, "kind") != "wordmark" || field(logo, "style") != "color" ||
		!strings.HasPrefix(light, "https://baoopn.com/media/brand/wordmark-primary.") ||
		field(field(logo, "dark"), "url") == light {
		t.Errorf("default logo = %v", logo)
	}

	w = c.raw("GET", "/api/admin/credit", nil, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET admin = %d %v", w.Code, w.Header())
	}
	adm := decodeData(t, w, adminCredit)
	if field(adm, "endpoint") != "https://baoopn.com/api/v1/credit" || len(list(field(adm, "origins"))) != 0 ||
		field(field(adm, "limits"), "origins_max") != 50.0 || field(field(adm, "limits"), "prefix_max") != 80.0 ||
		field(field(field(adm, "variants"), "mark"), "prefix") != "Designed & built by" {
		t.Errorf("admin default = %v", adm)
	}
	for _, kind := range []string{"wordmark", "mark"} {
		for _, style := range []string{"color", "mono"} {
			l := field(field(field(adm, "logos"), kind), style)
			if field(l, "kind") != kind || field(l, "style") != style ||
				field(field(l, "light"), "url") == field(field(l, "dark"), "url") {
				t.Errorf("logos[%s][%s] = %v", kind, style, l)
			}
		}
	}

	variant := func(kind, key string, v any) map[string]any {
		vs := creditVariants()
		vs[kind].(map[string]any)[key] = v
		return map[string]any{"variants": vs}
	}
	for name, c2 := range map[string]struct {
		body map[string]any
		want string
	}{
		"url":           {map[string]any{"url": "http://baoopn.com"}, "url"},
		"logo":          {map[string]any{"logo": "big"}, "logo"},
		"variants":      {map[string]any{"variants": map[string]any{"mark": map[string]any{}}}, "variants must have"},
		"prefix":        {variant("wordmark", "prefix", strings.Repeat("x", 81)), "variants.wordmark.prefix"},
		"suffix":        {variant("mark", "suffix", "a\nb"), "variants.mark.suffix"},
		"style":         {variant("mark", "style", "big"), "variants.mark.style"},
		"text":          {map[string]any{"variants": nil, "text": "a\nb"}, "text"},
		"origins":       {map[string]any{"origins": []string{"https://a.com/path"}}, "origins"},
		"url before va": {map[string]any{"url": "x", "variants": map[string]any{}}, "url"},
	} {
		in := creditInput()
		for k, v := range c2.body {
			in[k] = v
		}
		w := c.raw("PUT", "/api/admin/credit", jsonBody(in), nil)
		c.failRaw(w, "PUT bad "+name, 422, "validation")
		if !strings.HasPrefix(w.Body.String(), `{"error":{"code":"validation","message":"`+c2.want) {
			t.Errorf("PUT bad %s message = %s", name, w.Body)
		}
	}
	c.fail("PUT", "/api/admin/credit", `{"url":"https://a.com","logo":"mark","origins":[],"extra":1}`, 400, "bad_request")
	c.fail("PUT", "/api/admin/credit", `{"url":"https://a.com","logo":"mark","origins":[],
		"variants":{"wordmark":{"prefix":"x","extra":1},"mark":{}}}`, 400, "bad_request")

	// The old body (text, no variants) still saves: text is both prefixes.
	got := c.ok("PUT", "/api/admin/credit",
		map[string]any{"text": "Made by", "url": "https://a.com", "logo": "mark", "origins": nil}, 200, adminCredit, false)
	for _, kind := range []string{"wordmark", "mark"} {
		v := field(field(got, "variants"), kind)
		if field(v, "prefix") != "Made by" || field(v, "suffix") != "" || field(v, "style") != "color" {
			t.Errorf("old body %s = %v", kind, v)
		}
	}
	// origins null means none.
	if len(list(field(got, "origins"))) != 0 {
		t.Errorf("PUT null origins = %v", got)
	}

	got = c.ok("PUT", "/api/admin/credit",
		creditInput("HTTPS://TLPortraitz.com/", "https://tlportraitz.com", "http://localhost:5173"), 200, adminCredit, false)
	if o := list(field(got, "origins")); len(o) != 2 || o[0] != "https://tlportraitz.com" || o[1] != "http://localhost:5173" ||
		field(got, "logo") != "mark" || field(got, "updated_at") == nil ||
		field(field(field(got, "variants"), "mark"), "style") != "mono" {
		t.Errorf("PUT = %v", got)
	}
	in := creditInput("https://tlportraitz.com")
	in["logo"] = "wordmark"
	c.ok("PUT", "/api/admin/credit", in, 200, adminCredit, false)

	w = c.raw("GET", "/api/v1/credit", nil, map[string]string{"Origin": "https://tlportraitz.com"})
	if w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != "https://tlportraitz.com" ||
		w.Header().Get("Vary") != "Origin" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("GET listed origin = %d %v", w.Code, w.Header())
	}
	pub = decodeData(t, w, creditShape)
	if field(field(pub, "logo"), "kind") != "wordmark" || field(pub, "prefix") != "Designed & built by" ||
		field(pub, "updated_at") == nil {
		t.Errorf("credit after PUT = %v", pub)
	}
	// ?logo=mark gives the mark variant: its own prefix, suffix and style.
	w = c.raw("GET", "/api/v1/credit?logo=mark", nil, map[string]string{"Origin": "https://tlportraitz.com"})
	pub = decodeData(t, w, creditShape)
	ml, _ := field(field(field(pub, "logo"), "light"), "url").(string)
	if w.Header().Get("Access-Control-Allow-Origin") != "https://tlportraitz.com" ||
		field(pub, "prefix") != "by" || field(pub, "text") != "by" || field(pub, "suffix") != "baoopn" ||
		field(field(pub, "logo"), "kind") != "mark" || field(field(pub, "logo"), "style") != "mono" ||
		!strings.HasPrefix(ml, "https://baoopn.com/media/brand/monogram-navy.") {
		t.Errorf("GET ?logo=mark = %v", pub)
	}
	// ?color= overrides the style for one request; the stored doc keeps mono.
	for q, want := range map[string]string{"?logo=mark&color=mono": "mono", "?logo=mark&color=color": "color",
		"?logo=mark&color=MONO": "mono", "?color=MONO": "color", "?color=mono": "mono"} {
		pub = decodeData(t, c.raw("GET", "/api/v1/credit"+q, nil, nil), creditShape)
		if field(field(pub, "logo"), "style") != want {
			t.Errorf("GET %s style = %v, want %s", q, field(field(pub, "logo"), "style"), want)
		}
	}
	adm = c.ok("GET", "/api/admin/credit", nil, 200, adminCredit, false)
	if field(field(field(adm, "variants"), "mark"), "style") != "mono" ||
		field(field(field(adm, "variants"), "wordmark"), "style") != "color" {
		t.Errorf("stored styles changed: %v", field(adm, "variants"))
	}
	w = c.raw("GET", "/api/v1/credit", nil, map[string]string{"Origin": "https://evil.example"})
	if w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Vary") != "Origin" {
		t.Errorf("GET unlisted origin = %d %v", w.Code, w.Header())
	}

	w = c.raw("OPTIONS", "/api/v1/credit", nil, map[string]string{"Origin": "https://tlportraitz.com",
		"Access-Control-Request-Method": "GET"})
	h := w.Header()
	if w.Code != 204 || w.Body.Len() != 0 || h.Get("Access-Control-Allow-Origin") != "https://tlportraitz.com" ||
		h.Get("Access-Control-Allow-Methods") != "GET, OPTIONS" || h.Get("Access-Control-Allow-Headers") != "Content-Type" ||
		h.Get("Access-Control-Max-Age") != "86400" ||
		h.Get("Vary") != "Origin, Access-Control-Request-Method, Access-Control-Request-Headers" {
		t.Errorf("OPTIONS listed = %d %v", w.Code, h)
	}
	w = c.raw("OPTIONS", "/api/v1/credit", nil, map[string]string{"Origin": "https://evil.example"})
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Errorf("OPTIONS unlisted = %d %v", w.Code, w.Header())
	}
}
