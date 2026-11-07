import { defineEnvVars } from '@sveltejs/kit/env';

/** SvelteKit 3 explicit environment variables (import from $app/env/private). */
export const variables = defineEnvVars({
	API_URL: {
		description: 'Internal URL of the Go API (proxied for /api/* and /media/*)',
		schema: (v) => (v ?? 'http://localhost:8080').replace(/\/$/, '')
	},
	INTERNAL_API_TOKEN: {
		description: 'Shared secret letting the API trust forwarded client IP/country',
		schema: (v) => v ?? ''
	},
	PUBLIC_TURNSTILE_SITE_KEY: {
		public: true,
		description: 'Cloudflare Turnstile site key; empty hides the captcha (pair with TURNSTILE_SECRET_KEY)',
		schema: (v) => v ?? ''
	},
	TRUST_CLOUDFLARE: {
		description:
			'Honour CF-Connecting-IP/CF-IPCountry. Only enable when the origin firewall admits Cloudflare IPs only',
		schema: (v) => v === 'true' || v === '1'
	}
});
