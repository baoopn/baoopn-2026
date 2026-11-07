import { beforeNavigate } from '$app/navigation';

/**
 * Warn before leaving a form with unsaved changes: in-app navigation asks with
 * `confirm()`, closing/reloading the tab gets the browser's own prompt
 * (SvelteKit routes `beforeunload` through `beforeNavigate` as type "leave").
 * Call during component init.
 */
export function guardUnsaved(isDirty: () => boolean, message = 'Discard unsaved changes?'): void {
	beforeNavigate((nav) => {
		if (!isDirty()) return;
		if (nav.type === 'leave') {
			nav.cancel();
			return;
		}
		if (!confirm(message)) nav.cancel();
	});
}
