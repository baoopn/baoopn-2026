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
- API `2xx`: done. `413`: refused as too large. Other `4xx`: logged and dropped
  (retrying cannot help; the API records its own rejects with `204`).
- `5xx` or a network error: up to 3 attempts (0.5 s, then 2 s apart), re-signed
  each time; after the last one the worker throws, so Cloudflare reports a
  temporary failure and the sending server retries later. Mail is lost only if
  the API stays down longer than the sender keeps retrying.

Nothing of a message (addresses, subject, body) is logged.

## Tests

`node --test test` (from this directory) or `node --test workers/inbound-email/test`
from the repo root. No dependencies; Node 22 provides WebCrypto.
