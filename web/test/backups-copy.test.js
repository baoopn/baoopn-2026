import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	HEALTH,
	HEALTH_FALLBACK,
	TONE_SPOT,
	finishedRun,
	healthCopy,
	runBlockedReason,
	runKindLabel,
	runStatusCopy,
	utcStamp
} from '../src/lib/admin/backups/copy.js';
import {
	fieldFromMessage,
	formFromSettings,
	validateForm
} from '../src/lib/admin/backups/rules.js';

const CODES = [
	'ok',
	'warning',
	'stale',
	'waiting',
	'paused',
	'not_configured',
	'offline',
	'not_reporting'
];
const TONES = Object.keys(TONE_SPOT);

test('every health code has a label, a known tone, a title and an explanation', () => {
	assert.deepEqual(Object.keys(HEALTH).sort(), [...CODES].sort());
	for (const code of CODES) {
		const c = healthCopy(code);
		assert.ok(c.label, code);
		assert.ok(TONES.includes(c.tone), code);
		assert.ok(c.title && !c.title.includes('{'), code);
		assert.ok(c.explanation, code);
	}
});

test('unknown health codes fall back', () => {
	assert.deepEqual(healthCopy('something_new'), HEALTH_FALLBACK);
	assert.deepEqual(healthCopy('toString'), HEALTH_FALLBACK);
});

test('health titles fill in hours, since and the idle reason', () => {
	assert.equal(healthCopy('stale', { hours: 48 }).title, 'No backup in the last 48 hours');
	assert.equal(healthCopy('stale').title, 'No backup in the last 26 hours');
	assert.match(healthCopy('offline', { since: '2026-11-09 03:00 UTC' }).title, /since 2026-11-09/);
	assert.equal(
		healthCopy('not_configured', { idleReason: 'BACKUP_AGE_RECIPIENT is not set' }).explanation,
		'BACKUP_AGE_RECIPIENT is not set'
	);
});

test('copy has no em dashes', () => {
	for (const c of [...Object.values(HEALTH), HEALTH_FALLBACK]) {
		assert.ok(!/—/.test(c.title + c.explanation + c.label), c.label);
	}
});

test('run kinds and statuses have labels with fallbacks', () => {
	for (const k of ['scheduled', 'startup', 'retry', 'manual', 'cli']) {
		assert.notEqual(runKindLabel(k), 'Other', k);
	}
	assert.equal(runKindLabel('nope'), 'Other');
	for (const s of ['queued', 'running', 'ok', 'failed']) {
		const c = runStatusCopy(s);
		assert.notEqual(c.label, 'Unknown', s);
		assert.ok(TONES.includes(c.tone), s);
	}
	assert.deepEqual(runStatusCopy('nope'), { label: 'Unknown', tone: 'muted' });
});

test('back up now is blocked while active and when the service cannot run it', () => {
	assert.equal(runBlockedReason('ok', null), null);
	assert.equal(runBlockedReason('paused', null), null);
	assert.equal(runBlockedReason('stale', null), null);
	assert.match(runBlockedReason('ok', { status: 'queued' }) ?? '', /queued/);
	assert.match(runBlockedReason('ok', { status: 'running' }) ?? '', /running/);
	for (const h of ['offline', 'not_configured', 'not_reporting']) {
		assert.ok(runBlockedReason(h, null), h);
	}
});

test('utcStamp formats UTC and tolerates bad input', () => {
	assert.equal(utcStamp('2026-11-09T03:05:00Z'), '2026-11-09 03:05 UTC');
	assert.equal(utcStamp('2026-11-09T23:30:00-02:00'), '2026-11-10 01:30 UTC');
	assert.equal(utcStamp(null), '');
	assert.equal(utcStamp('nope'), '');
});

test('finishedRun finds the run that just ended', () => {
	const ok = { id: 7, status: 'ok' };
	const failed = { id: 8, status: 'failed' };
	assert.equal(finishedRun(null, { active_run: null, last_run: ok, runs: [ok] }), null);
	assert.equal(
		finishedRun(
			{ id: 7, status: 'running' },
			{ active_run: { id: 7, status: 'running' }, last_run: null, runs: [] }
		),
		null
	);
	assert.equal(
		finishedRun(
			{ id: 7, status: 'running' },
			{ active_run: null, last_run: failed, runs: [failed, ok] }
		),
		ok
	);
	// The queued stand-in (id 0) resolves to the newest finished run.
	assert.equal(
		finishedRun(
			{ id: 0, status: 'queued' },
			{ active_run: null, last_run: failed, runs: [failed] }
		),
		failed
	);
});

test('validateForm mirrors the API rules', () => {
	const empty = { backup_time: '', keep_days: '', keep_min: '', max_age_hours: '' };
	assert.deepEqual(validateForm(empty), {
		input: { backup_time: null, keep_days: null, keep_min: null, max_age_hours: null },
		errors: {}
	});
	const good = validateForm({
		backup_time: '23:59',
		keep_days: '0',
		keep_min: '1000',
		max_age_hours: '25'
	});
	assert.deepEqual(good.errors, {});
	assert.deepEqual(good.input, {
		backup_time: '23:59',
		keep_days: 0,
		keep_min: 1000,
		max_age_hours: 25
	});
	assert.equal(validateForm({ ...empty, max_age_hours: '720' }).input.max_age_hours, 720);

	for (const [k, v] of [
		['backup_time', '24:00'],
		['backup_time', '3:00'],
		['keep_days', '3651'],
		['keep_days', '-1'],
		['keep_days', '1.5'],
		['keep_min', '1001'],
		['max_age_hours', '24'],
		['max_age_hours', '721'],
		['max_age_hours', 'abc']
	]) {
		const r = validateForm({ ...empty, [k]: v });
		assert.ok(r.errors[k], `${k}=${v}`);
		assert.equal(r.input[k], null);
	}
});

test('formFromSettings and fieldFromMessage', () => {
	const set = (override) => ({ override, env: null, value: override, source: 'env' });
	assert.deepEqual(
		formFromSettings({
			backup_time: set('04:30'),
			keep_days: set(14),
			keep_min: set(null),
			max_age_hours: set(0)
		}),
		{ backup_time: '04:30', keep_days: '14', keep_min: '', max_age_hours: '0' }
	);
	assert.equal(fieldFromMessage('keep_min must be between 0 and 1000'), 'keep_min');
	assert.equal(fieldFromMessage('bad'), null);
});
