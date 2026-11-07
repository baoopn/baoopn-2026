import { test } from "node:test";
import assert from "node:assert/strict";
import {
  CSP_REPORT_MAX_BYTES,
  readCapped,
  setSecurityHeaders,
  summarizeCspReport,
  windowLimiter,
  withSecurityHeaders,
} from "../src/lib/server/security-headers.js";

/** A stream that yields the given chunks, recording whether it was cancelled. */
function streamOf(chunks) {
  const state = { cancelled: false, pulled: 0 };
  const stream = new ReadableStream({
    pull(c) {
      if (state.pulled < chunks.length)
        c.enqueue(new TextEncoder().encode(chunks[state.pulled++]));
      else c.close();
    },
    cancel() {
      state.cancelled = true;
    },
  });
  return { stream, state };
}

test("base headers are set on every response, HSTS only over https", () => {
  const http = new Headers();
  setSecurityHeaders(http, false);
  assert.equal(http.get("x-frame-options"), "DENY");
  assert.equal(http.get("x-content-type-options"), "nosniff");
  assert.equal(http.get("referrer-policy"), "strict-origin-when-cross-origin");
  assert.match(http.get("permissions-policy"), /camera=\(\)/);
  assert.equal(http.get("strict-transport-security"), null);

  const https = new Headers();
  setSecurityHeaders(https, true);
  assert.equal(https.get("strict-transport-security"), "max-age=15552000");
});

test("values already on the response are kept", () => {
  const h = new Headers({
    "X-Frame-Options": "SAMEORIGIN",
    "Strict-Transport-Security": "max-age=1",
  });
  setSecurityHeaders(h, true);
  assert.equal(h.get("x-frame-options"), "SAMEORIGIN");
  assert.equal(h.get("strict-transport-security"), "max-age=1");
});

test("responses with immutable headers are copied, body and status intact", async () => {
  const res = Response.redirect("http://localhost/x", 302); // immutable headers
  assert.throws(() => res.headers.set("a", "b"));
  const out = withSecurityHeaders(res, false);
  assert.notEqual(out, res);
  assert.equal(out.status, 302);
  assert.equal(out.headers.get("location"), "http://localhost/x");
  assert.equal(out.headers.get("x-frame-options"), "DENY");

  const body = withSecurityHeaders(
    new Response("hello", { status: 201 }),
    false,
  );
  assert.equal(body.status, 201);
  assert.equal(await body.text(), "hello");
});

test("readCapped returns the body up to the cap and null past it, cancelling the stream", async () => {
  assert.equal(await readCapped(null, 10), "");
  const ok = streamOf(["a".repeat(CSP_REPORT_MAX_BYTES - 1), "b"]);
  assert.equal(
    (await readCapped(ok.stream, CSP_REPORT_MAX_BYTES)).length,
    CSP_REPORT_MAX_BYTES,
  );

  // A chunked body with no Content-Length: stops reading once over the cap.
  const big = streamOf(Array(100).fill("x".repeat(1024)));
  assert.equal(await readCapped(big.stream, CSP_REPORT_MAX_BYTES), null);
  assert.ok(big.state.cancelled, "stream was not cancelled");
  assert.ok(
    big.state.pulled <= 10,
    `read ${big.state.pulled} KiB of a 100 KiB body`,
  );
});

test("readCapped decodes multi-byte characters split across chunks", async () => {
  const bytes = new TextEncoder().encode("é");
  const s = new ReadableStream({
    start(c) {
      c.enqueue(bytes.slice(0, 1));
      c.enqueue(bytes.slice(1));
      c.close();
    },
  });
  assert.equal(await readCapped(s, 10), "é");
});

test("legacy reports: queries and fragments are stripped, fields clipped", () => {
  const r = summarizeCspReport(
    JSON.stringify({
      "csp-report": {
        "document-uri": "https://baoopn.com/blog/x?token=secret#frag",
        "effective-directive": "img-src",
        "blocked-uri": "https://evil.example/p.png?email=a@b.c",
        "source-file": "https://baoopn.com/_app/x.js#L1",
        "script-sample": "should not be logged",
      },
    }),
  );
  assert.deepEqual(r, {
    "document-uri": "https://baoopn.com/blog/x",
    "violated-directive": "img-src",
    "blocked-uri": "https://evil.example/p.png",
    "source-file": "https://baoopn.com/_app/x.js",
  });
  const long = summarizeCspReport(
    JSON.stringify({
      "csp-report": { "blocked-uri": "https://x/" + "a".repeat(5000) },
    }),
  );
  assert.equal(long["blocked-uri"].length, 200);
});

test("Reporting API reports are read; anything else is null", () => {
  const r = summarizeCspReport(
    JSON.stringify([
      {
        type: "csp-violation",
        body: {
          documentURL: "http://l/?q=1",
          effectiveDirective: "script-src-elem",
          blockedURL: "inline",
        },
      },
    ]),
  );
  assert.equal(r["document-uri"], "http://l/");
  assert.equal(r["blocked-uri"], "inline");
  assert.equal(r["source-file"], null);
  for (const bad of [
    "",
    "not json",
    "null",
    "42",
    "[]",
    "[null]",
    '{"csp-report":"x"}',
    '{"csp-report":{"blocked-uri":{"a":1}}}',
  ]) {
    const out = summarizeCspReport(bad);
    assert.ok(out === null || out["blocked-uri"] === null, `input ${bad}`);
  }
});

test("windowLimiter allows `limit` hits per window, then resets", () => {
  let t = 1_000_000;
  const allow = windowLimiter(60, 60_000, () => t);
  let n = 0;
  for (let i = 0; i < 100; i++) if (allow()) n++;
  assert.equal(n, 60);
  t += 59_999;
  assert.equal(allow(), false);
  t += 1;
  assert.equal(allow(), true);
});
