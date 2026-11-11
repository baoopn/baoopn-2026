import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	isClientAbort,
	proxyFetch,
	stripHopHeaders,
	upstreamUnavailable
} from '../src/lib/server/proxy.js';

test('stripHopHeaders drops hop-by-hop headers and keeps the rest', () => {
	const h = new Headers({
		connection: 'keep-alive',
		'keep-alive': 'timeout=5',
		expect: '100-continue',
		te: 'trailers',
		trailer: 'x-checksum',
		'proxy-connection': 'keep-alive',
		'proxy-authorization': 'Basic abc',
		upgrade: 'websocket',
		host: 'baoopn.com',
		accept: 'application/json',
		cookie: 'a=b'
	});
	stripHopHeaders(h);
	assert.deepEqual([...h.keys()].sort(), ['accept', 'cookie']);
});

test('stripHopHeaders drops headers named in Connection', () => {
	const h = new Headers({
		connection: 'close, X-Secret-Hop ,x-other',
		'x-secret-hop': '1',
		'x-other': '2',
		'x-kept': '3'
	});
	stripHopHeaders(h);
	assert.deepEqual([...h.keys()], ['x-kept']);
});

test('isClientAbort: aborted signal or AbortError is a client abort', () => {
	const ac = new AbortController();
	ac.abort();
	assert.equal(isClientAbort(new TypeError('fetch failed'), ac.signal), true);
	assert.equal(isClientAbort(new DOMException('aborted', 'AbortError'), undefined), true);
	assert.equal(isClientAbort(new TypeError('fetch failed'), new AbortController().signal), false);
});

test('upstreamUnavailable is a 503 in the API error envelope', async () => {
	const res = upstreamUnavailable();
	assert.equal(res.status, 503);
	assert.match(res.headers.get('content-type') ?? '', /application\/json/);
	const body = await res.json();
	assert.equal(body.error.code, 'upstream_unavailable');
	assert.equal(typeof body.error.message, 'string');
});

const API = 'http://api:8080/api/v1/x';

/** Runs fn with console.error captured; returns the captured lines. */
async function captureErrors(fn) {
	const orig = console.error;
	const lines = [];
	console.error = (...a) => lines.push(a.join(' '));
	try {
		await fn();
	} finally {
		console.error = orig;
	}
	return lines;
}

test('proxyFetch: a failing fetch becomes the 503 envelope and is logged', async () => {
	let res;
	const lines = await captureErrors(async () => {
		res = await proxyFetch(
			async () => {
				throw new TypeError('fetch failed');
			},
			API,
			new Request('http://site/api/v1/x'),
			new Headers()
		);
	});
	assert.equal(res.status, 503);
	assert.equal((await res.json()).error.code, 'upstream_unavailable');
	assert.equal(lines.length, 1);
	assert.match(lines[0], /api proxy failed/);
});

test('proxyFetch: a client abort answers 503 without logging', async () => {
	const ac = new AbortController();
	ac.abort();
	let res;
	const lines = await captureErrors(async () => {
		res = await proxyFetch(
			async () => {
				throw new DOMException('aborted', 'AbortError');
			},
			API,
			new Request('http://site/api/v1/x', { signal: ac.signal }),
			new Headers()
		);
	});
	assert.equal(res.status, 503);
	assert.deepEqual(lines, []);
});

test('proxyFetch: strips hop-by-hop and encoding headers, keeps SSE intact', async () => {
	const upstream = new Response('data: hi\n\n', {
		status: 200,
		headers: {
			'content-type': 'text/event-stream',
			'cache-control': 'no-cache',
			connection: 'keep-alive, x-hop',
			'keep-alive': 'timeout=5',
			'x-hop': '1',
			'content-encoding': 'gzip',
			'content-length': '10'
		}
	});
	let sent;
	const res = await proxyFetch(
		async (url, init) => {
			sent = { url, init };
			return upstream;
		},
		API,
		new Request('http://site/api/v1/x'),
		new Headers({ accept: 'text/event-stream' })
	);
	assert.equal(sent.url, API);
	assert.equal(sent.init.method, 'GET');
	assert.equal(sent.init.body, undefined);
	assert.equal(res.status, 200);
	assert.equal(res.headers.get('content-type'), 'text/event-stream');
	assert.equal(res.headers.get('cache-control'), 'no-cache');
	for (const h of ['connection', 'keep-alive', 'x-hop', 'content-encoding', 'content-length']) {
		assert.equal(res.headers.get(h), null, h);
	}
	assert.equal(await res.text(), 'data: hi\n\n');
});
