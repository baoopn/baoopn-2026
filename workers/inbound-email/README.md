# inbound-email (Cloudflare Email Worker)

Visitors answer our mail at `reply+<token>@reply.example.com`. Cloudflare Email
Routing hands each message to this worker, which posts the raw RFC 822 message to
the API's `POST /api/hooks/inbound-email`, signed:

```
X-Timestamp: <unix seconds>
X-Signature: hex(HMAC-SHA256(INBOUND_WEBHOOK_SECRET, timestamp + "." + envelopeTo + "\n" + body))
X-Envelope-To / X-Envelope-From: the SMTP envelope
```

The API checks the signature and a 5-minute timestamp window, finds the thread by
token and stores the reply with the quoted history stripped.

## Setup

1. In Cloudflare, **Email → Email Routing** for `example.com`: add the subdomain
   `reply.example.com` (Cloudflare adds its MX and SPF records).
2. Deploy the worker from this directory: `npx wrangler deploy`.
3. Set its variables in Cloudflare (Workers -> baoopn-inbound-email -> Settings ->
   Variables; `wrangler.toml` has `keep_vars = true`, so deploys never overwrite them):
   `API_URL` = `https://<site>/api/hooks/inbound-email`, `INBOUND_DOMAIN` =
   `reply.example.com` (must equal the API's `INBOUND_MAIL_DOMAIN`), and the secret
   `npx wrangler secret put INBOUND_WEBHOOK_SECRET` (same value as the API's).
4. Email Routing -> Settings: enable **Subaddressing**. Routes: add the custom address
   `reply@reply.example.com` with the action "Send to a Worker" ->
   `baoopn-inbound-email` (subaddressing makes `reply+<token>@...` match it). Don't use
   the zone catch-all, and don't change the root domain's MX records.
5. On the API set `INBOUND_MAIL_DOMAIN=reply.example.com` and
   `INBOUND_WEBHOOK_SECRET`.

## Failure handling

- An address that is not `reply+<32 hex>@INBOUND_DOMAIN`, or a message over
  10 MiB, is refused with `setReject` (the sender gets a bounce).
- API `2xx`: done. `413`: refused as too large. `400` and `422`: logged and
  dropped, because retrying cannot help a malformed request. The API records
  its own rejects with `204`.
- Everything else is retried: `401`/`403` (clock skew, or the secret rotated on
  one side first), `404` (the API has no `INBOUND_WEBHOOK_SECRET` or
  `INBOUND_MAIL_DOMAIN`; each one is logged as an error), `408`, `425`, `429`,
  `5xx` (the API answers `503` when it is busy, over its site-wide limit, or
  could not read the body) and network errors. There are up to 3
  attempts (0.5 s, then 2 s apart), re-signed each time. After the last one the
  worker throws, so Email Routing reports a failure to the sending server, which
  keeps the message and retries later. Mail is lost only if the API stays
  unavailable longer than the sender keeps retrying.
- Retries are safe: the API stores a Message-ID once and notifies once.
- That a throw becomes a temporary failure rather than a bounce is not stated in
  the [Email Workers runtime API](https://developers.cloudflare.com/email-routing/email-workers/runtime-api/)
  docs, which only say that `setReject` is permanent. Check it before relying on
  long API outages.

Nothing of a message (addresses, subject, body) is logged.

## Tests

`node --test test` (from this directory) or `node --test workers/inbound-email/test`
from the repo root. No dependencies; Node 22 provides WebCrypto.
