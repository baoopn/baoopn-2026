// Pure rule behind guardUnsaved (guard.svelte.ts), importable by node --test.

/**
 * Whether a navigation can skip the unsaved-changes prompt. Only a shallow
 * goto skips: it rewrites the URL (e.g. ?brand= on settings) and leaves the
 * form in place. Back or Forward onto an entry a shallow goto wrote is also
 * marked shallow, but it can leave the page, so it still prompts.
 * @param {{ type: string; shallow: boolean }} nav
 * @param {boolean} dirty
 * @returns {boolean}
 */
export function skipsGuard(nav, dirty) {
	return !dirty || (nav.type === 'goto' && nav.shallow);
}
