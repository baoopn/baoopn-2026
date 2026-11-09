import { test } from 'node:test';
import assert from 'node:assert/strict';
import { applyInboxEvent } from '../src/lib/admin/inbox/events.js';

/** A fake QueryClient that records calls, plus recorded hook calls. */
function harness() {
	const calls = [];
	const hooks = [];
	const qc = {
		invalidateQueries: (f) => calls.push(['invalidate', f]),
		removeQueries: (f) => calls.push(['remove', f])
	};
	const h = {
		fresh: (id) => hooks.push(['fresh', id]),
		inbound: (id) => hooks.push(['inbound', id]),
		ownReply: (rid, mid) => hooks.push(['ownReply', rid, mid])
	};
	return { qc, h, calls, hooks };
}

const inv = (queryKey) => ['invalidate', { queryKey }];
const THREADS = ['inbox', 'threads'];
const UNREAD = ['inbox', 'unread'];

test('ready resyncs everything under ["inbox"]', () => {
	const { qc, h, calls, hooks } = harness();
	applyInboxEvent(qc, 'ready', '{}', h);
	assert.deepEqual(calls, [inv(['inbox'])]);
	assert.deepEqual(hooks, []);
});

test('thread.created: lists + unread, highlights the thread', () => {
	const { qc, h, calls, hooks } = harness();
	applyInboxEvent(qc, 'thread.created', '{"thread_id":5}', h);
	assert.deepEqual(calls, [inv(THREADS), inv(UNREAD)]);
	assert.deepEqual(hooks, [['fresh', 5]]);
});

test('message.created inbound: lists, unread, thread; fresh + inbound hooks', () => {
	const { qc, h, calls, hooks } = harness();
	applyInboxEvent(qc, 'message.created', '{"thread_id":5,"message_id":9,"direction":"inbound"}', h);
	assert.deepEqual(calls, [inv(THREADS), inv(UNREAD), inv(['inbox', 'thread', 5])]);
	assert.deepEqual(hooks, [
		['fresh', 5],
		['inbound', 5]
	]);
});

test('message.created outbound with request_id: ownReply hook, no highlight', () => {
	const { qc, h, calls, hooks } = harness();
	applyInboxEvent(
		qc,
		'message.created',
		'{"thread_id":5,"message_id":9,"direction":"outbound","request_id":"r-1"}',
		h
	);
	assert.deepEqual(calls, [inv(THREADS), inv(UNREAD), inv(['inbox', 'thread', 5])]);
	assert.deepEqual(hooks, [['ownReply', 'r-1', 9]]);
});

test('message.updated: only the thread', () => {
	const { qc, h, calls, hooks } = harness();
	applyInboxEvent(qc, 'message.updated', '{"thread_id":5,"message_id":9,"status":"sent"}', h);
	assert.deepEqual(calls, [inv(['inbox', 'thread', 5])]);
	assert.deepEqual(hooks, []);
});

test('thread.updated: lists, unread, thread', () => {
	const { qc, h, calls } = harness();
	applyInboxEvent(qc, 'thread.updated', '{"thread_id":5,"change":"status","status":"spam"}', h);
	assert.deepEqual(calls, [inv(THREADS), inv(UNREAD), inv(['inbox', 'thread', 5])]);
});

test('thread.deleted: drops inactive thread cache, refetches open views, lists, unread', () => {
	const { qc, h, calls } = harness();
	applyInboxEvent(qc, 'thread.deleted', '{"thread_id":5}', h);
	assert.deepEqual(calls, [
		['remove', { queryKey: ['inbox', 'thread', 5], type: 'inactive' }],
		inv(['inbox', 'thread', 5]),
		inv(THREADS),
		inv(UNREAD)
	]);
});

test('unknown events, bad JSON and missing ids are ignored', () => {
	const { qc, h, calls, hooks } = harness();
	applyInboxEvent(qc, 'thread.exploded', '{"thread_id":5}', h);
	applyInboxEvent(qc, 'thread.created', 'not json', h);
	applyInboxEvent(qc, 'thread.created', 'null', h);
	applyInboxEvent(qc, 'thread.created', '{}', h);
	applyInboxEvent(qc, 'thread.created', '{"thread_id":"5"}', h);
	applyInboxEvent(qc, 'message', '{"thread_id":5}', h);
	assert.deepEqual(calls, []);
	assert.deepEqual(hooks, []);
});

test('hooks are optional', () => {
	const { qc, calls } = harness();
	applyInboxEvent(qc, 'message.created', '{"thread_id":1,"message_id":2,"direction":"inbound"}');
	assert.equal(calls.length, 3);
});
