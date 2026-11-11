import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isClientAbort, stripHopHeaders, upstreamUnavailable } from '../src/lib/server/proxy.js';

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
