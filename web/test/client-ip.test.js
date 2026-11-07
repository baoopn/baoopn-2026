import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	CLOUDFLARE_RANGES,
	inCloudflare,
	parseIP,
	resolveClient
} from '../src/lib/server/client-ip.js';

/** First and last address of an IPv4 CIDR. */
function v4Edges(cidr) {
	const [addr, bits] = cidr.split('/');
	const n = addr.split('.').reduce((a, p) => a * 256 + Number(p), 0);
	const size = 2 ** (32 - Number(bits));
	const fmt = (x) => [24, 16, 8, 0].map((s) => Math.floor(x / 2 ** s) % 256).join('.');
	return { first: fmt(n), last: fmt(n + size - 1), before: fmt(n - 1), after: fmt(n + size) };
}

test('every IPv4 range: first and last are in, neighbours are out', () => {
	const v4 = CLOUDFLARE_RANGES.filter((r) => !r.includes(':'));
	assert.equal(v4.length, 15);
	for (const cidr of v4) {
		const e = v4Edges(cidr);
		assert.ok(inCloudflare(e.first), `${e.first} in ${cidr}`);
		assert.ok(inCloudflare(e.last), `${e.last} in ${cidr}`);
		// 104.16.0.0/13 and 104.24.0.0/14 are adjacent, so skip neighbours that land in another range.
		const covered = (ip) => v4.some((c) => c !== cidr && inRange(ip, c));
		if (!covered(e.before)) assert.ok(!inCloudflare(e.before), `${e.before} outside ${cidr}`);
		if (!covered(e.after)) assert.ok(!inCloudflare(e.after), `${e.after} outside ${cidr}`);
	}
});

function inRange(ip, cidr) {
	const e = v4Edges(cidr);
	const num = (s) => s.split('.').reduce((a, p) => a * 256 + Number(p), 0);
	return num(ip) >= num(e.first) && num(ip) <= num(e.last);
}

test('IPv6 ranges: first and last address in, neighbours out', () => {
	const cases = [
		['2400:cb00::', '2400:cb00:ffff:ffff:ffff:ffff:ffff:ffff', '2400:caff:ffff:ffff:ffff:ffff:ffff:ffff', '2400:cb01::'],
		['2606:4700::', '2606:4700:ffff:ffff:ffff:ffff:ffff:ffff', '2606:46ff:ffff:ffff:ffff:ffff:ffff:ffff', '2606:4701::'],
		['2803:f800::', '2803:f800:ffff:ffff:ffff:ffff:ffff:ffff', '2803:f7ff:ffff:ffff:ffff:ffff:ffff:ffff', '2803:f801::'],
		['2405:b500::', '2405:b500:ffff:ffff:ffff:ffff:ffff:ffff', '2405:b4ff:ffff:ffff:ffff:ffff:ffff:ffff', '2405:b501::'],
		['2405:8100::', '2405:8100:ffff:ffff:ffff:ffff:ffff:ffff', '2405:80ff:ffff:ffff:ffff:ffff:ffff:ffff', '2405:8101::'],
		['2a06:98c0::', '2a06:98c7:ffff:ffff:ffff:ffff:ffff:ffff', '2a06:98bf:ffff:ffff:ffff:ffff:ffff:ffff', '2a06:98c8::'],
		['2c0f:f248::', '2c0f:f248:ffff:ffff:ffff:ffff:ffff:ffff', '2c0f:f247:ffff:ffff:ffff:ffff:ffff:ffff', '2c0f:f249::']
	];
	for (const [first, last, before, after] of cases) {
		assert.ok(inCloudflare(first), first);
		assert.ok(inCloudflare(last), last);
		assert.ok(!inCloudflare(before), before);
		assert.ok(!inCloudflare(after), after);
	}
	assert.ok(inCloudflare('2606:4700:4700::1111'));
	assert.ok(inCloudflare('2606:4700:4700:0:0:0:0:1111'));
});

test('parseIP canonicalises IPv4, IPv6 and IPv4-mapped IPv6', () => {
	assert.deepEqual(parseIP('1.2.3.4'), { family: 4, value: 0x01020304n, address: '1.2.3.4' });
	assert.equal(parseIP('::ffff:1.2.3.4')?.address, '1.2.3.4');
	assert.equal(parseIP('::ffff:1.2.3.4')?.family, 4);
	assert.equal(parseIP('::FFFF:0102:0304')?.address, '1.2.3.4');
	assert.equal(parseIP('2001:0DB8:0000:0000:0000:0000:0000:0001')?.address, '2001:db8::1');
	assert.equal(parseIP('2001:db8:0:0:1:0:0:1')?.address, '2001:db8::1:0:0:1');
	assert.equal(parseIP('::')?.address, '::');
	assert.equal(parseIP('::1')?.address, '::1');
	assert.equal(parseIP('1:2:3:4:5:6:7:8')?.address, '1:2:3:4:5:6:7:8');
	assert.equal(parseIP('64:ff9b::1.2.3.4')?.family, 6);
});

test('IPv4-mapped CF edge counts as in range', () => {
	assert.ok(inCloudflare('::ffff:104.16.0.1'));
	assert.ok(inCloudflare('::ffff:173.245.63.255'));
	assert.ok(!inCloudflare('::ffff:8.8.8.8'));
});

test('parseIP rejects garbage', () => {
	for (const bad of [
		'1.2.3',
		'1.2.3.4.5',
		'1.2.3.256',
		'01.2.3.4',
		'::gg',
		'',
		'1.2.3.4, 5.6.7.8',
		' 1.2.3.4',
		'1.2.3.4 ',
		'1.2.3.4:80',
		'[::1]',
		'fe80::1%eth0',
		'1::2::3',
		':1::',
		'1:2:3:4:5:6:7:8:9',
		'1:2:3:4:5:6:7:8::',
		'12345::',
		'1.2.3.4::',
		'::ffff:1.2.3',
		'unknown',
		null,
		undefined,
		42
	]) {
		assert.equal(parseIP(bad), null, String(bad));
		assert.equal(inCloudflare(/** @type {any} */ (bad)), false, String(bad));
	}
});

const CF_PEER = '172.70.1.2';
const OTHER_PEER = '203.0.113.9';

test('trust off: always the peer, never a country', () => {
	assert.deepEqual(
		resolveClient({ peer: CF_PEER, cfIP: '198.51.100.7', cfCountry: 'DE', trust: false }),
		{ ip: CF_PEER, country: null }
	);
});

test('trust on, non-CF peer, spoofed headers: the peer', () => {
	assert.deepEqual(
		resolveClient({ peer: OTHER_PEER, cfIP: '198.51.100.7', cfCountry: 'DE', trust: true }),
		{ ip: OTHER_PEER, country: null }
	);
});

test('trust on, CF peer, bad cfIP: the peer', () => {
	for (const cfIP of [null, undefined, '', 'nope', '1.2.3.4, 5.6.7.8']) {
		assert.deepEqual(resolveClient({ peer: CF_PEER, cfIP, cfCountry: 'DE', trust: true }), {
			ip: CF_PEER,
			country: null
		});
	}
});

test('trust on, CF peer, valid cfIP: the canonical cfIP', () => {
	assert.deepEqual(
		resolveClient({ peer: CF_PEER, cfIP: '198.51.100.7', cfCountry: 'DE', trust: true }),
		{ ip: '198.51.100.7', country: 'DE' }
	);
	assert.equal(
		resolveClient({ peer: '2606:4700::1', cfIP: '2001:DB8::0:1', trust: true }).ip,
		'2001:db8::1'
	);
	assert.equal(
		resolveClient({ peer: '::ffff:' + CF_PEER, cfIP: '::ffff:198.51.100.7', trust: true }).ip,
		'198.51.100.7'
	);
});

test('country rules', () => {
	const c = (cfCountry) =>
		resolveClient({ peer: CF_PEER, cfIP: '198.51.100.7', cfCountry, trust: true }).country;
	assert.equal(c('US'), 'US');
	assert.equal(c('XX'), 'XX');
	assert.equal(c('T1'), 'T1');
	assert.equal(c('us'), null);
	assert.equal(c('USA'), null);
	assert.equal(c('U'), null);
	assert.equal(c(''), null);
	assert.equal(c(null), null);
	assert.equal(c('D E'), null);
});
