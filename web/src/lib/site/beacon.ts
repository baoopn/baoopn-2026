// Privacy-friendly page analytics (ported from spotify-tracker): one beacon per
// page view and, when the page is hidden or left, how long it was visible.
// No cookies, no storage, no IDs.
import type { BeaconEvent } from '#lib/api/types.js';

const ENDPOINT = '/api/v1/events';

function send(event: BeaconEvent): void {
	const body = JSON.stringify(event);
	try {
		if (navigator.sendBeacon?.(ENDPOINT, body)) return;
	} catch {
		// fall through to fetch
	}
	void fetch(ENDPOINT, { method: 'POST', body, keepalive: true }).catch(() => {});
}

// Automated browsers (tests, scrapers) announce themselves; don't count them.
const enabled = () => typeof navigator !== 'undefined' && !navigator.webdriver;

let currentPath: string | null = null;
let visibleSince: number | null = null;

function reportEngagement(): void {
	if (currentPath == null || visibleSince == null) return;
	const ms = Math.round(performance.now() - visibleSince);
	visibleSince = null;
	if (ms > 0) {
		send({ type: 'engagement', path: currentPath, width: window.innerWidth, visible_ms: ms });
	}
}

/** Call on every client navigation (afterNavigate). */
export function trackPageview(path: string, referrer: string): void {
	if (!enabled() || path === currentPath) return;
	reportEngagement();
	currentPath = path;
	visibleSince = document.visibilityState === 'visible' ? performance.now() : null;
	send({ type: 'pageview', path, referrer, width: window.innerWidth });
}

/** Starts engagement listeners; returns a cleanup function. */
export function startEngagement(): () => void {
	if (!enabled()) return () => {};
	const onVisibility = () => {
		if (document.visibilityState === 'hidden') reportEngagement();
		else visibleSince ??= performance.now();
	};
	document.addEventListener('visibilitychange', onVisibility);
	window.addEventListener('pagehide', reportEngagement);
	return () => {
		reportEngagement();
		document.removeEventListener('visibilitychange', onVisibility);
		window.removeEventListener('pagehide', reportEngagement);
	};
}
