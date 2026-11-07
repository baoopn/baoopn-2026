import type { Attachment } from 'svelte/attachments';

/** Minimal typing of the Cloudflare Turnstile browser API we use. */
interface TurnstileAPI {
	render(
		el: HTMLElement,
		opts: {
			sitekey: string;
			theme?: 'auto' | 'light' | 'dark';
			callback?: (token: string) => void;
			'expired-callback'?: () => void;
			'error-callback'?: () => void;
		}
	): string;
	reset(id?: string): void;
	remove(id: string): void;
}

declare global {
	interface Window {
		turnstile?: TurnstileAPI;
	}
}

const SCRIPT = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';
let loading: Promise<TurnstileAPI> | null = null;

function load(): Promise<TurnstileAPI> {
	if (window.turnstile) return Promise.resolve(window.turnstile);
	loading ??= new Promise((resolve, reject) => {
		const s = document.createElement('script');
		s.src = SCRIPT;
		s.async = true;
		s.onload = () => (window.turnstile ? resolve(window.turnstile) : reject(new Error('turnstile missing')));
		s.onerror = () => {
			loading = null;
			reject(new Error('turnstile failed to load'));
		};
		document.head.append(s);
	});
	return loading;
}

/**
 * Attachment that renders a Turnstile widget and reports its token
 * (empty string when it expires or errors).
 */
export function turnstile(sitekey: string, onToken: (token: string) => void): Attachment<HTMLElement> {
	return (el) => {
		let id: string | undefined;
		let alive = true;
		load()
			.then((ts) => {
				if (!alive) return;
				id = ts.render(el, {
					sitekey,
					theme: 'auto',
					callback: onToken,
					'expired-callback': () => onToken(''),
					'error-callback': () => onToken('')
				});
			})
			.catch(() => onToken(''));
		return () => {
			alive = false;
			if (id && window.turnstile) window.turnstile.remove(id);
		};
	};
}

/** Resets a rendered widget (a token is single-use). */
export function resetTurnstile(): void {
	window.turnstile?.reset();
}
