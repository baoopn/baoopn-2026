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

export function clearDraft(key: string): void {
	try {
		localStorage.removeItem(PREFIX + key);
	} catch {
		// ignore
	}
}
