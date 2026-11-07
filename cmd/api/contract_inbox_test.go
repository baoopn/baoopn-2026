package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func contactForm(name, email, msg string) map[string]any {
	return map[string]any{"name": name, "email": email, "subject": "", "message": msg, "website": "",
		"started_at": time.Now().Add(-20 * time.Second).UnixMilli()}
}

// inboundEML is a Gmail-style reply: multipart/alternative with quoted
// history and an attachment, addressed to the thread's reply address.
func inboundEML(token, msgID, inReplyTo string) string {
	return strings.ReplaceAll(`Return-Path: <priya@example.com>
Delivered-To: reply+`+token+`@reply.baoopn.com
From: Priya Shah <Priya@Example.com>
To: Bao Nguyen <reply+`+token+`@reply.baoopn.com>
Subject: Re: Collaboration on a side project
Date: Sat, 7 Nov 2026 09:12:44 +0700
Message-ID: `+msgID+`
In-Reply-To: `+inReplyTo+`
References: `+inReplyTo+`
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="mix"

--mix
Content-Type: multipart/alternative; boundary="alt"

--alt
Content-Type: text/plain; charset="UTF-8"
Content-Transfer-Encoding: quoted-printable

Monday at 10 works for me. I attached the brief.

On Fri, Nov 6, 2026 at 4:01 PM Bao Nguyen <bao@mail.baoopn.com> wrote:
> Thanks for reaching out! Could we talk on Monday?
--alt
Content-Type: text/html; charset="UTF-8"

<div dir=3D"ltr">Monday at 10 works for me. I attached the brief.</div><div class=3D"gmail_quote">On Fri wrote:<blockquote>Thanks</blockquote></div>
--alt--
--mix
Content-Type: application/pdf; name="brief.pdf"
Content-Disposition: attachment; filename="brief.pdf"
Content-Transfer-Encoding: base64

JVBERi0xLjQK
--mix--
`, "\n", "\r\n")
}

func TestContractContactAndInbox(t *testing.T) {
	c := newHarness(t)

	if d := c.ok("POST", "/api/v1/contact", contactForm("Priya Shah", "priya@example.com",
		"Hi Bao, would you like to collaborate on a side project?"), 201, okShape, false); field(d, "ok") != true {
		t.Errorf("contact = %v", d)
	}
	c.fail("POST", "/api/v1/contact", contactForm("Priya", "not-an-email", "a long enough message"), 422, "validation")
	// Honeypot: pretends to work, stores nothing.
	bot := contactForm("Bot", "bot@example.com", "cheap watches for sale")
	bot["website"] = "https://spam.example"
	c.ok("POST", "/api/v1/contact", bot, 201, okShape, false)
	c.wait()
	if n := c.scalar(`SELECT count(*) FROM threads`); n != int64(1) {
		t.Errorf("threads after honeypot = %v, want 1", n)
	}

	threads := list(c.ok("GET", "/api/admin/threads", nil, 200, arr(threadSummary), false))
	if len(threads) != 1 {
		t.Fatalf("threads = %v", threads)
	}
	tid := idOf(threads[0])
	base := "/api/admin/threads/" + tid
	if got := c.ok("GET", "/api/admin/threads/unread-count", nil, 200, countShape, false); field(got, "count") != float64(1) {
		t.Errorf("unread = %v", got)
	}
	c.ok("GET", "/api/admin/threads?q=priya&status=open", nil, 200, arr(threadSummary), false)
	c.empty("POST", base+"/read", nil, 204)
	c.empty("POST", base+"/unread", nil, 204)
	c.empty("POST", base+"/read", nil, 204)
	d := c.ok("GET", base, nil, 200, threadDetail, false)
	if field(d, "reply_enabled") != true {
		t.Errorf("reply_enabled = %v", field(d, "reply_enabled"))
	}

	// Reply, then the same request_id again: one row, one send.
	sendsBefore := c.mailer.count()
	reply := map[string]string{"body": "Thanks for reaching out! Could we talk on Monday?", "request_id": "req-contract-0001"}
	m1 := c.ok("POST", base+"/messages", reply, 201, threadMessage, false)
	m2 := c.ok("POST", base+"/messages", reply, 200, threadMessage, false)
	if idOf(m1) != idOf(m2) || field(m1, "status") != "sent" || c.mailer.count() != sendsBefore+1 {
		t.Errorf("idempotent reply: %v vs %v, sends %d", m1, m2, c.mailer.count()-sendsBefore)
	}
	c.fail("POST", base+"/messages/"+idOf(m1)+"/retry", nil, 409, "not_retryable")
	c.fail("POST", base+"/messages", map[string]string{"body": "", "request_id": "req-contract-0002"}, 422, "validation")

	// A failed send, then a retry.
	c.mailer.failOnce()
	f := c.ok("POST", base+"/messages", map[string]string{"body": "Second try", "request_id": "req-contract-0003"},
		201, threadMessage, false)
	if field(f, "status") != "failed" || field(f, "error") == "" {
		t.Errorf("failed send = %v", f)
	}
	if r := c.ok("POST", base+"/messages/"+idOf(f)+"/retry", nil, 200, threadMessage, false); field(r, "status") != "sent" {
		t.Errorf("retry = %v", r)
	}

	// Inbound: a raw .eml through the signed hook into the thread view.
	token, _ := c.scalar(`SELECT reply_token FROM threads WHERE id = $1`, tid).(string)
	outID, _ := c.scalar(`SELECT rfc_message_id FROM thread_messages WHERE id = $1`, idOf(m1)).(string)
	eml := inboundEML(token, "<CAF-contract-1@mail.gmail.com>", outID)
	envTo := "reply+" + token + "@" + ctInboundDomain
	if w := c.hook(eml, envTo, time.Now(), ctInboundSecret); w.Code != 204 {
		t.Fatalf("hook = %d %s", w.Code, w.Body)
	}
	if w := c.hook(eml, envTo, time.Now(), ctInboundSecret); w.Code != 204 {
		t.Errorf("duplicate delivery = %d", w.Code)
	}
	c.failRaw(c.hook(eml, envTo, time.Now(), "wrong-secret"), "hook bad signature", 401, "bad_signature")
	c.failRaw(c.hook(eml, envTo, time.Now().Add(-10*time.Minute), ctInboundSecret), "hook stale", 401, "bad_signature")
	c.wait()
	// form, reply, failed-then-retried reply, inbound email
	d = c.ok("GET", base, nil, 200, threadDetail, false)
	msgs := list(field(d, "messages"))
	last := msgs[len(msgs)-1]
	if len(msgs) != 4 || field(last, "source") != "email" || field(last, "direction") != "inbound" ||
		field(last, "body") != "Monday at 10 works for me. I attached the brief." ||
		!strings.Contains(fmt.Sprint(field(last, "full_body")), "> Thanks for reaching out!") ||
		field(last, "attachments_dropped") != float64(1) || field(last, "from_address") != "priya@example.com" ||
		field(last, "other_sender") != false {
		t.Errorf("inbound message (of %d) = %v", len(msgs), last)
	}
	th := field(d, "thread")
	if field(th, "unread") != true || field(th, "last_direction") != "inbound" || field(th, "message_count") != float64(4) {
		t.Errorf("thread after inbound = %v", th)
	}

	if s := c.ok("PATCH", base, map[string]string{"status": "archived"}, 200, threadSummary, false); field(s, "status") != "archived" {
		t.Errorf("patch = %v", s)
	}
	c.fail("PATCH", base, map[string]string{"status": "deleted"}, 422, "validation")
	if a := list(c.ok("GET", "/api/admin/threads?status=archived", nil, 200, arr(threadSummary), false)); len(a) != 1 {
		t.Errorf("archived = %v", a)
	}
	c.empty("DELETE", base, nil, 204)
	c.fail("GET", base, nil, 404, "not_found")
}

// The form allows 5 submissions per hour per client IP.
func TestContractContactRateLimit(t *testing.T) {
	c := newHarness(t)
	c.ip = "198.51.100.23"
	for i := range 5 {
		c.ok("POST", "/api/v1/contact", contactForm("Visitor", fmt.Sprintf("v%d@example.com", i),
			"Just saying hello, this is long enough."), 201, okShape, false)
	}
	c.fail("POST", "/api/v1/contact", contactForm("Visitor", "v6@example.com", "One message too many today."),
		429, "rate_limited")
	c.ip = "198.51.100.24"
	c.ok("POST", "/api/v1/contact", contactForm("Other", "o@example.com", "A different visitor is fine."), 201, okShape, false)
	c.wait()
	if n := c.scalar(`SELECT count(*) FROM threads`); n != int64(6) {
		t.Errorf("threads = %v, want 6", n)
	}
}

func TestContractAnalyticsAndAdminMisc(t *testing.T) {
	c := newHarness(t)

	for i, ev := range []string{
		`{"type":"pageview","path":"/","referrer":"https://www.google.com/search?q=bao","width":1440}`,
		`{"type":"pageview","path":"/blog","referrer":"https://baoopn.com/","width":390}`,
		`{"type":"pageview","path":"/projects","referrer":"https://www.baoopn.com/blog","width":800}`,
		`{"type":"pageview","path":"/contact","referrer":"https://news.ycombinator.com/item?id=1"}`,
		`{"type":"engagement","path":"/","visible_ms":12000}`,
	} {
		c.ip = fmt.Sprintf("203.0.113.%d", 10+i)
		// sendBeacon posts text/plain.
		if w := c.raw("POST", "/api/v1/events", strings.NewReader(ev), map[string]string{"Content-Type": "text/plain;charset=UTF-8"}); w.Code != 202 || w.Body.Len() != 0 {
			t.Errorf("beacon %d = %d %q", i, w.Code, w.Body)
		}
	}
	c.failRaw(c.raw("POST", "/api/v1/events", strings.NewReader(`{"type":"click","path":"/"}`), nil), "bad beacon", 400, "bad_request")
	c.failRaw(c.raw("POST", "/api/v1/events", strings.NewReader(`{"type":"pageview","path":"/","referrer":"`+strings.Repeat("a", 3000)+`"}`), nil),
		"big beacon", 413, "too_large")
	c.rec.Flush(t.Context())

	rep := c.ok("GET", "/api/admin/analytics?range=24h&tz=Asia/Ho_Chi_Minh", nil, 200, analyticsReport, false)
	if field(field(rep, "totals"), "views") != float64(4) {
		t.Errorf("totals = %v", field(rep, "totals"))
	}
	var refs []string
	for _, r := range list(field(rep, "referrers")) {
		refs = append(refs, fmt.Sprint(field(r, "key")))
	}
	for _, r := range refs {
		if strings.Contains(r, "baoopn.com") {
			t.Errorf("same-host referrer kept: %v", refs)
		}
	}
	if !strings.Contains(strings.Join(refs, ","), "google.com") {
		t.Errorf("external referrer missing: %v", refs)
	}
	for _, rg := range []string{"7d", "30d", "90d"} {
		c.ok("GET", "/api/admin/analytics?range="+rg+"&tz=UTC", nil, 200, analyticsReport, false)
	}
	c.fail("GET", "/api/admin/analytics?range=1y", nil, 400, "bad_range")

	c.ok("GET", "/api/admin/overview", nil, 200, overviewShape, false)
	c.ok("GET", "/api/admin/me", nil, 200, adminUser, false)
	if cfg := c.ok("GET", "/api/admin/config", nil, 200, adminConfigShape, false); field(cfg, "inbound_enabled") != true {
		t.Errorf("config = %v", cfg)
	}
	s := c.ok("GET", "/api/admin/settings", nil, 200, siteSettings, false)
	if m, ok := s.(map[string]any); ok {
		m["signature"] = "— Bao"
		c.ok("PUT", "/api/admin/settings", m, 200, siteSettings, false)
	}
	if r := c.ok("POST", "/api/admin/settings/test-email", nil, 200, sentToShape, false); field(r, "sent_to") != ctNotify {
		t.Errorf("test-email = %v", r)
	}

	if np := c.ok("GET", "/api/v1/listening/now-playing", nil, 200, null(nowPlaying), false); np == nil {
		t.Error("now-playing is null with a playing upstream")
	}
	c.ok("GET", "/api/v1/listening/recent?limit=3", nil, 200, arr(recentPlay), false)
	c.ok("GET", "/api/v1/listening/top-artists?range=7d&limit=5", nil, 200, arr(topArtist), false)
	c.fail("GET", "/api/v1/listening/recent?limit=21", nil, 400, "bad_request")

	c.fail("GET", "/api/v1/nope", nil, 404, "not_found")
}

// Empty states must still be arrays (never null) in every list field.
func TestContractEmptyStates(t *testing.T) {
	c := newHarness(t)
	c.ok("GET", "/api/admin/threads", nil, 200, arr(threadSummary), false)
	c.ok("GET", "/api/admin/threads?status=spam", nil, 200, arr(threadSummary), false)
	c.ok("GET", "/api/admin/media", nil, 200, arr(mediaShape), true)
	c.ok("GET", "/api/admin/projects", nil, 200, arr(adminProject), false)
	c.ok("GET", "/api/v1/projects", nil, 200, arr(projectSummary), false)
	c.ok("GET", "/api/v1/posts?tag=nothing-here", nil, 200, arr(postSummary), true)
	c.ok("GET", "/api/v1/posts?page=99", nil, 200, arr(postSummary), true)
	c.ok("GET", "/api/admin/posts?q=nothing-here", nil, 200, arr(adminPost), false)
	c.ok("GET", "/api/admin/analytics?range=24h", nil, 200, analyticsReport, false)
	c.ok("GET", "/api/admin/overview", nil, 200, overviewShape, false)
	c.ok("POST", "/api/admin/posts/preview", map[string]string{"markdown": ""}, 200, previewResult, false)
	c.ok("POST", "/api/admin/posts/preview", map[string]string{"markdown": "no headings"}, 200, previewResult, false)

	plain := map[string]any{"title": "No Headings", "excerpt": "", "content_md": "Just text.", "cover_url": "",
		"tags": nil, "series": "", "series_title": "", "series_order": 0, "status": "published", "published_at": nil}
	p := c.ok("POST", "/api/admin/posts", plain, 201, adminPost, false)
	c.ok("GET", "/api/v1/posts/"+fmt.Sprint(field(p, "slug")), nil, 200, postDetail, false)

	manual := c.ok("POST", "/api/admin/projects", map[string]any{"title": "Bare"}, 201, adminProject, false)
	c.ok("PATCH", "/api/admin/projects/"+idOf(manual), map[string]any{"visible": true}, 200, adminProject, false)
	c.ok("GET", "/api/v1/projects/bare", nil, 200, projectDetail, false)

	c.fail("GET", "/api/admin/threads/999999", nil, 404, "not_found")
	c.fail("GET", "/api/admin/threads/abc", nil, 404, "not_found")
	c.fail("GET", "/api/admin/posts/999999", nil, 404, "not_found")
	c.fail("POST", "/api/admin/threads/999999/messages", map[string]string{"body": "x", "request_id": "req-nothread"}, 404, "not_found")
}
