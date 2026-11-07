import adapter from '@sveltejs/adapter-node';
import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

// Only the three directives from enforced() are enforced. The full policy runs
// as Report-Only and posts violations to /csp-report (logged by hooks.server.ts).
// Once the reports stay clean, move reportOnly into directives (docs/deployment.md).
// 'unsafe-inline' in style-src covers Svelte transitions, style= attributes,
// Clerk and CodeMirror; with it present Kit adds no style nonce, as intended.
const enforced = () => ({
	'frame-ancestors': ['none' as const],
	'base-uri': ['none' as const],
	'object-src': ['none' as const]
});

const CLERK = ['https://clerk.baoopn.com', 'https://*.clerk.accounts.dev'] as const;
// Clerk Protect (bot detection) loads scripts from *.protect.clerk.com and
// probes it on several ports; seen in the CSP reports on /admin.
const CLERK_PROTECT = 'https://*.protect.clerk.com';
const TURNSTILE = 'https://challenges.cloudflare.com';
const MEDIA = 'https://media.baoopn.com';

export default defineConfig({
	plugins: [
		tailwindcss(),
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			adapter: adapter({ out: 'build', precompress: true }),
			csp: {
				// No page is prerendered today, so 'auto' means a fresh nonce per
				// request. A prerendered page would get hashes instead.
				mode: 'auto',
				directives: enforced(),
				reportOnly: {
					'default-src': ['self'],
					'script-src': ['self', 'wasm-unsafe-eval', TURNSTILE, ...CLERK, CLERK_PROTECT],
					'style-src': ['self', 'unsafe-inline'],
					'img-src': [
						'self',
						'data:',
						'blob:',
						MEDIA,
						'https://i.scdn.co',
						'https://mosaic.scdn.co',
						'https://*.spotifycdn.com',
						'https://img.clerk.com',
						'https://avatars.githubusercontent.com'
					],
					'font-src': ['self', 'data:'],
					'connect-src': [
						'self',
						...CLERK,
						`${CLERK_PROTECT}:*`,
						// Clerk dev instances send telemetry here.
						'https://clerk-telemetry.com',
						TURNSTILE
					],
					'frame-src': [TURNSTILE, 'https://open.spotify.com'],
					// Clerk and heic-to start workers from blob: URLs.
					'worker-src': ['self', 'blob:'],
					'media-src': ['self', MEDIA],
					'form-action': ['self'],
					...enforced(),
					'report-uri': ['/csp-report']
				}
			}
		})
	]
});
