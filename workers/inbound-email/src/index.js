// Cloudflare Email Worker: receives replies sent to reply+<token>@<INBOUND_DOMAIN>
// (Email Routing catch-all on that subdomain) and forwards the raw message to the
// API, signed with INBOUND_WEBHOOK_SECRET. Nothing of a message is logged.

import { sign, nowSeconds } from './sign.js';
import { BACKOFF_MS, classify, rejectReason } from './rules.js';

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Posts raw to the API, re-signing on every attempt. Returns "done", "reject"
 * or "drop"; throws when the API stayed unreachable, so Cloudflare reports a
 * temporary failure and the sender's server tries again later.
 */
export async function forward(raw, { to, from }, env, { fetchImpl = fetch, wait = sleep } = {}) {
	let lastError = 'no attempt';
	for (let attempt = 0; attempt <= BACKOFF_MS.length; attempt++) {
		if (attempt > 0) await wait(BACKOFF_MS[attempt - 1]);
		const ts = String(nowSeconds());
		try {
			const res = await fetchImpl(env.API_URL, {
				method: 'POST',
				headers: {
					'Content-Type': 'message/rfc822',
					'X-Envelope-To': to,
					'X-Envelope-From': from ?? '',
					'X-Timestamp': ts,
					'X-Signature': await sign(env.INBOUND_WEBHOOK_SECRET, ts, to, raw),
				},
				body: raw,
			});
			const action = classify(res.status);
			if (action !== 'retry') {
				if (action === 'drop') console.warn(`inbound-email: API answered ${res.status}, dropped`);
				return action;
			}
			if (res.status === 404) {
				// The API has no secret or domain set: a misconfiguration on our side.
				console.error('inbound-email: API answered 404 (hook not configured), will retry');
			}
			lastError = `status ${res.status}`;
		} catch {
			lastError = 'network error';
		}
	}
	throw new Error(`inbound-email: API unavailable (${lastError})`);
}

export default {
	async email(message, env) {
		const reason = rejectReason(message.to, message.rawSize, env.INBOUND_DOMAIN);
		if (reason) {
			message.setReject(reason);
			return;
		}
		const raw = new Uint8Array(await new Response(message.raw).arrayBuffer());
		const result = await forward(raw, { to: message.to, from: message.from }, env);
		if (result === 'reject') message.setReject('Message too large');
	},
};
