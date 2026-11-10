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
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, _ := mw.CreateFormFile(field, name)
	_, _ = fw.Write(content)
	_ = mw.Close()
	return c.raw("PUT", "/api/admin/brand/"+slot, &b, map[string]string{"Content-Type": mw.FormDataContentType()})
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
			field(items[i], "updated_at") != nil {
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
