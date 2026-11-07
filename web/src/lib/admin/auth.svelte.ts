// Clerk-based admin authentication (ported from spotify-tracker).
//
// Boot: GET /api/admin/config (public) -> load Clerk with the publishable key
// -> if a Clerk session exists, GET /api/admin/me to ask the API whether this
// Clerk user is an admin. `@clerk/clerk-js` + `@clerk/ui` are imported lazily
// so nothing is fetched from a CDN and the shell paints first.

import type { Clerk } from '@clerk/clerk-js';
import { goto } from '$app/navigation';
import { ApiRequestError, api } from '#lib/api/client.js';
import type { AdminConfig, AdminUser } from '#lib/api/types.js';
import { errorMessage } from './format';

export type AuthStatus =
	| 'idle'
	| 'loading'
	| 'signed-out'
	| 'checking'
	| 'signed-in'
	/** Clerk session exists but the API rejected its token (401). */
	| 'unauthorized'
	/** Valid Clerk user that is not an admin (403). */
	| 'forbidden'
	| 'error';

/** Navigate within the SPA when same-origin, otherwise do a full page load. */
async function navigateTo(to: string, replaceState = false): Promise<void> {
	const url = new URL(to, window.location.href);
	if (url.origin !== window.location.origin) {
		window.location.href = url.href;
		return;
	}
	await goto(url.pathname + url.search + url.hash, { replaceState });
}

/** Clerk appearance built from the CSS tokens, so it follows palette + mode. */
export function clerkAppearance() {
	return {
		variables: {
			colorPrimary: 'var(--ink)',
			colorPrimaryForeground: 'var(--paper)',
			colorDanger: 'var(--danger)',
			colorSuccess: 'var(--ok)',
			colorWarning: 'var(--warn)',
			colorNeutral: 'var(--ink)',
			colorForeground: 'var(--ink)',
			colorMuted: 'var(--paper-3)',
			colorMutedForeground: 'var(--ink-soft)',
			colorBackground: 'var(--paper-2)',
			colorInput: 'var(--paper)',
			colorInputForeground: 'var(--ink)',
			colorRing: 'var(--ink)',
			colorBorder: 'var(--rule-strong)',
			colorShadow: 'transparent',
			borderRadius: '8px',
			fontFamily: 'inherit',
			fontFamilyButtons: 'inherit'
		},
		elements: {
			// Fluid: the card fills its container (the sign-in page sizes it),
			// so it never overflows a 320 px phone.
			rootBox: { width: '100%' },
			cardBox: {
				width: '100%',
				maxWidth: '100%',
				boxShadow: 'var(--print-shadow)',
				border: '1px solid var(--ink)',
				borderRadius: 'var(--baoopn-radius)'
			}
		}
	};
}

class AuthState {
	status = $state<AuthStatus>('idle');
	user = $state<AdminUser | null>(null);
	error = $state<string | null>(null);
	config = $state<AdminConfig | null>(null);
	clerk = $state.raw<Clerk | null>(null);

	#initPromise: Promise<void> | null = null;
	#clerkUserId: string | null = null;

	init(): Promise<void> {
		this.#initPromise ??= this.#boot();
		return this.#initPromise;
	}

	retry(): Promise<void> {
		this.#initPromise = null;
		this.error = null;
		return this.init();
	}

	async #boot(): Promise<void> {
		this.status = 'loading';
		try {
			const cfg = await api<AdminConfig>('/api/admin/config');
			this.config = cfg;
			if (!cfg.clerk_publishable_key) {
				throw new Error('The API did not return a Clerk publishable key.');
			}
			const [{ Clerk }, { ui }] = await Promise.all([
				import('@clerk/clerk-js'),
				import('@clerk/ui')
			]);
			const clerk = new Clerk(cfg.clerk_publishable_key);
			await clerk.load({
				ui,
				appearance: clerkAppearance(),
				signInUrl: '/admin',
				afterSignOutUrl: '/admin',
				routerPush: (to: string) => navigateTo(to),
				routerReplace: (to: string) => navigateTo(to, true)
			});
			this.clerk = clerk;
			this.#clerkUserId = clerk.user?.id ?? null;
			clerk.addListener(({ user }) => {
				const id = user?.id ?? null;
				if (id !== this.#clerkUserId) {
					this.#clerkUserId = id;
					void this.refresh();
				}
			});
			await this.refresh();
		} catch (err) {
			this.status = 'error';
			this.error = errorMessage(err);
		}
	}

	/** Re-evaluate the API-side admin check for the current Clerk user. */
	async refresh(): Promise<void> {
		const clerk = this.clerk;
		if (!clerk?.user || !clerk.session) {
			this.user = null;
			this.status = 'signed-out';
			return;
		}
		if (this.status !== 'signed-in') this.status = 'checking';
		try {
			this.user = await api<AdminUser>('/api/admin/me', {
				token: await this.getToken()
			});
			this.status = 'signed-in';
			this.error = null;
		} catch (err) {
			this.user = null;
			if (err instanceof ApiRequestError && err.status === 403) {
				this.status = 'forbidden';
			} else if (err instanceof ApiRequestError && err.status === 401) {
				this.status = 'unauthorized';
				this.error = err.message;
			} else {
				this.status = 'error';
				this.error = errorMessage(err);
			}
		}
	}

	/** Called by the API wrapper when an admin request comes back 401/403. */
	rejected(status: 401 | 403): void {
		if (status === 403) this.status = 'forbidden';
		else this.status = this.clerk?.session ? 'unauthorized' : 'signed-out';
	}

	async getToken(): Promise<string | null> {
		const session = this.clerk?.session;
		if (!session) return null;
		try {
			return await session.getToken();
		} catch {
			return null;
		}
	}

	async signOut(): Promise<void> {
		this.user = null;
		if (this.clerk) await this.clerk.signOut({ redirectUrl: '/admin' });
		this.status = 'signed-out';
	}
}

export const auth = new AuthState();
