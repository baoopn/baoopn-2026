// Formatting helpers for the public site. API timestamps are UTC instants;
// dates are shown through Intl in the viewer's locale and zone.

const DATE_ONLY = /^\d{4}(-\d{2}(-\d{2})?)?$/;

export function toDate(v: string | null | undefined): Date | null {
	if (!v || DATE_ONLY.test(v.trim())) return null;
	const d = new Date(v);
	return Number.isNaN(d.getTime()) ? null : d;
}

const longDate = new Intl.DateTimeFormat('en', { dateStyle: 'long', timeZone: 'UTC' });
const shortDate = new Intl.DateTimeFormat('en', {
	year: 'numeric',
	month: 'short',
	day: '2-digit',
	timeZone: 'UTC'
});
const monthYear = new Intl.DateTimeFormat('en', {
	year: 'numeric',
	month: 'short',
	timeZone: 'UTC'
});

/** "November 6, 2026" (UTC calendar day, so SSR and hydration agree). */
export function dateLong(v: string | null | undefined): string {
	const d = toDate(v);
	return d ? longDate.format(d) : '';
}

/** "Nov 06, 2026". */
export function dateShort(v: string | null | undefined): string {
	const d = toDate(v);
	return d ? shortDate.format(d) : '';
}

/** "2024-03" -> "Mar 2024"; empty -> "Present". */
export function yearMonth(v: string | undefined): string {
	if (!v) return 'Present';
	const m = v.match(/^(\d{4})-(\d{2})/);
	if (!m) return v;
	return monthYear.format(new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, 1)));
}

const RANGE_OPTS: Intl.DateTimeFormatOptions = { year: 'numeric', month: 'short', day: 'numeric' };

/**
 * Half-open period [start, end) as a calendar range in `timeZone`
 * ("Nov 2 – 8, 2026"). The last instant shown is end − 1 ms. An unknown zone
 * falls back to UTC.
 */
export function periodRange(start: string, end: string, timeZone: string): string {
	const s = toDate(start);
	const e = toDate(end);
	if (!s || !e) return '';
	const last = new Date(Math.max(s.getTime(), e.getTime() - 1));
	let fmt: Intl.DateTimeFormat;
	try {
		fmt = new Intl.DateTimeFormat('en', { ...RANGE_OPTS, timeZone });
	} catch {
		fmt = new Intl.DateTimeFormat('en', { ...RANGE_OPTS, timeZone: 'UTC' });
	}
	return fmt.formatRange(s, last);
}

/** "1 track", "12 tracks". */
export function plural(n: number, one: string, many = `${one}s`): string {
	return `${n.toLocaleString('en')} ${n === 1 ? one : many}`;
}

const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto', style: 'short' });

export function relative(v: string | null | undefined, now: number = Date.now()): string {
	const d = toDate(v);
	if (!d) return '';
	const sec = (d.getTime() - now) / 1000;
	const abs = Math.abs(sec);
	if (abs < 60) return 'just now';
	if (abs < 3600) return rtf.format(Math.round(sec / 60), 'minute');
	if (abs < 86_400) return rtf.format(Math.round(sec / 3600), 'hour');
	if (abs < 7 * 86_400) return rtf.format(Math.round(sec / 86_400), 'day');
	if (abs < 30 * 86_400) return rtf.format(Math.round(sec / (7 * 86_400)), 'week');
	if (abs < 365 * 86_400) return rtf.format(Math.round(sec / (30 * 86_400)), 'month');
	return rtf.format(Math.round(sec / (365 * 86_400)), 'year');
}

export function duration(ms: number | null | undefined): string {
	if (ms == null || Number.isNaN(ms) || ms < 0) return '–:––';
	const total = Math.floor(ms / 1000);
	const m = Math.floor(total / 60);
	const s = String(total % 60).padStart(2, '0');
	return `${m}:${s}`;
}

export const pad2 = (n: number) => String(n).padStart(2, '0');

const compact = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 });
export function count(n: number | null | undefined): string {
	return compact.format(n ?? 0);
}

export function artistNames(artists: { name: string }[] | undefined): string {
	return artists?.map((a) => a.name).join(', ') || 'Unknown artist';
}

/** Only http(s) and mailto links from the API become hrefs. */
export function safeUrl(url: string | null | undefined): string | undefined {
	if (!url) return undefined;
	if (url.startsWith('/') && !url.startsWith('//')) return url;
	try {
		const u = new URL(url);
		return ['https:', 'http:', 'mailto:'].includes(u.protocol) ? u.href : undefined;
	} catch {
		return undefined;
	}
}

/** Images: same-origin paths (/media/...) or https URLs only. */
export function safeImage(url: string | null | undefined): string | undefined {
	if (!url) return undefined;
	if (url.startsWith('/') && !url.startsWith('//')) return url;
	try {
		return new URL(url).protocol === 'https:' ? url : undefined;
	} catch {
		return undefined;
	}
}

export function absolute(origin: string, url: string | null | undefined): string | undefined {
	const s = safeImage(url);
	if (!s) return undefined;
	return s.startsWith('/') ? origin + s : s;
}

/** Strip a URL to a readable host + path ("github.com/baoopn"). */
export function prettyUrl(url: string): string {
	return url.replace(/^mailto:/, '').replace(/^https?:\/\/(www\.)?/, '').replace(/\/$/, '');
}

export function escapeXml(s: string): string {
	return s
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;')
		.replace(/'/g, '&apos;');
}

/** JSON for a <script type="application/ld+json"> block, safe against </script>. */
export function jsonLd(value: unknown): string {
	return `<script type="application/ld+json">${JSON.stringify(value).replace(/</g, '\\u003c')}</script>`;
}

/** Stable spot colour for a string (tag, project), used for typographic placeholders. */
const SPOTS = ['peach', 'mint', 'lilac', 'butter', 'sky', 'rose'] as const;
export function spotFor(key: string): string {
	let h = 0;
	for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) | 0;
	return `var(--spot-${SPOTS[Math.abs(h) % SPOTS.length]})`;
}
