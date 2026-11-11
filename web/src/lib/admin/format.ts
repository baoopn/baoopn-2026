// Formatting helpers for the admin. Large standalone numbers use compact
// notation; tables use full numbers with grouping.

const nf = new Intl.NumberFormat(undefined);
const nfCompact = new Intl.NumberFormat(undefined, {
	notation: 'compact',
	maximumFractionDigits: 1
});
const nf1 = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });
const pct = new Intl.NumberFormat(undefined, {
	style: 'percent',
	maximumFractionDigits: 0,
	signDisplay: 'exceptZero'
});

export function num(n: number | null | undefined): string {
	return n == null || Number.isNaN(n) ? '–' : nf.format(n);
}

export function compact(n: number | null | undefined): string {
	if (n == null || Number.isNaN(n)) return '–';
	return Math.abs(n) < 10_000 ? nf.format(n) : nfCompact.format(n);
}

/** Signed change versus a previous value, e.g. "+12%"; null when undefined. */
export function delta(
	cur: number,
	prev: number
): { text: string; dir: 'up' | 'down' | 'flat' } | null {
	if (!prev) return cur > 0 ? { text: 'new', dir: 'up' } : null;
	const r = (cur - prev) / prev;
	return {
		text: pct.format(r),
		dir: r > 0.005 ? 'up' : r < -0.005 ? 'down' : 'flat'
	};
}

/** Milliseconds as a short span: "45s", "1m 12s". */
export function timeSpan(ms: number | null | undefined): string {
	if (ms == null || Number.isNaN(ms) || ms < 0) return '–';
	const total = Math.round(ms / 1000);
	const h = Math.floor(total / 3600);
	const m = Math.floor((total % 3600) / 60);
	const s = total % 60;
	if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`;
	if (m > 0) return s > 0 ? `${m}m ${s}s` : `${m}m`;
	return `${s}s`;
}

export function bytes(n: number): string {
	if (n < 1024) return `${n} B`;
	if (n < 1024 ** 2) return `${nf1.format(n / 1024)} KB`;
	return `${nf1.format(n / 1024 ** 2)} MB`;
}

function parse(v: string | Date | null | undefined): Date | null {
	if (!v) return null;
	const d = v instanceof Date ? v : new Date(v);
	return Number.isNaN(d.getTime()) ? null : d;
}

const dtf = new Intl.DateTimeFormat(undefined, {
	dateStyle: 'medium',
	timeStyle: 'short'
});
const df = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' });
const tf = new Intl.DateTimeFormat(undefined, { timeStyle: 'short' });

export function dateTime(v: string | Date | null | undefined): string {
	const d = parse(v);
	return d ? dtf.format(d) : '–';
}

export function date(v: string | Date | null | undefined): string {
	const d = parse(v);
	return d ? df.format(d) : '–';
}

/** Chat-style stamp: time today, otherwise date + time. */
export function stamp(v: string | Date | null | undefined, now = Date.now()): string {
	const d = parse(v);
	if (!d) return '–';
	const today = new Date(now);
	return d.toDateString() === today.toDateString() ? tf.format(d) : dtf.format(d);
}

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
	['year', 31_536_000],
	['month', 2_592_000],
	['week', 604_800],
	['day', 86_400],
	['hour', 3_600],
	['minute', 60],
	['second', 1]
];
const rtf = new Intl.RelativeTimeFormat(undefined, {
	numeric: 'auto',
	style: 'short'
});

export function relative(v: string | Date | null | undefined, now = Date.now()): string {
	const d = parse(v);
	if (!d) return '–';
	const diff = (d.getTime() - now) / 1000;
	const abs = Math.abs(diff);
	if (abs < 30) return 'just now';
	for (const [unit, sec] of UNITS) {
		if (abs >= sec) return rtf.format(Math.round(diff / sec), unit);
	}
	return dateTime(d);
}

/** ISO 3166-1 alpha-2 code to a flag emoji (regional indicators); "" if not a code. */
export function flag(code: string): string {
	const c = code.trim().toUpperCase();
	if (!/^[A-Z]{2}$/.test(c)) return '';
	return String.fromCodePoint(...[...c].map((ch) => 0x1f1e6 + ch.charCodeAt(0) - 65));
}

let regionNames: Intl.DisplayNames | null = null;
export function countryName(code: string): string {
	try {
		regionNames ??= new Intl.DisplayNames(undefined, { type: 'region' });
		return regionNames.of(code.toUpperCase()) ?? code;
	} catch {
		return code;
	}
}

/** URL-safe slug from a title. */
export function slugify(s: string): string {
	return s
		.normalize('NFKD')
		.replace(/[̀-ͯ]/g, '')
		.toLowerCase()
		.replace(/đ/g, 'd')
		.replace(/[^a-z0-9]+/g, '-')
		.replace(/^-+|-+$/g, '')
		.slice(0, 80);
}

export function wordCount(md: string): number {
	const text = md
		.replace(/```[\s\S]*?```/g, ' ')
		.replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
		.replace(/[#>*_`~[\]()|-]/g, ' ');
	return text.split(/\s+/).filter(Boolean).length;
}

/** ISO instant -> value for <input type="datetime-local"> in the browser's zone. */
export function toLocalInput(iso: string | null | undefined): string {
	const d = parse(iso);
	if (!d) return '';
	const p = (n: number) => String(n).padStart(2, '0');
	return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`;
}

/** <input type="datetime-local"> value (browser zone) -> ISO instant, or null. */
export function fromLocalInput(v: string): string | null {
	if (!v) return null;
	const d = new Date(v);
	return Number.isNaN(d.getTime()) ? null : d.toISOString();
}

export function errorMessage(err: unknown): string {
	if (err instanceof Error) return err.message || 'Something went wrong';
	return typeof err === 'string' ? err : 'Something went wrong';
}

/** Shown when POST /projects/sync answers 202: the sync goes on in the background. */
export const SYNC_RUNNING_MESSAGE =
	'Sync is running in the background. Refresh in a minute to see the result.';
