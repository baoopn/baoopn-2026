// Credit form rules, mirroring the API's validation (credit contract §1).
// Plain JS so `node --test` can load it. The API stays the final judge; these
// checks catch mistakes before a round trip.

/** @typedef {'wordmark' | 'mark'} CreditLogoKind */
/** @typedef {{ text: string; url: string; logo: string; origins: string[] }} CreditForm */
/** @typedef {{ text: string; url: string; logo: CreditLogoKind; origins: string[] }} CreditInput */
/** Keys: text, url, logo, origins (the list) and origins[<i>] (one row). */
/** @typedef {Record<string, string>} CreditErrors */

export const TEXT_MAX = 80;
export const URL_MAX = 500;
export const ORIGINS_MAX = 50;
export const ORIGIN_MAX_BYTES = 200;

/** @type {CreditLogoKind[]} */
export const LOGOS = ['wordmark', 'mark'];

export const MESSAGES = {
	text: `Enter 1 to ${TEXT_MAX} characters on one line.`,
	url: 'Enter an absolute https URL, for example https://baoopn.com.',
	logo: 'Pick Wordmark or Mark.',
	origin: 'Use an origin like https://example.com, with no path. Only localhost can use http.',
	origins: `The list can hold at most ${ORIGINS_MAX} origins.`,
	duplicate: 'This origin is already in the list.'
};

/** Hosts that may use plain http (local development). */
const LOCAL_HOSTS = new Set(['localhost', '127.0.0.1', '[::1]']);
/** Go's unicode.IsControl: C0, DEL and C1. */
const CONTROL = /[\u0000-\u001f\u007f-\u009f]/;
/** Text must stay on one line and render in order: no controls, line or
 * paragraph separators (Zl, Zp), or format characters (Cf, which holds the
 * bidi controls). */
const TEXT_BAD = /[\p{Cc}\p{Cf}\p{Zl}\p{Zp}]/u;
const NON_ASCII = /[^\u0000-\u007f]/;
/** scheme://authority with an optional single "/" and nothing after it. */
const ORIGIN_SHAPE = /^([A-Za-z][A-Za-z0-9+.-]*):\/\/([^/?#]*)\/?$/;
/** Host (LDH labels separated by dots, or a bracketed IPv6 literal without a zone) and optional port. */
const HOST_PORT = /^([a-z0-9-]+(?:\.[a-z0-9-]+)*|\[[0-9a-f:.]+\])(?::(\d+))?$/;
/** @type {Record<string, number>} */
const DEFAULT_PORT = { https: 443, http: 80 };

/** @param {string} s */
const byteLength = (s) => new TextEncoder().encode(s).length;

/**
 * Normalise one client origin the way the API stores it:
 * lowercase scheme://host[:port], port as a plain number, default port dropped.
 * @param {string} raw
 * @returns {{ origin: string } | { error: string }}
 */
export function normalizeOrigin(raw) {
	const bad = { error: MESSAGES.origin };
	const s = String(raw ?? '').trim();
	if (!s || byteLength(s) > ORIGIN_MAX_BYTES) return bad;
	// IDN hosts must be punycode; "*" and userinfo are never allowed.
	if (NON_ASCII.test(s) || CONTROL.test(s) || s.includes('*') || s.includes('@')) return bad;
	const m = ORIGIN_SHAPE.exec(s);
	if (!m) return bad;
	const scheme = m[1].toLowerCase();
	const hp = HOST_PORT.exec(m[2].toLowerCase());
	if (!hp) return bad;
	const host = hp[1];
	if (scheme !== 'https' && !(scheme === 'http' && LOCAL_HOSTS.has(host))) return bad;
	let port = '';
	if (hp[2] !== undefined) {
		const n = Number(hp[2]);
		if (!Number.isInteger(n) || n < 1 || n > 65535) return bad;
		if (n !== DEFAULT_PORT[scheme]) port = ':' + n;
	}
	return { origin: `${scheme}://${host}${port}` };
}

/**
 * @param {string} raw
 * @returns {string | null} the error, or null when the URL looks valid
 */
function checkUrl(raw) {
	if (!raw || byteLength(raw) > URL_MAX || CONTROL.test(raw)) return MESSAGES.url;
	if (!/^https:\/\//i.test(raw)) return MESSAGES.url;
	/** @type {URL} */
	let u;
	try {
		u = new URL(raw);
	} catch {
		return MESSAGES.url;
	}
	const authority = raw.slice(raw.indexOf('//') + 2).split(/[/?#]/, 1)[0];
	if (u.protocol !== 'https:' || !u.hostname || authority.includes('@')) return MESSAGES.url;
	return null;
}

/**
 * Check the form and build the PUT body. Origins are normalised and
 * deduplicated (first one wins), as the API does.
 * @param {CreditForm} form
 * @returns {{ input: CreditInput; errors: CreditErrors }}
 */
export function validateForm(form) {
	/** @type {CreditErrors} */
	const errors = {};

	const text = String(form.text ?? '').trim();
	const runes = [...text].length;
	if (runes < 1 || runes > TEXT_MAX || TEXT_BAD.test(text)) errors.text = MESSAGES.text;

	const url = String(form.url ?? '').trim();
	const urlError = checkUrl(url);
	if (urlError) errors.url = urlError;

	const logo = /** @type {CreditLogoKind} */ (form.logo);
	if (!LOGOS.includes(logo)) errors.logo = MESSAGES.logo;

	/** @type {string[]} */
	const origins = [];
	(form.origins ?? []).forEach((raw, i) => {
		const r = normalizeOrigin(raw);
		if ('error' in r) errors[`origins[${i}]`] = r.error;
		else if (!origins.includes(r.origin)) origins.push(r.origin);
	});
	if (origins.length > ORIGINS_MAX) errors.origins = MESSAGES.origins;

	return { input: { text, url, logo, origins }, errors };
}

/**
 * Which field a 422 message from the API is about, or null. The API starts
 * each message with the field name: "text ...", "url ...", "logo ...",
 * "origins[3] ..." (one row) or "origins ..." (the list).
 * @param {string} message
 * @returns {string | null} text, url, logo, origins or origins[<i>]
 */
export function fieldFromMessage(message) {
	const msg = String(message ?? '');
	const row = /^origins\[(\d+)\]/.exec(msg);
	if (row) return `origins[${row[1]}]`;
	for (const f of ['text', 'url', 'logo', 'origins']) {
		if (msg === f || msg.startsWith(f + ' ')) return f;
	}
	return null;
}

/**
 * Embed HTML for a client site footer: a static line that works without
 * JavaScript, then a module script that swaps in the current credit.
 * @param {string} endpoint absolute URL of GET /api/v1/credit
 * @returns {string}
 */
export function snippet(endpoint) {
	// JSON string literal; "<" escaped so the value cannot close the script.
	const url = JSON.stringify(String(endpoint)).replace(/</g, '\\u003c');
	return `<p id="baoopn-credit" class="baoopn-credit">Designed &amp; built by <a href="https://baoopn.com">baoopn</a></p>
<script type="module">
	const fallback = document.getElementById('baoopn-credit');
	try {
		if (!fallback) throw new Error('no credit line');
		const res = await fetch(${url});
		if (!res.ok) throw new Error('credit unavailable');
		const { data } = await res.json();
		if (!/^https:\\/\\//.test(data.url)) throw new Error('bad link');
		const { light, dark } = data.logo;
		const width = (img) => Math.round((20 * img.width) / (img.height || 1));
		const source = document.createElement('source');
		source.media = '(prefers-color-scheme: dark)';
		source.srcset = dark.url;
		source.width = width(dark);
		source.height = 20;
		const img = document.createElement('img');
		img.src = light.url;
		img.alt = data.alt;
		img.width = width(light);
		img.height = 20;
		const picture = document.createElement('picture');
		picture.append(source, img);
		const link = document.createElement('a');
		link.href = data.url;
		link.rel = 'noopener';
		link.append(picture);
		const line = document.createElement('p');
		line.id = 'baoopn-credit';
		line.className = 'baoopn-credit';
		line.append(document.createTextNode(data.text + ' '), link);
		fallback.replaceWith(line);
	} catch {
		// Keep the static line.
	}
</script>
`;
}
