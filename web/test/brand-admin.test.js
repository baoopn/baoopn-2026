import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { compile } from 'svelte/compiler';
import {
	BRAND_COPY,
	BRAND_NAMES,
	GROUPS,
	apiBase,
	brandUrl,
	currentBrand,
	groupAssets,
	isMaskable,
	isWide,
	parseBrand,
	previewSizes,
	slotPath
} from '../src/lib/admin/brand/layout.js';
import { skipsGuard } from '../src/lib/admin/guard-rules.js';

const ICONS = ['headphones', 'now_playing', 'now_playing_static', 'paused', 'playlist', 'track'];

test('apiBase and slotPath build admin paths for both brands', () => {
	assert.equal(apiBase('baoopn'), '/brand');
	assert.equal(apiBase('listening'), '/brand/listening');
	assert.equal(slotPath('baoopn', 'favicon_ico'), '/brand/favicon_ico');
	assert.equal(slotPath('listening', 'icon_192'), '/brand/listening/icon_192');
});

test('parseBrand falls back to baoopn', () => {
	assert.equal(parseBrand('listening'), 'listening');
	assert.equal(parseBrand('baoopn'), 'baoopn');
	for (const v of [null, undefined, '', 'Listening', 'other']) assert.equal(parseBrand(v), 'baoopn');
	assert.deepEqual(BRAND_NAMES, ['baoopn', 'listening']);
});

test('brand copy has an intro and toast for each brand, with no em dashes', () => {
	for (const b of BRAND_NAMES) {
		assert.ok(BRAND_COPY[b].intro.length > 0);
		assert.ok(BRAND_COPY[b].toast.length > 0);
		assert.ok(!/—/.test(BRAND_COPY[b].intro + BRAND_COPY[b].toast));
	}
	assert.equal(
		BRAND_COPY.listening.intro,
		'Favicons, logos, icons and the share image for listening.baoopn.com.'
	);
	assert.equal(BRAND_COPY.listening.toast, 'Saved to the listening brand.');
	// listening.baoopn.com does not read this API yet, so promise no timing.
	assert.ok(!/picks up changes within a few minutes/.test(JSON.stringify(BRAND_COPY.listening)));
});

test('GROUPS are in display order with plain labels', () => {
	assert.deepEqual(
		GROUPS.map((g) => [g.key, g.label]),
		[
			['favicons', 'Favicons and app icons'],
			['logos', 'Logos'],
			['icons', 'UI icons'],
			['social', 'Social'],
			['email', 'Email']
		]
	);
});

test('groupAssets orders groups, keeps API order inside each and drops empty ones', () => {
	const assets = [
		{ slot: 'email_logo', group: 'email' },
		{ slot: 'wordmark_primary', group: 'logos' },
		{ slot: 'favicon_ico', group: 'favicons' },
		{ slot: 'monogram_primary', group: 'logos' },
		{ slot: 'favicon_svg', group: 'favicons' }
	];
	const out = groupAssets(assets);
	assert.deepEqual(
		out.map((g) => [g.key, g.assets.map((a) => a.slot)]),
		[
			['favicons', ['favicon_ico', 'favicon_svg']],
			['logos', ['wordmark_primary', 'monogram_primary']],
			['email', ['email_logo']]
		]
	);
	assert.equal(out[0].label, 'Favicons and app icons');
	assert.deepEqual(groupAssets([]), []);
});

test('groupAssets puts a missing or unknown group last instead of losing it', () => {
	const out = groupAssets([
		{ slot: 'x', group: 'future' },
		{ slot: 'y' },
		{ slot: 'og_image', group: 'social' }
	]);
	assert.deepEqual(
		out.map((g) => [g.key, g.assets.map((a) => a.slot)]),
		[
			['social', ['og_image']],
			['other', ['x', 'y']]
		]
	);
});

test('previewSizes covers favicons, app icons and UI icons', () => {
	assert.deepEqual(previewSizes('favicon_ico'), [16, 32, 48]);
	assert.deepEqual(previewSizes('favicon_svg'), [32, 64]);
	assert.deepEqual(previewSizes('favicon_16'), [16]);
	assert.deepEqual(previewSizes('favicon_32'), [32]);
	assert.deepEqual(previewSizes('favicon_48'), [48]);
	assert.deepEqual(previewSizes('apple_touch_icon'), [90]);
	assert.deepEqual(previewSizes('icon_192'), [96]);
	assert.deepEqual(previewSizes('icon_512'), [128]);
	assert.deepEqual(previewSizes('icon_maskable_512'), [128]);
	for (const n of ICONS)
		for (const c of ['navy', 'cream']) assert.deepEqual(previewSizes(`icon_${n}_${c}`), [24, 48]);
	for (const s of ['wordmark_primary', 'mark_navy', 'monogram_primary', 'og_image', 'email_logo'])
		assert.equal(previewSizes(s), null);
	// Callers get a copy, not the shared table.
	previewSizes('favicon_ico').push(1);
	assert.deepEqual(previewSizes('favicon_ico'), [16, 32, 48]);
});

test('isWide matches the old baoopn rule and handles listening shapes', () => {
	// baoopn defaults (viewBox / PNG sizes from internal/brand/defaults).
	const baoopn = {
		wordmark_primary: [320, 132, true],
		wordmark_navy: [254, 109, true],
		monogram_primary: [200, 200, false],
		monogram_navy: [160, 78, true],
		email_logo: [640, 264, true],
		favicon_svg: [200, 200, false]
	};
	for (const [slot, [width, height, want]] of Object.entries(baoopn))
		assert.equal(isWide({ slot, width, height }), want, slot);
	assert.equal(isWide({ slot: 'mark_navy', width: 160, height: 151 }), false);
	assert.equal(isWide({ slot: 'wordmark_cream', width: 254, height: 121 }), true);
	assert.equal(isWide({ slot: 'og_image', width: 0, height: 0 }), true);
	assert.equal(isWide({ slot: 'x', width: 13, height: 10 }), true);
	assert.equal(isWide({ slot: 'x', width: 12, height: 10 }), false);
	assert.equal(isWide({ slot: 'x', width: 0, height: 0 }), false);
});

test('isMaskable flags only the maskable icon', () => {
	assert.equal(isMaskable('icon_maskable_512'), true);
	assert.equal(isMaskable('icon_512'), false);
	assert.equal(isMaskable('favicon_ico'), false);
});

for (const name of ['BrandAssets', 'BrandSlot']) {
	test(`${name}.svelte compiles with runes and no warnings`, () => {
		const src = readFileSync(new URL(`../src/lib/admin/brand/${name}.svelte`, import.meta.url), 'utf8');
		for (const generate of ['client', 'server']) {
			const out = compile(src, { filename: `${name}.svelte`, runes: true, generate });
			assert.ok(out.js.code.length > 0);
			assert.deepEqual(
				out.warnings.map((w) => w.code),
				[]
			);
		}
	});
}

const SETTINGS = 'https://baoopn.com/admin/settings';
const at = (href, shallowHref) => ({
	url: new URL(href),
	shallow: shallowHref ? { url: new URL(shallowHref) } : null
});

test('currentBrand reads page.url before any shallow goto', () => {
	assert.equal(currentBrand(at(SETTINGS)), 'baoopn');
	assert.equal(currentBrand(at(SETTINGS + '?brand=listening')), 'listening');
	assert.equal(currentBrand({ url: new URL(SETTINGS + '?brand=listening') }), 'listening');
});

test('currentBrand follows page.shallow.url after a shallow goto or Back', () => {
	// SvelteKit 3 leaves page.url alone on a shallow goto and sets page.shallow.url.
	assert.equal(currentBrand(at(SETTINGS, SETTINGS + '?brand=listening')), 'listening');
	assert.equal(currentBrand(at(SETTINGS + '?brand=listening', SETTINGS)), 'baoopn');
});

test('brandUrl builds from the shown URL and keeps other params', () => {
	const p = at(SETTINGS + '?x=1', SETTINGS + '?x=1&brand=listening');
	assert.equal(brandUrl(p, 'baoopn').href, SETTINGS + '?x=1');
	assert.equal(brandUrl(p, 'listening').href, SETTINGS + '?x=1&brand=listening');
	assert.equal(brandUrl(at(SETTINGS), 'listening').href, SETTINGS + '?brand=listening');
	// A copy: the page URL is never changed in place.
	assert.equal(p.shallow.url.href, SETTINGS + '?x=1&brand=listening');
});

test('skipsGuard skips only a shallow goto or a clean form', () => {
	assert.equal(skipsGuard({ type: 'goto', shallow: true }, true), true);
	assert.equal(skipsGuard({ type: 'goto', shallow: false }, true), false);
	assert.equal(skipsGuard({ type: 'link', shallow: false }, true), false);
	// Back onto an entry a shallow goto wrote is shallow too, and can lose edits.
	assert.equal(skipsGuard({ type: 'popstate', shallow: true }, true), false);
	assert.equal(skipsGuard({ type: 'leave', shallow: false }, true), false);
	assert.equal(skipsGuard({ type: 'popstate', shallow: true }, false), true);
	assert.equal(skipsGuard({ type: 'link', shallow: false }, false), true);
});
