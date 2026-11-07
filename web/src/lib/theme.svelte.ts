// Palette + light/dark/system mode. The inline script in app.html applies the
// saved values before first paint; this module keeps them in sync afterwards.

export const PALETTES = [
	{ id: 'baoopn', label: 'Baoopn' },
	{ id: 'lavender', label: 'Lavender' },
	{ id: 'mint', label: 'Mint' },
	{ id: 'peach', label: 'Peach' },
	{ id: 'sky', label: 'Sky' },
	{ id: 'rose', label: 'Rose' }
] as const;

export type PaletteId = (typeof PALETTES)[number]['id'];
export type Mode = 'light' | 'dark' | 'system';
export type ResolvedTheme = 'light' | 'dark';

export const MODES: { id: Mode; label: string }[] = [
	{ id: 'light', label: 'Light' },
	{ id: 'dark', label: 'Dark' },
	{ id: 'system', label: 'System' }
];

const PALETTE_KEY = 'baoopn:palette';
const MODE_KEY = 'baoopn:mode';

function read(key: string): string | null {
	try {
		return localStorage.getItem(key);
	} catch {
		return null;
	}
}

function write(key: string, value: string): void {
	try {
		localStorage.setItem(key, value);
	} catch {
		// storage unavailable (private mode, blocked): keep the in-memory value
	}
}

function isPalette(v: string | null): v is PaletteId {
	return PALETTES.some((p) => p.id === v);
}

function isMode(v: string | null): v is Mode {
	return v === 'light' || v === 'dark' || v === 'system';
}

class ThemeState {
	palette = $state<PaletteId>('baoopn');
	mode = $state<Mode>('system');
	systemDark = $state(false);
	resolved: ResolvedTheme = $derived(
		this.mode === 'system' ? (this.systemDark ? 'dark' : 'light') : this.mode
	);

	#started = false;

	/** Read persisted values and follow `prefers-color-scheme` live. Returns a cleanup fn. */
	start(): () => void {
		if (this.#started) return () => {};
		this.#started = true;
		const p = read(PALETTE_KEY);
		const m = read(MODE_KEY);
		if (isPalette(p)) this.palette = p;
		if (isMode(m)) this.mode = m;

		const mq = window.matchMedia('(prefers-color-scheme: dark)');
		this.systemDark = mq.matches;
		const onChange = (e: MediaQueryListEvent) => {
			this.systemDark = e.matches;
		};
		mq.addEventListener('change', onChange);
		return () => {
			mq.removeEventListener('change', onChange);
			this.#started = false;
		};
	}

	/** Push the current state onto <html>. Called from an effect in the root layout. */
	apply(): void {
		const root = document.documentElement;
		root.dataset.palette = this.palette;
		root.dataset.theme = this.resolved;
		root.dataset.mode = this.mode;
	}

	setPalette(id: PaletteId): void {
		this.palette = id;
		write(PALETTE_KEY, id);
	}

	setMode(mode: Mode): void {
		this.mode = mode;
		write(MODE_KEY, mode);
	}
}

export const theme = new ThemeState();
