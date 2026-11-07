// Local (per-browser) draft autosave. Storage can be unavailable or full, so
// every access is wrapped; losing a local draft is never fatal.

export interface LocalDraft<T> {
	saved_at: string;
	value: T;
}

const PREFIX = 'baoopn:admin:draft:';

export function readDraft<T>(key: string): LocalDraft<T> | null {
	try {
		const raw = localStorage.getItem(PREFIX + key);
		if (!raw) return null;
		const d = JSON.parse(raw) as LocalDraft<T>;
		return d && typeof d === 'object' && 'value' in d ? d : null;
	} catch {
		return null;
	}
}

export function writeDraft<T>(key: string, value: T): void {
	try {
		const d: LocalDraft<T> = { saved_at: new Date().toISOString(), value };
		localStorage.setItem(PREFIX + key, JSON.stringify(d));
	} catch {
		// storage blocked or full: keep editing without a local copy
	}
}

/**
 * Every bounded 24-character lowercase hex run in s (a stored media file's
 * stem): the characters around it are not hex digits. Same rule as the API's
 * mediarefs.Stems.
 */
export function mediaStems(s: string): Set<string> {
	const out = new Set<string>();
	for (const m of s.matchAll(/(?<![0-9a-fA-F])[0-9a-f]{24}(?![0-9a-fA-F])/g)) out.add(m[0]);
	return out;
}

/**
 * Stems named in this browser's local drafts. The server cannot see them, so
 * the orphan cleanup leaves those files out.
 */
export function draftMediaStems(): Set<string> {
	const out = new Set<string>();
	try {
		for (let i = 0; i < localStorage.length; i++) {
			const key = localStorage.key(i);
			if (!key?.startsWith(PREFIX)) continue;
			for (const st of mediaStems(localStorage.getItem(key) ?? '')) out.add(st);
		}
	} catch {
		// storage blocked: nothing known
	}
	return out;
}

export function clearDraft(key: string): void {
	try {
		localStorage.removeItem(PREFIX + key);
	} catch {
		// ignore
	}
}
