package main

import (
	"bytes"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func pngSquare(n int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, n, n)))
	return b.Bytes()
}

func pngRect(w, h int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	return b.Bytes()
}

func (c *harness) brandPut(slot, field, name string, content []byte) *httptest.ResponseRecorder {
	c.t.Helper()
	return c.brandPutAt("/api/admin/brand/"+slot, field, name, content)
}

func (c *harness) brandPutAt(path, field, name string, content []byte) *httptest.ResponseRecorder {
	c.t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, _ := mw.CreateFormFile(field, name)
	_, _ = fw.Write(content)
	_ = mw.Close()
	return c.raw("PUT", path, &b, map[string]string{"Content-Type": mw.FormDataContentType()})
}

var brandSlots = []string{"favicon_ico", "favicon_svg", "apple_touch_icon", "wordmark_primary", "wordmark_inverse",
	"wordmark_navy", "wordmark_cream", "monogram_primary", "monogram_inverse", "monogram_navy", "monogram_cream", "email_logo"}

func TestContractBrand(t *testing.T) {
	c := newHarness(t)

	w := c.raw("GET", "/api/v1/brand", nil, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("GET /brand = %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	assets, _ := field(decodeData(t, w, brandShape), "assets").(map[string]any)
	for _, slot := range brandSlots {
		a := assets[slot]
		u, _ := field(a, "url").(string)
		if field(a, "slot") != slot || field(a, "custom") != false || !strings.HasPrefix(u, "/media/brand/") {
			t.Errorf("%s = %v", slot, a)
		}
		// The default was uploaded at startup and is served.
		if fw := c.raw("GET", u, nil, nil); fw.Code != 200 || fw.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("GET %s = %d %v", u, fw.Code, fw.Header())
		}
	}
	if ct := field(assets["favicon_ico"], "content_type"); ct != "image/x-icon" {
		t.Errorf("favicon_ico content_type = %v", ct)
	}

	w = c.raw("GET", "/api/v1/brand/favicon_svg", nil, nil)
	if w.Code != http.StatusFound || w.Header().Get("Location") != field(assets["favicon_svg"], "url") ||
		w.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Errorf("redirect = %d %v", w.Code, w.Header())
	}
	c.fail("GET", "/api/v1/brand/nope", nil, 404, "not_found")

	list := c.ok("GET", "/api/admin/brand", nil, 200, arr(adminBrandAsset), false)
	items, _ := list.([]any)
	if len(items) != len(brandSlots) {
		t.Fatalf("admin list = %d items", len(items))
	}
	for i, slot := range brandSlots {
		if field(items[i], "slot") != slot || field(items[i], "url") != field(items[i], "default_url") ||
			field(items[i], "updated_at") != nil || field(items[i], "group") != baoopnGroup(slot) {
			t.Errorf("admin[%d] = %v", i, items[i])
		}
	}

	mediaRows := c.scalar(`SELECT count(*) FROM media`)
	jobs := c.scalar(`SELECT count(*) FROM media_jobs`)
	img := pngSquare(180)
	w = c.brandPut("apple_touch_icon", "file", "icon.png", img)
	if w.Code != 200 {
		t.Fatalf("PUT apple_touch_icon = %d %s", w.Code, w.Body)
	}
	got := decodeData(t, w, adminBrandAsset)
	newURL, _ := field(got, "url").(string)
	if field(got, "custom") != true || newURL == field(assets["apple_touch_icon"], "url") ||
		field(got, "original") != "icon.png" || field(got, "width") != 180.0 {
		t.Errorf("PUT result = %v", got)
	}
	if fw := c.raw("GET", newURL, nil, nil); fw.Code != 200 || !bytes.Equal(fw.Body.Bytes(), img) ||
		fw.Header().Get("Content-Type") != "image/png" {
		t.Errorf("stored file = %d, bytes equal %v", fw.Code, bytes.Equal(fw.Body.Bytes(), img))
	}
	pub := c.ok("GET", "/api/v1/brand", nil, 200, brandShape, false)
	if a := field(field(pub, "assets"), "apple_touch_icon"); field(a, "url") != newURL || field(a, "custom") != true {
		t.Errorf("public after PUT = %v", a)
	}
	if c.scalar(`SELECT count(*) FROM media`) != mediaRows || c.scalar(`SELECT count(*) FROM media_jobs`) != jobs {
		t.Error("brand upload made a media row or job")
	}

	c.failRaw(c.brandPut("favicon_ico", "file", "x.ico", pngSquare(48)), "PUT png as favicon_ico", 415, "unsupported_type")
	c.failRaw(c.brandPut("email_logo", "file", "x.png", pngSquare(300)), "PUT square email logo", 422, "validation")
	c.failRaw(c.brandPut("apple_touch_icon", "file", "x.png", pngSquare(192)), "PUT 192px apple icon", 422, "validation")
	c.failRaw(c.brandPut("monogram_primary", "file", "x.svg",
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><script>alert(1)</script></svg>`)),
		"PUT svg with script", 422, "validation")
	c.failRaw(c.brandPut("favicon_svg", "file", "x.svg", append([]byte("<svg "), bytes.Repeat([]byte(" "), 300<<10)...)),
		"PUT oversized svg", 413, "too_large")
	c.failRaw(c.brandPut("favicon_svg", "other", "x.svg", []byte("<svg/>")), "PUT without file", 400, "bad_request")
	c.failRaw(c.brandPut("nope", "file", "x.png", img), "PUT unknown slot", 404, "not_found")
	c.fail("PUT", "/api/admin/brand/favicon_svg", "{}", 400, "bad_request")

	w = c.raw("DELETE", "/api/admin/brand/apple_touch_icon", nil, nil)
	if w.Code != 200 {
		t.Fatalf("DELETE = %d %s", w.Code, w.Body)
	}
	reset := decodeData(t, w, adminBrandAsset)
	if field(reset, "custom") != false || field(reset, "url") != field(reset, "default_url") || field(reset, "updated_at") != nil {
		t.Errorf("reset = %v", reset)
	}
	c.ok("DELETE", "/api/admin/brand/apple_touch_icon", nil, 200, adminBrandAsset, false)
	c.fail("DELETE", "/api/admin/brand/nope", nil, 404, "not_found")
	// The replaced file stays for 7 days (pages and caches may still name it).
	if fw := c.raw("GET", newURL, nil, nil); fw.Code != 200 {
		t.Errorf("retired file = %d", fw.Code)
	}
}

// baoopnGroup is the admin group of a baoopn slot.
func baoopnGroup(slot string) string {
	switch {
	case slot == "email_logo":
		return "email"
	case strings.HasPrefix(slot, "wordmark_"), strings.HasPrefix(slot, "monogram_"):
		return "logos"
	default:
		return "favicons"
	}
}

// listeningGroup is the admin group of a listening slot.
func listeningGroup(slot string) string {
	switch {
	case slot == "email_logo":
		return "email"
	case slot == "og_image":
		return "social"
	case strings.HasPrefix(slot, "mark_"), strings.HasPrefix(slot, "wordmark_"):
		return "logos"
	case strings.HasPrefix(slot, "icon_") && (strings.HasSuffix(slot, "_navy") || strings.HasSuffix(slot, "_cream")):
		return "icons"
	default:
		return "favicons"
	}
}

func TestContractBrandListening(t *testing.T) {
	c := newHarness(t)
	const pub, adm = "/api/v1/brand/listening", "/api/admin/brand/listening"

	w := c.raw("GET", pub, nil, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=60" || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("GET %s = %d %v", pub, w.Code, w.Header())
	}
	assets, _ := field(decodeData(t, w, listeningBrandShape), "assets").(map[string]any)
	if len(assets) != len(listeningBrandSlots) {
		t.Fatalf("assets = %d, want %d", len(assets), len(listeningBrandSlots))
	}
	for _, slot := range listeningBrandSlots {
		a := assets[slot]
		u, _ := field(a, "url").(string)
		if field(a, "slot") != slot || field(a, "custom") != false || !strings.HasPrefix(u, "/media/brand/listening/") {
			t.Errorf("%s = %v", slot, a)
		}
		if fw := c.raw("GET", u, nil, nil); fw.Code != 200 || fw.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("GET %s = %d %v", u, fw.Code, fw.Header())
		}
	}
	if a := assets["og_image"]; field(a, "width") != 1200.0 || field(a, "height") != 630.0 {
		t.Errorf("og_image = %v", a)
	}

	w = c.raw("GET", pub+"/icon_now_playing_navy", nil, nil)
	if w.Code != http.StatusFound || w.Header().Get("Location") != field(assets["icon_now_playing_navy"], "url") ||
		w.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Errorf("redirect = %d %v", w.Code, w.Header())
	}
	c.fail("GET", pub+"/nope", nil, 404, "not_found")
	c.fail("GET", pub+"/monogram_primary", nil, 404, "not_found")
	c.fail("GET", "/api/v1/brand/icon_192", nil, 404, "not_found")
	if w := c.raw("GET", pub+"/", nil, nil); w.Code != 404 {
		t.Errorf("GET %s/ = %d", pub, w.Code)
	}

	list := c.ok("GET", adm, nil, 200, arr(adminBrandAsset), false)
	items, _ := list.([]any)
	if len(items) != len(listeningBrandSlots) {
		t.Fatalf("admin list = %d items", len(items))
	}
	for i, slot := range listeningBrandSlots {
		if field(items[i], "slot") != slot || field(items[i], "group") != listeningGroup(slot) ||
			field(items[i], "url") != field(items[i], "default_url") {
			t.Errorf("admin[%d] = %v", i, items[i])
		}
	}

	img := pngSquare(192)
	w = c.brandPutAt(adm+"/icon_192", "file", "app.png", img)
	if w.Code != 200 {
		t.Fatalf("PUT icon_192 = %d %s", w.Code, w.Body)
	}
	got := decodeData(t, w, adminBrandAsset)
	newURL, _ := field(got, "url").(string)
	if field(got, "custom") != true || !strings.HasPrefix(newURL, "/media/brand/listening/icon-192.") ||
		field(got, "group") != "favicons" || field(got, "width") != 192.0 {
		t.Errorf("PUT result = %v", got)
	}
	if fw := c.raw("GET", newURL, nil, nil); fw.Code != 200 || !bytes.Equal(fw.Body.Bytes(), img) {
		t.Errorf("stored file = %d", fw.Code)
	}
	p := c.ok("GET", pub, nil, 200, listeningBrandShape, false)
	if a := field(field(p, "assets"), "icon_192"); field(a, "url") != newURL || field(a, "custom") != true {
		t.Errorf("public after PUT = %v", a)
	}
	// The baoopn brand is untouched.
	if a := field(field(c.ok("GET", "/api/v1/brand", nil, 200, brandShape, false), "assets"), "favicon_ico"); field(a, "custom") != false {
		t.Errorf("baoopn after listening PUT = %v", a)
	}
	if n := c.scalar(`SELECT count(*) FROM settings WHERE key = 'brand_listening'`); n != int64(1) {
		t.Errorf("brand_listening rows = %d", n)
	}

	c.failRaw(c.brandPutAt(adm+"/favicon_ico", "file", "x.png", pngSquare(48)), "PUT png as favicon_ico", 415, "unsupported_type")
	c.failRaw(c.brandPutAt(adm+"/og_image", "file", "x.png", pngRect(1200, 628)), "PUT og 1200x628", 422, "validation")
	c.failRaw(c.brandPutAt(adm+"/favicon_16", "file", "x.png", pngSquare(32)), "PUT 32px favicon_16", 422, "validation")
	c.failRaw(c.brandPutAt(adm+"/nope", "file", "x.png", img), "PUT unknown slot", 404, "not_found")
	c.failRaw(c.brandPutAt(adm+"/monogram_primary", "file", "x.png", img), "PUT baoopn-only slot", 404, "not_found")
	c.failRaw(c.brandPutAt(adm, "file", "x.png", img), "PUT the list route", 404, "not_found")

	w = c.raw("DELETE", adm+"/icon_192", nil, nil)
	if w.Code != 200 {
		t.Fatalf("DELETE = %d %s", w.Code, w.Body)
	}
	if r := decodeData(t, w, adminBrandAsset); field(r, "custom") != false || field(r, "url") != field(r, "default_url") {
		t.Errorf("reset = %v", r)
	}
	c.fail("DELETE", adm+"/nope", nil, 404, "not_found")
	if fw := c.raw("GET", newURL, nil, nil); fw.Code != 200 {
		t.Errorf("retired file = %d", fw.Code)
	}
}
