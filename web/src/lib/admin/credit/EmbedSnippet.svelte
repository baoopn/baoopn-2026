<script lang="ts">
	import { Copy } from '@lucide/svelte';
	import { toasts } from '../toast.svelte';
	import { snippet } from './rules.js';

	let {
		endpoint,
		logoUrl
	}: {
		endpoint: string;
		/** One of the logo URLs, to name its host in the CSP note. */
		logoUrl: string;
	} = $props();

	const code = $derived(snippet(endpoint));
	const apiOrigin = $derived(originOf(endpoint));
	const logoOrigin = $derived(originOf(logoUrl));

	function originOf(u: string): string {
		try {
			return new URL(u).origin;
		} catch {
			return u;
		}
	}

	async function copy() {
		try {
			await navigator.clipboard.writeText(code);
			toasts.success('Snippet copied');
		} catch {
			toasts.error('Could not copy the snippet', 'Select the text and copy it by hand.');
		}
	}
</script>

<section class="card flex min-w-0 flex-col gap-4 p-4 sm:p-5" aria-labelledby="credit-embed-h">
	<div class="flex items-start justify-between gap-3">
		<div class="min-w-0">
			<h2 id="credit-embed-h" class="eyebrow">Embed snippet</h2>
			<p class="hint mt-1">Paste this into the client site footer.</p>
		</div>
		<button type="button" class="btn btn-secondary btn-sm shrink-0" onclick={copy}>
			<Copy class="size-4" aria-hidden="true" />
			Copy
		</button>
	</div>

	<!-- Focusable so keyboard users can scroll the snippet. -->
	<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
	<pre
		class="max-h-96 max-w-full min-w-0 overflow-auto rounded-lg border border-rule bg-paper-2 p-3 font-mono text-xs leading-relaxed text-ink"
		aria-label="Embed snippet"
		tabindex="0"><code>{code}</code></pre>

	<ul class="hint mt-0 flex list-disc flex-col gap-1 pl-5">
		<li>Add the client site's origin to the list, or the browser blocks the request.</li>
		<li>
			If the client site sets a Content Security Policy, its
			<code class="font-mono">img-src</code> must allow
			<code class="font-mono break-all">{logoOrigin}</code> and its
			<code class="font-mono">connect-src</code> must allow
			<code class="font-mono break-all">{apiOrigin}</code>.
		</li>
		<li>The static line stays in place when the script cannot load the credit.</li>
	</ul>
</section>
