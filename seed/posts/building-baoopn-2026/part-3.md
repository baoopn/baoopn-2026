I wanted each contact message to become a thread, not a dead end in my mailbox. I answer it from the dashboard, the visitor gets an email, and when they reply, their reply lands back in the same thread. This part follows one message through that loop.

![The public contact page with the message form](screenshot:public-contact)

## The form

The form posts to `POST /api/v1/contact`. Before anything is stored, the API runs a few cheap checks against bots:

- **A hidden field.** The form has a `website` input that people never see. If it has a value, a bot filled it. The API answers `201 { ok: true }` and stores nothing, so the bot learns nothing.
- **A fill time.** The page sends the time the form was opened. Under three seconds is too fast for a person. Older than 24 hours means the page is stale.
- **A rate limit** of five posts per hour per IP.
- **Turnstile**, Cloudflare's captcha, when I set its keys. If Turnstile is down, the API returns 503 and asks the visitor to try again. It does not let the message through unchecked.

Then the API creates a thread with a random reply token: 16 random bytes, as 32 hex characters. It answers the browser at once and sends mail in the background.

## Mail out

Outbound mail goes through useSend, which I host myself, over SMTP. Two kinds of mail leave on a new message.

The first is a note to me with the message and a link to the thread. It carries `Auto-Submitted: auto-generated`, so other systems know a machine sent it.

The second is an acknowledgement to the visitor. This one needs care. Anyone can type any address into a form. If the site mailed whatever address it was given, with text the sender chose, it would be a free spam relay. So the acknowledgement has strict limits:

```go
// Anyone can type any address into the form, so this is a potential relay:
// it is only sent when Turnstile is configured (checked by the caller), never
// quotes the visitor's message, only uses a link-free name, and is capped
// site-wide by ackPerHour.
func (m *Module) acknowledge(ctx context.Context, id int64, token string, s submission, template, signature string) {
	if !m.allowAck(time.Now()) {
		m.d.Log.Warn("contact: acknowledgement cap reached, skipped", slog.Int64("thread_id", id))
		return
	}
	text := strings.ReplaceAll(template, "{name}", safeName(s.name))
	// ...
}
```

The cap is 30 per hour across the whole site. `safeName` replaces the name with "there" if it looks like it hides a link or an address: an `@`, a slash, `http`, `www.`, more than one dot, or over 40 characters. The text is a fixed template I set in the dashboard.

Every email I send on a thread has `Reply-To: reply+<token>@reply.example.com`. Replies also set `In-Reply-To` and `References` from the earlier messages, so mail apps group them as one conversation.

## Mail in

Replies go to a subdomain that Cloudflare Email Routing handles. With subaddressing turned on, one route for `reply@` matches every `reply+<token>@` address and hands the message to a small Email Worker.

The worker does three things. It rejects any address that is not `reply+` followed by 32 hex characters, and any message over 10 MiB. It signs the raw message. And it POSTs it to the API's webhook.

### Signing what matters

The signature is an HMAC: a hash keyed with a secret that the worker and the API share. It covers a timestamp, the envelope recipient, and the body:

```go
// Sign is hex(HMAC-SHA256(secret, ts + "." + envTo + "\n" + body)), as the
// Email Worker computes it. Covering the envelope recipient stops a captured
// request from being replayed to a different thread token.
func Sign(secret, ts, envTo string, body []byte) string {
	return hex.EncodeToString(mac(secret, ts, envTo, body))
}
```

The envelope recipient matters because the API picks the thread from it. If the signature covered the body alone, someone holding one signed request could change `X-Envelope-To` and push that message into another thread. With the recipient inside the HMAC, that edit breaks the signature. The API also rejects timestamps more than five minutes away from its clock, and it compares signatures in constant time.

### Retries without duplicates

The worker tries three times, with waits of 500 ms and 2 s. It reads the API's status to decide what to do:

| API answer | Worker action |
|---|---|
| 2xx | done |
| 413 | reject the message |
| other 4xx | log and drop; retrying will not help |
| 5xx or network error | try again |

If all three tries fail, the worker throws. Cloudflare then reports a temporary failure to the sending server, which tries again later. So the same email can reach the API more than once. The API stores inbound mail with `ON CONFLICT (rfc_message_id) DO NOTHING`, so a second copy is a no-op. A message with no `Message-ID` gets one made from a SHA-256 of its raw bytes.

### What gets dropped

Before storing a reply, the API checks the reasons not to. It drops the mail and records why:

- no token, or a token that matches no thread
- mail it cannot parse
- **automatic mail**: auto-replies, list mail, bounces and delivery reports
- mail from my own sending address
- a thread I marked as spam
- more than 50 emails on one thread in 24 hours

The automatic check reads the headers that mail systems set for this: `Auto-Submitted` (anything but `no`), `Precedence: bulk/list/junk`, `List-Id`, Exchange's `X-Auto-Response-Suppress`, a `multipart/report` body, and senders like `mailer-daemon`. This matters for more than noise. If my site sent an acknowledgement to an out-of-office bot, and the bot answered, two machines could mail each other forever.

Each drop is logged with its reason and the token hidden, and the log keeps 90 days.

## Taking out the quoted history

Most replies carry the whole earlier thread under them. I want the thread view to show what the visitor wrote, not my own words again. But losing a visitor's words is worse than showing a quote. So the quote stripper is conservative, and it says so in its header comment:

> A line is removed only when it is recognisably the mail app's and not theirs; when in doubt it stays.

For quoted lines that start with `>`, it looks for an attribution line like "On <date>, <name> <address> wrote:" in English, French, Spanish or German. A date is not enough. The line must name an email address or my sender name, and the next line with text must start with `>`. Without that, the `>` lines stay, because the visitor may have typed them.

Outlook does not mark quotes. It puts a block of `From:`, `Sent:`, `To:`, `Subject:` lines above the old message. Here the rule is stricter still. Everything below the block is removed only when every line in it matches something I sent on this thread. One line that is not mine, such as an answer typed between my paragraphs, and the whole block stays.

The stripper can still be wrong. So whenever it removes anything, the API stores the full email next to the cleaned one, and the dashboard can show both.

## Answering

My reply from the dashboard goes to `POST /api/admin/threads/{id}/messages` with the text and a `request_id` the browser makes. If the same request arrives twice, from a double click or a retry on a bad connection, the insert hits a unique index and the API returns the first result:

```go
msg, err := scanMessage(tx.QueryRow(ctx, `
	INSERT INTO thread_messages (thread_id, direction, source, body, status, request_id, from_address)
	VALUES ($1, 'outbound', 'admin', $2, 'sending', $3, $4)
	ON CONFLICT (thread_id, request_id) WHERE request_id IS NOT NULL DO NOTHING
	RETURNING `+messageCols, id, body, in.RequestID, m.d.Mailer.FromAddress()))
if isNoRows(err) {
	// The same request again: answer with what it made the first time.
	// ...
}
```

The message is saved as `sending` before the SMTP call, then marked `sent` or `failed`. A failed message gets a retry button, and the API refuses to resend one that did not fail.

When a visitor replies, I get a note by email, at most once every ten minutes per thread, so a fast back-and-forth does not flood my inbox.

Part 4 moves on to analytics, and how the site counts visits without cookies.
