import { test } from 'node:test';
import assert from 'node:assert/strict';
import { forward } from '../src/index.js';
import { BACKOFF_MS } from '../src/rules.js';

const raw = new TextEncoder().encode('From: a@example.com\r\n\r\nhi');
const env = { API_URL: 'http://api.test/api/hooks/inbound-email', INBOUND_WEBHOOK_SECRET: 's' };
const attempts = BACKOFF_MS.length + 1;

/** Runs forward against an API that always answers status. */
async function always(status) {
	let calls = 0;
	const waits = [];
	const run = forward(raw, { to: 'x' }, env, {
		fetchImpl: async () => (calls++, { status }),
		wait: async (ms) => void waits.push(ms)
	});
	return { run, calls: () => calls, waits };
}

test('every non-permanent failure is retried, then throws so Cloudflare defers', async () => {
	for (const status of [401, 403, 404, 408, 425, 429, 500, 502, 503, 504]) {
		const r = await always(status);
		await assert.rejects(r.run, new RegExp(`API unavailable \\(status ${status}\\)`), String(status));
		assert.equal(r.calls(), attempts, `${status}: attempts`);
		assert.deepEqual(r.waits, BACKOFF_MS, `${status}: backoff`);
	}
});

test('a network error on every attempt throws', async () => {
	let calls = 0;
	await assert.rejects(
		forward(raw, { to: 'x' }, env, {
			fetchImpl: async () => {
				calls++;
				throw new TypeError('fetch failed');
			},
			wait: async () => {}
		}),
		/API unavailable \(network error\)/
	);
	assert.equal(calls, attempts);
});

test('permanent answers stop at once; 413 rejects; 2xx is done', async () => {
	const want = { 400: 'drop', 422: 'drop', 413: 'reject', 200: 'done', 202: 'done', 204: 'done' };
	const quiet = { warn: console.warn, error: console.error };
	console.warn = console.error = () => {};
	try {
		for (const [status, action] of Object.entries(want)) {
			const r = await always(Number(status));
			assert.equal(await r.run, action, status);
			assert.equal(r.calls(), 1, `${status}: attempts`);
		}
	} finally {
		Object.assign(console, quiet);
	}
});

test('404 (hook not configured) is retried 3 times, logged as an error, then throws', async () => {
	const errors = [];
	const { error } = console;
	console.error = (msg) => void errors.push(msg);
	try {
		const r = await always(404);
		await assert.rejects(r.run, /API unavailable \(status 404\)/);
		assert.equal(r.calls(), 3);
		assert.equal(errors.length, 3);
		assert.match(errors[0], /404/);
	} finally {
		console.error = error;
	}
});

test('400 and 422 (malformed) drop after 1 attempt', async () => {
	const { warn } = console;
	console.warn = () => {};
	try {
		for (const status of [400, 422]) {
			const r = await always(status);
			assert.equal(await r.run, 'drop', String(status));
			assert.equal(r.calls(), 1, `${status}: attempts`);
			assert.deepEqual(r.waits, [], `${status}: no wait`);
		}
	} finally {
		console.warn = warn;
	}
});
