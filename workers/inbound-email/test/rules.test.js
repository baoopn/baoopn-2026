import { test } from 'node:test';
import assert from 'node:assert/strict';
import { classify, rejectReason, MAX_BYTES, BACKOFF_MS } from '../src/rules.js';
import { forward } from '../src/index.js';

const DOMAIN = 'reply.baoopn.com';
const TOKEN = '0123456789abcdef0123456789abcdef';

test('accepts only reply addresses on the inbound domain', () => {
	assert.equal(rejectReason(`reply+${TOKEN}@${DOMAIN}`, 100, DOMAIN), null);
	assert.equal(rejectReason(`REPLY+${TOKEN.toUpperCase()}@Reply.Baoopn.com`, 100, DOMAIN), null);
	for (const to of [
		`reply+${TOKEN}@evil.example`,
		`reply+${TOKEN}@x.${DOMAIN}`,
		`reply+0123@${DOMAIN}`,
		`reply+${'g'.repeat(32)}@${DOMAIN}`,
		`bao@${DOMAIN}`,
		`reply+${TOKEN}`,
		'',
		undefined,
	]) {
		assert.equal(rejectReason(to, 100, DOMAIN), 'Unknown address', String(to));
	}
	assert.equal(rejectReason(`reply+${TOKEN}@${DOMAIN}`, 100, ''), 'Unknown address');
});

test('refuses messages over 10 MiB', () => {
	assert.equal(rejectReason(`reply+${TOKEN}@${DOMAIN}`, MAX_BYTES, DOMAIN), null);
	assert.equal(rejectReason(`reply+${TOKEN}@${DOMAIN}`, MAX_BYTES + 1, DOMAIN), 'Message too large');
});

test('classifies API answers', () => {
	assert.equal(classify(204), 'done');
	assert.equal(classify(200), 'done');
	assert.equal(classify(413), 'reject');
	assert.equal(classify(401), 'drop');
	assert.equal(classify(404), 'drop');
	assert.equal(classify(503), 'retry');
	assert.equal(classify(500), 'retry');
});

const env = { API_URL: 'https://api.test/hook', INBOUND_WEBHOOK_SECRET: 'test-secret' };
const raw = new TextEncoder().encode('hello');

test('forward signs, retries 5xx and network errors, then succeeds', async () => {
	const calls = [];
	const waits = [];
	const answers = [() => { throw new Error('down'); }, () => ({ status: 503 }), () => ({ status: 204 })];
	const fetchImpl = async (url, init) => {
		calls.push({ url, init });
		return answers[calls.length - 1]();
	};
	const result = await forward(raw, { to: `reply+${TOKEN}@${DOMAIN}`, from: 'p@example.com' }, env, {
		fetchImpl,
		wait: async (ms) => waits.push(ms),
	});
	assert.equal(result, 'done');
	assert.equal(calls.length, 3);
	assert.deepEqual(waits, BACKOFF_MS);
	const h = calls[2].init.headers;
	assert.equal(calls[2].url, env.API_URL);
	assert.equal(h['Content-Type'], 'message/rfc822');
	assert.equal(h['X-Envelope-To'], `reply+${TOKEN}@${DOMAIN}`);
	assert.match(h['X-Timestamp'], /^\d+$/);
	assert.match(h['X-Signature'], /^[0-9a-f]{64}$/);
});

test('forward stops on 4xx and throws after the last failed attempt', async () => {
	const quiet = console.warn;
	console.warn = () => {};
	try {
		let n = 0;
		const r = await forward(raw, { to: 'x' }, env, { fetchImpl: async () => (n++, { status: 401 }), wait: async () => {} });
		assert.equal(r, 'drop');
		assert.equal(n, 1);
	} finally {
		console.warn = quiet;
	}
	assert.equal(await forward(raw, { to: 'x' }, env, { fetchImpl: async () => ({ status: 413 }), wait: async () => {} }), 'reject');
	let n = 0;
	await assert.rejects(
		forward(raw, { to: 'x' }, env, { fetchImpl: async () => (n++, { status: 502 }), wait: async () => {} }),
		/API unavailable/
	);
	assert.equal(n, 3);
});
