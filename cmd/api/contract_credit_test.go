package main

import (
	"strings"
	"testing"
)

var (
	creditImage = obj(fields{"url": str, "width": num, "height": num, "content_type": str})
	creditLogo  = obj(fields{"kind": enum("wordmark", "mark"), "light": creditImage, "dark": creditImage})
	creditShape = obj(fields{"text": str, "url": str, "alt": str, "logo": creditLogo, "updated_at": null(ts)})
	adminCredit = obj(fields{"text": str, "url": str, "logo": enum("wordmark", "mark"), "origins": arr(str),
		"updated_at": null(ts), "endpoint": str,
		"logos":  obj(fields{"wordmark": creditLogo, "mark": creditLogo}),
		"limits": obj(fields{"text_max": num, "url_max": num, "origins_max": num})})
)

func creditInput(origins ...string) map[string]any {
	if origins == nil {
		origins = []string{}
	}
	return map[string]any{"text": "Designed & built by", "url": "https://baoopn.com", "logo": "mark", "origins": origins}
}

func TestContractCredit(t *testing.T) {
	c := newHarness(t)

	w := c.raw("GET", "/api/v1/credit", nil, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=300" || w.Header().Get("Vary") != "Origin" ||
		w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("GET /credit = %d %v", w.Code, w.Header())
	}
	pub := decodeData(t, w, creditShape)
	if field(pub, "text") != "Designed & built by" || field(pub, "url") != "https://baoopn.com" ||
		field(pub, "alt") != "baoopn.com" || field(pub, "updated_at") != nil {
		t.Errorf("default credit = %v", pub)
	}
	logo := field(pub, "logo")
	light, _ := field(field(logo, "light"), "url").(string)
	if field(logo, "kind") != "wordmark" || !strings.HasPrefix(light, "https://baoopn.com/media/brand/") ||
		field(field(logo, "dark"), "url") != light {
		t.Errorf("default logo = %v", logo)
	}

	w = c.raw("GET", "/api/admin/credit", nil, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET admin = %d %v", w.Code, w.Header())
	}
	adm := decodeData(t, w, adminCredit)
	if field(adm, "endpoint") != "https://baoopn.com/api/v1/credit" || len(list(field(adm, "origins"))) != 0 ||
		field(field(adm, "limits"), "origins_max") != 50.0 {
		t.Errorf("admin default = %v", adm)
	}
	mark := field(field(adm, "logos"), "mark")
	if field(field(mark, "light"), "url") == field(field(mark, "dark"), "url") {
		t.Errorf("mark light and dark share a file: %v", mark)
	}

	for name, body := range map[string]map[string]any{
		"text":    {"text": "a\nb"},
		"url":     {"url": "http://baoopn.com"},
		"logo":    {"logo": "big"},
		"origins": {"origins": []string{"https://a.com/path"}},
	} {
		in := creditInput()
		for k, v := range body {
			in[k] = v
		}
		w := c.raw("PUT", "/api/admin/credit", jsonBody(in), nil)
		c.failRaw(w, "PUT bad "+name, 422, "validation")
		if !strings.HasPrefix(w.Body.String(), `{"error":{"code":"validation","message":"`+name) {
			t.Errorf("PUT bad %s message = %s", name, w.Body)
		}
	}
	c.fail("PUT", "/api/admin/credit", `{"text":"x","url":"https://a.com","logo":"mark","origins":[],"extra":1}`, 400, "bad_request")

	got := c.ok("PUT", "/api/admin/credit",
		creditInput("HTTPS://TLPortraitz.com/", "https://tlportraitz.com", "http://localhost:5173"), 200, adminCredit, false)
	if o := list(field(got, "origins")); len(o) != 2 || o[0] != "https://tlportraitz.com" || o[1] != "http://localhost:5173" ||
		field(got, "logo") != "mark" || field(got, "updated_at") == nil {
		t.Errorf("PUT = %v", got)
	}
	// origins null means none.
	if got := c.ok("PUT", "/api/admin/credit",
		map[string]any{"text": "x", "url": "https://a.com", "logo": "mark", "origins": nil}, 200, adminCredit, false); len(list(field(got, "origins"))) != 0 {
		t.Errorf("PUT null origins = %v", got)
	}
	c.ok("PUT", "/api/admin/credit", creditInput("https://tlportraitz.com"), 200, adminCredit, false)

	w = c.raw("GET", "/api/v1/credit", nil, map[string]string{"Origin": "https://tlportraitz.com"})
	if w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != "https://tlportraitz.com" ||
		w.Header().Get("Vary") != "Origin" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("GET listed origin = %d %v", w.Code, w.Header())
	}
	pub = decodeData(t, w, creditShape)
	if field(field(pub, "logo"), "kind") != "mark" || field(pub, "updated_at") == nil {
		t.Errorf("credit after PUT = %v", pub)
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
