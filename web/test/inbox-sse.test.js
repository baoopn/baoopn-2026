import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createSSEParser, MAX_SSE_EVENT } from '../src/lib/admin/inbox/sse.js';

/** Feed chunks and collect [type, data] pairs. */
function parse(...chunks) {
	const out = [];
	const p = createSSEParser((type, data) => out.push([type, data]));
	for (const c of chunks) p.push(c);
	return { out, p };
}

const frames =
	'retry: 5000\n\nevent: ready\ndata: {}\n\nevent: thread.created\ndata: {"thread_id":7}\n\n';
const expected = [
	['ready', '{}'],
	['thread.created', '{"thread_id":7}']
];

test('LF line endings', () => {
	assert.deepEqual(parse(frames).out, expected);
});

test('CRLF line endings', () => {
	assert.deepEqual(parse(frames.replaceAll('\n', '\r\n')).out, expected);
});

test('lone CR line endings', () => {
	assert.deepEqual(parse(frames.replaceAll('\n', '\r')).out, expected);
});

test('CRLF split between chunks counts as one line ending', () => {
	const s = 'event: a\r\ndata: 1\r\n\r\nevent: b\r\ndata: 2\r\n\r\n';
	const out = [];
	const p = createSSEParser((t, d) => out.push([t, d]));
	for (const part of s.split(/(?<=\r)/)) p.push(part); // every chunk ends right after a CR
	assert.deepEqual(out, [
		['a', '1'],
		['b', '2']
	]);
});

test('chunks split mid-line and mid-field, one character at a time', () => {
	const { out } = parse(...frames.split(''));
	assert.deepEqual(out, expected);
});

test('comments (keep-alive) are ignored and never dispatch', () => {
	const { out } = parse(': keep-alive\n\n', ':\n\n', 'event: ready\n: in the middle\ndata: {}\n\n');
	assert.deepEqual(out, [['ready', '{}']]);
});

test('multi-line data joins with LF; space after colon is optional', () => {
	const { out } = parse('data: a\ndata:b\ndata:  c\n\n');
	assert.deepEqual(out, [['message', 'a\nb\n c']]);
});

test('an event without data is not dispatched; event type resets per event', () => {
	const { out } = parse('event: x\n\ndata: 1\n\n');
	assert.deepEqual(out, [['message', '1']]);
});

test('id, retry and unknown fields are ignored', () => {
	const { out } = parse('id: 4\nretry: 10\nfoo: bar\nevent: e\ndata: d\n\n');
	assert.deepEqual(out, [['e', 'd']]);
});

test('a leading BOM is stripped', () => {
	const { out } = parse('﻿event: ready\ndata: {}\n\n');
	assert.deepEqual(out, [['ready', '{}']]);
});

test('an unterminated event at end() is dropped', () => {
	const { out, p } = parse('event: a\ndata: 1\n');
	p.end();
	p.push('\n');
	assert.deepEqual(out, []);
});

test('an event over the size cap is dropped, the stream continues', () => {
	const big = 'x'.repeat(MAX_SSE_EVENT / 2);
	const { out } = parse(`data: ${big}\n`, `data: ${big}\n`, `data: ${big}\n\n`, 'data: ok\n\n');
	assert.deepEqual(out, [['message', 'ok']]);
});

test('a single line over the cap is discarded up to its line ending', () => {
	const huge = 'y'.repeat(MAX_SSE_EVENT + 10);
	const { out } = parse(
		'data: ',
		huge.slice(0, 600_000),
		huge.slice(600_000),
		'\n\n',
		'data: ok\n\n'
	);
	assert.deepEqual(out, [['message', 'ok']]);
});
