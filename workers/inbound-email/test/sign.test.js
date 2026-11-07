import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sign, concat, hex } from '../src/sign.js';

// The shared vector, pinned here and in internal/contact/verify_test.go.
const VECTOR = {
	secret: 'test-secret',
	ts: '1700000000',
	body: 'hello',
	to: 'reply+abc@reply.example.com',
	sig: '16e360ecccd3d3712a57caff006a9e5306bd55e25b11570e07a6d2a2ca87a313',
};

test('signs the shared vector', async () => {
	const body = new TextEncoder().encode(VECTOR.body);
	assert.equal(await sign(VECTOR.secret, VECTOR.ts, VECTOR.to, body), VECTOR.sig);
	assert.equal(await sign(VECTOR.secret, Number(VECTOR.ts), VECTOR.to, body.buffer), VECTOR.sig);
});

test('a different secret, timestamp, recipient or body changes the signature', async () => {
	const body = new TextEncoder().encode(VECTOR.body);
	assert.notEqual(await sign(VECTOR.secret, VECTOR.ts, 'reply+other@reply.example.com', body), VECTOR.sig);
	assert.notEqual(await sign('other', VECTOR.ts, VECTOR.to, body), VECTOR.sig);
	assert.notEqual(await sign(VECTOR.secret, '1700000001', VECTOR.to, body), VECTOR.sig);
	assert.notEqual(await sign(VECTOR.secret, VECTOR.ts, VECTOR.to, new TextEncoder().encode('hellO')), VECTOR.sig);
});

test('signs raw bytes that are not UTF-8', async () => {
	const sig = await sign('k', '1', 'a@b.c', new Uint8Array([0xff, 0x00, 0xe9]));
	assert.match(sig, /^[0-9a-f]{64}$/);
});

test('helpers', () => {
	assert.deepEqual(concat(new Uint8Array([1]), new Uint8Array([2, 3])), new Uint8Array([1, 2, 3]));
	assert.equal(hex(new Uint8Array([0, 15, 255])), '000fff');
});
