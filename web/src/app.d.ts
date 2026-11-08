// See https://svelte.dev/docs/kit/types#app.d.ts
import type { Brand } from '#lib/api/types.js';

declare global {
	namespace App {
		interface Error {
			code?: string;
			message: string;
		}
		interface PageData {
			/** Brand asset URLs from the root +layout.server.ts; null when the API is down. */
			brand?: Brand | null;
		}
	}
}

export {};
