import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	BACKOFF_MAX_MS,
	backoffDelay,
	retryAfterDelay,
	statusAfterGrace
} from '../src/lib/admin/inbox/backoff.js';

const mid = () => 0.5; // no jitter
const low = () => 0; // -50 %
const high = () => 0.999999; // just under +50 %

test('backoff doubles from 1 s and caps at 30 s', () => {
	assert.deepEqual(
		[0, 1, 2, 3, 4, 5, 6, 10].map((n) => backoffDelay(n, mid)),
		[1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000]
	);
});

test('backoff jitter stays within ±50 %', () => {
	assert.equal(backoffDelay(0, low), 500);
	assert.equal(backoffDelay(0, high), 1500);
	assert.equal(backoffDelay(20, high), Math.round(BACKOFF_MAX_MS * 1.499999));
	for (let i = 0; i < 200; i++) {
		const d = backoffDelay(2);
		assert.ok(d >= 2000 && d <= 6000, String(d));
	}
});

test('negative attempts behave like the first retry', () => {
	assert.equal(backoffDelay(-3, mid), 1000);
});

test('Retry-After wins when longer than the backoff, defaults to 10 s', () => {
	assert.equal(retryAfterDelay('20', 1000), 20_000);
	assert.equal(retryAfterDelay('1', 4000), 4000);
	assert.equal(retryAfterDelay(null, 1000), 10_000);
	assert.equal(retryAfterDelay('', 1000), 10_000);
	assert.equal(retryAfterDelay('soon', 1000), 10_000);
	assert.equal(retryAfterDelay('-5', 1000), 10_000);
});

test('after the 2 s grace: not ready means reconnecting, unless offline or paused', () => {
	assert.equal(statusAfterGrace('connecting', false), 'reconnecting');
	assert.equal(statusAfterGrace('live', false), 'reconnecting');
	assert.equal(statusAfterGrace('reconnecting', false), 'reconnecting');
	assert.equal(statusAfterGrace('offline', false), 'offline');
	assert.equal(statusAfterGrace('paused', false), 'paused');
	assert.equal(statusAfterGrace('live', true), 'live');
});
