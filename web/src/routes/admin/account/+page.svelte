<script lang="ts">
	import type { Attachment } from 'svelte/attachments';
	import PageHeader from '#lib/admin/components/PageHeader.svelte';
	import { auth, userProfileAppearance } from '#lib/admin/auth.svelte.js';

	// Mounts Clerk's UserProfile (sign-in, security, API keys). Hash routing
	// keeps its tabs inside this one route. The attachment re-runs when
	// auth.clerk changes and unmounts the profile when the page is left.
	const clerkProfile: Attachment<HTMLDivElement> = (node) => {
		const c = auth.clerk;
		if (!c) return;
		c.mountUserProfile(node, {
			routing: 'hash',
			appearance: userProfileAppearance(),
			apiKeysProps: { showDescription: true }
		});
		return () => c.unmountUserProfile(node);
	};
</script>

<PageHeader title="Account" description="Your Clerk sign-in, security and API keys for scripts." />

<div class="min-h-[32rem] w-full" {@attach clerkProfile}></div>

<p class="mt-4 text-sm text-ink-soft">
	Scripts send the key as <code class="font-mono">Authorization: Bearer</code>. Only admin
	accounts' keys work.
</p>
