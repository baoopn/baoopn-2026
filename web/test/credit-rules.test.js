import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
	MESSAGES,
	ORIGINS_MAX,
	STYLES,
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
	['  https://example.com  ', 'https://example.com'],
	['https://a.com:0443', 'https://a.com'],
	['https://a.com:08443', 'https://a.com:8443'],
	['https://a.com:65535', 'https://a.com:65535'],
	['https://xn--bcher-kva.de', 'https://xn--bcher-kva.de'],
	['https://[2001:DB8::1]:8443', 'https://[2001:db8::1]:8443']
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
	'https://a.com:0',
	'https://a.com:99999',
	'https://a_b.com',
	'https://a..com',
	'https://a.com.',
	'https://a!b.com',
	'https://[fe80::1%25eth0]',
	'https://a.com:abc',
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

const variant = (over = {}) => ({ prefix: 'Designed & built by', suffix: '', style: 'color', ...over });
const valid = () => ({
	url: 'https://baoopn.com',
	logo: 'wordmark',
	variants: { wordmark: variant(), mark: variant() },
	origins: /** @type {string[]} */ ([])
});
/** valid() with one variant changed. */
const withVariant = (/** @type {'wordmark' | 'mark'} */ kind, over = {}) => {
	const f = valid();
	f.variants[kind] = variant(over);
	return f;
};

test('validateForm passes a valid form and trims the values', () => {
	const f = valid();
	f.url = ' https://baoopn.com/ ';
	f.variants.mark = { prefix: '  by  ', suffix: ' baoopn ', style: 'mono' };
	const r = validateForm(f);
	assert.deepEqual(r.errors, {});
	assert.deepEqual(r.input, {
		url: 'https://baoopn.com/',
		logo: 'wordmark',
		variants: {
			wordmark: { prefix: 'Designed & built by', suffix: '', style: 'color' },
			mark: { prefix: 'by', suffix: 'baoopn', style: 'mono' }
		},
		origins: []
	});
});

test('validateForm allows an empty prefix and suffix', () => {
	const r = validateForm(withVariant('wordmark', { prefix: '', suffix: '   ' }));
	assert.deepEqual(r.errors, {});
	assert.deepEqual(r.input.variants.wordmark, { prefix: '', suffix: '', style: 'color' });
});

test('validateForm enforces the prefix and suffix limits per variant', () => {
	// Line and paragraph separators, bidi controls, zero-width space, BOM, newline.
	const bad = ['\n', ...[0x2028, 0x2029, 0x202a, 0x202e, 0x2066, 0x2069, 0x200b, 0xfeff].map((c) => String.fromCodePoint(c))];
	for (const kind of /** @type {const} */ (['wordmark', 'mark'])) {
		for (const field of /** @type {const} */ (['prefix', 'suffix'])) {
			const key = `variants.${kind}.${field}`;
			for (const ch of bad) {
				const r = validateForm(withVariant(kind, { [field]: `Built ${ch}by` }));
				assert.deepEqual(r.errors, { [key]: MESSAGES[field] }, `${key} ${JSON.stringify(ch)}`);
			}
			const long = validateForm(withVariant(kind, { [field]: 'x'.repeat(TEXT_MAX + 1) }));
			assert.deepEqual(long.errors, { [key]: MESSAGES[field] }, key);
			assert.deepEqual(validateForm(withVariant(kind, { [field]: 'x'.repeat(TEXT_MAX) })).errors, {});
			// Runes, not UTF-16 units: 80 emoji fit.
			assert.deepEqual(validateForm(withVariant(kind, { [field]: '🎨'.repeat(TEXT_MAX) })).errors, {});
		}
	}
});

test('validateForm accepts only the two styles, and reads an empty style as color', () => {
	assert.deepEqual(STYLES, ['color', 'mono']);
	assert.deepEqual(validateForm(withVariant('mark', { style: 'mono' })).errors, {});
	const empty = validateForm(withVariant('mark', { style: '' }));
	assert.deepEqual(empty.errors, {});
	assert.equal(empty.input.variants.mark.style, 'color');
	for (const style of ['big', 'Mono', 'COLOR']) {
		assert.deepEqual(
			validateForm(withVariant('mark', { style })).errors,
			{ 'variants.mark.style': MESSAGES.style },
			style
		);
	}
});

test('validateForm lists errors in the order the API checks them', () => {
	const f = valid();
	f.url = 'http://x';
	f.variants.mark = { prefix: 'a\nb', suffix: 'c\nd', style: 'big' };
	f.variants.wordmark = { prefix: '', suffix: 'x'.repeat(TEXT_MAX + 1), style: 'color' };
	assert.deepEqual(Object.keys(validateForm(f).errors), [
		'url',
		'variants.wordmark.suffix',
		'variants.mark.prefix',
		'variants.mark.suffix',
		'variants.mark.style'
	]);
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
	assert.equal(fieldFromMessage('url must be an absolute https URL'), 'url');
	assert.equal(fieldFromMessage('logo must be wordmark or mark'), 'logo');
	assert.equal(fieldFromMessage('origins can hold at most 50 entries'), 'origins');
	assert.equal(
		fieldFromMessage(
			'origins[3] "http://a.com" must be an origin like https://example.com (http only for localhost), with no path'
		),
		'origins[3]'
	);
	for (const kind of ['wordmark', 'mark']) {
		assert.equal(
			fieldFromMessage(`variants.${kind}.prefix must be at most 80 characters on one line`),
			`variants.${kind}.prefix`
		);
		assert.equal(
			fieldFromMessage(`variants.${kind}.suffix must be at most 80 characters on one line`),
			`variants.${kind}.suffix`
		);
		assert.equal(fieldFromMessage(`variants.${kind}.style must be color or mono`), `variants.${kind}.style`);
	}
	assert.equal(fieldFromMessage('variants must have wordmark and mark'), 'variants.wordmark.prefix');
	assert.equal(fieldFromMessage('text must be at most 80 characters on one line'), 'variants.wordmark.prefix');
	assert.equal(fieldFromMessage('variants.big.prefix must be short'), null);
	assert.equal(fieldFromMessage('something else went wrong'), null);
	assert.equal(fieldFromMessage('urls are fine'), null);
	assert.equal(fieldFromMessage('texts are fine'), null);
});

test('snippet embeds the endpoint and builds nodes without innerHTML', () => {
	const endpoint = 'https://baoopn.com/api/v1/credit';
	const s = snippet(endpoint);
	assert.ok(s.includes(JSON.stringify(endpoint)));
	assert.ok(
		s.startsWith(
			'<p id="baoopn-credit" class="baoopn-credit">Designed &amp; built by <a href="https://baoopn.com">baoopn</a></p>'
		)
	);
	assert.ok(s.includes('<script type="module">'));
	assert.ok(s.includes("document.getElementById('baoopn-credit')"));
	assert.ok(s.includes('(prefers-color-scheme: dark)'));
	assert.ok(s.includes("rel = 'noopener'"));
	assert.ok(!/innerHTML|outerHTML|insertAdjacentHTML|document\.write/.test(s));
});

test('snippet lays out prefix, then a link holding the logo and the suffix', () => {
	const s = snippet('https://baoopn.com/api/v1/credit?logo=mark');
	assert.ok(s.includes('fetch("https://baoopn.com/api/v1/credit?logo=mark")'));
	assert.ok(s.includes("if (data.suffix) link.append(document.createTextNode(' ' + data.suffix));"));
	assert.ok(s.includes("if (data.prefix) line.append(document.createTextNode(data.prefix + ' '));"));
	assert.ok(s.indexOf('link.append(picture)') < s.indexOf('data.suffix'));
	assert.ok(!s.includes('data.text'));
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
