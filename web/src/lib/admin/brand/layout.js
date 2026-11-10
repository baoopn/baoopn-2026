// Pure helpers for the admin brand page. Plain JS + JSDoc so node --test can
// import them without a build step (see web/test/brand-admin.test.js).

/** @typedef {import('#lib/api/types.js').BrandName} BrandName */
/** @typedef {import('#lib/api/types.js').BrandGroup} BrandGroup */
/** @typedef {import('#lib/api/types.js').AdminBrandAsset} AdminBrandAsset */

/** @type {readonly BrandName[]} */
export const BRAND_NAMES = ['baoopn', 'listening'];

/**
 * Copy for each brand: the intro under the heading and the toast after a change.
 * @type {Readonly<Record<BrandName, { intro: string; toast: string }>>}
 */
export const BRAND_COPY = {
	baoopn: {
		intro:
			'Favicons and logos. Changes apply right away, separate from the form above. The public site picks up changes within a minute.',
		toast: 'The public site picks up changes within a minute.'
	},
	listening: {
		intro: 'Favicons, logos, icons and the share image for listening.baoopn.com.',
		toast: 'Saved to the listening brand.'
	}
};

/**
 * The brand named by a `?brand=` value. Anything unknown means baoopn.
 * @param {string | null | undefined} value
 * @returns {BrandName}
 */
export function parseBrand(value) {
	return value === 'listening' ? 'listening' : 'baoopn';
}

/**
 * The URL the page shows. A shallow goto (and Back or Forward to an entry one
 * wrote) sets page.shallow.url and leaves page.url alone, so read that first.
 * @param {{ url: URL | { href: string; searchParams: { get(k: string): string | null } }; shallow?: { url: { href: string; searchParams: { get(k: string): string | null } } } | null }} page
 */
function shownUrl(page) {
	return page.shallow?.url ?? page.url;
}

/**
 * The brand the admin page shows, from `?brand=` on the shown URL.
 * @param {Parameters<typeof shownUrl>[0]} page
 * @returns {BrandName}
 */
export function currentBrand(page) {
	return parseBrand(shownUrl(page).searchParams.get('brand'));
}

/**
 * The shown URL with `?brand=` set for `next` (dropped for baoopn, the default).
 * @param {Parameters<typeof shownUrl>[0]} page
 * @param {BrandName} next
 * @returns {URL}
 */
export function brandUrl(page, next) {
	const url = new URL(shownUrl(page).href);
	if (next === 'listening') url.searchParams.set('brand', next);
	else url.searchParams.delete('brand');
	return url;
}

/**
 * Admin API path (relative to /api/admin) for a brand's slot list.
 * @param {BrandName} brand
 * @returns {string}
 */
export function apiBase(brand) {
	return brand === 'listening' ? '/brand/listening' : '/brand';
}

/**
 * Admin API path for one slot: PUT replaces it, DELETE resets it.
 * @param {BrandName} brand
 * @param {string} slot
 * @returns {string}
 */
export function slotPath(brand, slot) {
	return apiBase(brand) + '/' + slot;
}

/**
 * Display groups, in order.
 * @type {readonly { key: BrandGroup; label: string }[]}
 */
export const GROUPS = [
	{ key: 'favicons', label: 'Favicons and app icons' },
	{ key: 'logos', label: 'Logos' },
	{ key: 'icons', label: 'UI icons' },
	{ key: 'social', label: 'Social' },
	{ key: 'email', label: 'Email' }
];

/**
 * Splits assets into GROUPS order, keeping the API order inside each group
 * and dropping empty groups. Assets with a missing or unknown group land in a
 * trailing "Other" group, so nothing disappears if the API adds one.
 * @template {{ group?: string }} A
 * @param {readonly A[]} assets
 * @returns {{ key: string; label: string; assets: A[] }[]}
 */
export function groupAssets(assets) {
	/** @type {Map<string, A[]>} */
	const by = new Map();
	for (const a of assets) {
		const key = GROUPS.some((g) => g.key === a.group) ? /** @type {string} */ (a.group) : 'other';
		const list = by.get(key);
		if (list) list.push(a);
		else by.set(key, [a]);
	}
	/** @type {{ key: string; label: string }[]} */
	const order = [...GROUPS, { key: 'other', label: 'Other' }];
	return order.flatMap((g) => {
		const list = by.get(g.key);
		return list ? [{ key: g.key, label: g.label, assets: list }] : [];
	});
}

/** PNG favicons and app icons, by slot, in CSS px. */
/** @type {Readonly<Record<string, number[]>>} */
const SIZES = {
	favicon_ico: [16, 32, 48],
	favicon_svg: [32, 64],
	favicon_16: [16],
	favicon_32: [32],
	favicon_48: [48],
	apple_touch_icon: [90],
	icon_192: [96],
	icon_512: [128],
	icon_maskable_512: [128]
};

/**
 * Preview sizes in CSS px for a slot, or null for a logo or image that is
 * sized by its shape instead. Other icon_* slots are the SVG UI icons.
 * @param {string} slot
 * @returns {number[] | null}
 */
export function previewSizes(slot) {
	const fixed = SIZES[slot];
	if (fixed) return [...fixed];
	if (slot.startsWith('icon_')) return [24, 48];
	return null;
}

/**
 * Wide assets keep their aspect ratio in the preview; the rest fill a square.
 * @param {{ slot: string; width: number; height: number }} asset
 * @returns {boolean}
 */
export function isWide(asset) {
	if (asset.slot === 'og_image') return true;
	return asset.width > 0 && asset.height > 0 && asset.width / asset.height >= 1.3;
}

/**
 * Maskable icons also preview clipped to a circle, as launchers show them.
 * @param {string} slot
 * @returns {boolean}
 */
export function isMaskable(slot) {
	return slot === 'icon_maskable_512';
}
