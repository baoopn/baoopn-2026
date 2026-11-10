import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
	MESSAGES,
	ORIGINS_MAX,
	TEXT_MAX,
	fieldFromMessage,
	normalizeOrigin,
	snippet,
	validateForm
} from '../src/lib/admin/credit/rules.js';

// Same table as internal/credit/validate_test.go.
const ACCEPT = [
	['HTTPS://TLPortraitz.com/', 'https://tlportraitz.com'],
	['https://a.com:443', 'https://a.com'],
	['https://a.com:8443', 'https://a.com:8443'],
	['http://localhost:5173', 'http://localhost:5173'],
	['http://127.0.0.1:8080', 'http://127.0.0.1:8080'],
	['http://[::1]:3000', 'http://[::1]:3000'],
	['http://localhost:80', 'http://localhost'],
	['  https://example.com  ', 'https://example.com']
];

const REJECT = [
	'http://a.com',
	'https://a.com/x',
	'https://a.com?q',
	'https://a.com#top',
	'https://*.a.com',
	'https://u@a.com',
	'ftp://a.com',
	'https://bücher.de',
	'null',
	'',
	'   ',
	'//a.com',
	'a.com',
	'https://',
	'https://a.com:',
	'https://' + 'a'.repeat(200) + '.com'
];

test('normalizeOrigin accepts and normalises valid origins', () => {
	for (const [raw, want] of ACCEPT) {
		assert.deepEqual(normalizeOrigin(raw), { origin: want }, raw);
	}
});

test('normalizeOrigin rejects anything that is not a bare origin', () => {
	for (const raw of REJECT) {
		const r = normalizeOrigin(raw);
		assert.ok('error' in r, `expected an error for ${JSON.stringify(raw)}`);
		assert.equal(/** @type {{ error: string }} */ (r).error, MESSAGES.origin);
	}
});

const valid = () => ({
	text: 'Designed & built by',
	url: 'https://baoopn.com',
	logo: 'wordmark',
	origins: /** @type {string[]} */ ([])
});

test('validateForm passes a valid form and trims the values', () => {
	const r = validateForm({ ...valid(), text: '  Built by  ', url: ' https://baoopn.com/ ' });
	assert.deepEqual(r.errors, {});
	assert.deepEqual(r.input, {
		text: 'Built by',
		url: 'https://baoopn.com/',
		logo: 'wordmark',
		origins: []
	});
});

test('validateForm enforces the text limits', () => {
	assert.equal(validateForm({ ...valid(), text: '' }).errors.text, MESSAGES.text);
	assert.equal(validateForm({ ...valid(), text: 'two\nlines' }).errors.text, MESSAGES.text);
	assert.equal(validateForm({ ...valid(), text: 'x'.repeat(TEXT_MAX + 1) }).errors.text, MESSAGES.text);
	assert.equal(validateForm({ ...valid(), text: 'x'.repeat(TEXT_MAX) }).errors.text, undefined);
	// Runes, not UTF-16 units: 80 emoji fit.
	assert.equal(validateForm({ ...valid(), text: '🎨'.repeat(TEXT_MAX) }).errors.text, undefined);
});

test('validateForm accepts only absolute https URLs without userinfo', () => {
	for (const url of ['http://baoopn.com', '//baoopn.com', 'javascript:alert(1)', 'https://u:p@baoopn.com', 'https://' + 'a'.repeat(500)]) {
		assert.equal(validateForm({ ...valid(), url }).errors.url, MESSAGES.url, url);
	}
	assert.equal(validateForm({ ...valid(), url: 'https://baoopn.com/work?x=1' }).errors.url, undefined);
});

test('validateForm accepts only the two logo kinds', () => {
	assert.equal(validateForm({ ...valid(), logo: 'mark' }).errors.logo, undefined);
	assert.equal(validateForm({ ...valid(), logo: 'monogram' }).errors.logo, MESSAGES.logo);
});

test('validateForm flags bad origin rows by index and dedupes the rest', () => {
	const r = validateForm({
		...valid(),
		origins: ['https://A.com', 'http://a.com', 'https://a.com/', 'https://b.com']
	});
	assert.deepEqual(r.errors, { 'origins[1]': MESSAGES.origin });
	assert.deepEqual(r.input.origins, ['https://a.com', 'https://b.com']);
});

test('validateForm caps the origin list after dedupe', () => {
	const many = Array.from({ length: ORIGINS_MAX + 1 }, (_, i) => `https://s${i}.example.com`);
	assert.equal(validateForm({ ...valid(), origins: many }).errors.origins, MESSAGES.origins);
	const dupes = [...many.slice(0, ORIGINS_MAX), many[0]];
	assert.equal(validateForm({ ...valid(), origins: dupes }).errors.origins, undefined);
});

test('fieldFromMessage maps each API 422 message to its field', () => {
	assert.equal(fieldFromMessage('text must be 1 to 80 characters on one line'), 'text');
	assert.equal(fieldFromMessage('url must be an absolute https URL'), 'url');
	assert.equal(fieldFromMessage('logo must be wordmark or mark'), 'logo');
	assert.equal(fieldFromMessage('origins can hold at most 50 entries'), 'origins');
	assert.equal(
		fieldFromMessage(
			'origins[3] "http://a.com" must be an origin like https://example.com (http only for localhost), with no path'
		),
		'origins[3]'
	);
	assert.equal(fieldFromMessage('something else went wrong'), null);
	assert.equal(fieldFromMessage('urls are fine'), null);
});

test('snippet embeds the endpoint and builds nodes without innerHTML', () => {
	const endpoint = 'https://baoopn.com/api/v1/credit';
	const s = snippet(endpoint);
	assert.ok(s.includes(JSON.stringify(endpoint)));
	assert.ok(
		s.startsWith(
			'<p class="baoopn-credit">Designed &amp; built by <a href="https://baoopn.com">baoopn</a></p>'
		)
	);
	assert.ok(s.includes('<script type="module">'));
	assert.ok(s.includes('(prefers-color-scheme: dark)'));
	assert.ok(s.includes("rel = 'noopener'"));
	assert.ok(!/innerHTML|outerHTML|insertAdjacentHTML|document\.write/.test(s));
});

test('snippet cannot be broken out of with a hostile endpoint', () => {
	const s = snippet('https://x.com/</script><img src=x>');
	assert.equal(s.match(/<\/script>/g)?.length, 1);
});

test('docs/credit.md shows the same snippet as the admin page', () => {
	const doc = readFileSync(new URL('../../docs/credit.md', import.meta.url), 'utf8');
	assert.ok(doc.includes(snippet('https://baoopn.com/api/v1/credit').trimEnd()));
});

test('the snippet script is a valid module', async () => {
	const s = snippet('https://baoopn.com/api/v1/credit');
	const body = s.slice(s.indexOf('<script type="module">') + 22, s.lastIndexOf('</script>'));
	// Compile only: an async function accepts the same top-level await.
	const AsyncFunction = Object.getPrototypeOf(async () => {}).constructor;
	assert.doesNotThrow(() => new AsyncFunction(body));
});
