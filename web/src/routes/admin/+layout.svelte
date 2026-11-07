<script lang="ts">
	import '#lib/admin/admin.css';
	import { onMount } from 'svelte';
	import Sidebar from '#lib/admin/components/shell/Sidebar.svelte';
	import TopBar from '#lib/admin/components/shell/TopBar.svelte';
	import AuthScreen from '#lib/admin/components/shell/AuthScreen.svelte';
	import Toasts from '#lib/admin/components/Toasts.svelte';
	import { auth } from '#lib/admin/auth.svelte.js';
	import { inbox } from '#lib/admin/inbox.svelte.js';
	import { theme } from '#lib/theme.svelte.js';

	let { children } = $props();

	let sidebarOpen = $state(false);

	onMount(() => {
		const stopTheme = theme.start();
		void auth.init();
		return stopTheme;
	});

	// Keep <html data-palette/data-theme> in sync (system mode follows the OS live).
	$effect(() => {
		theme.apply();
	});

	// Clerk signed in but our status hasn't caught up yet.
	$effect(() => {
		if (auth.status === 'signed-out' && auth.clerk?.user) void auth.refresh();
	});

	// Unread badge, only while signed in.
	$effect(() => {
		if (auth.status !== 'signed-in') return;
		return inbox.poll(60_000);
	});
</script>

<div class="admin">
	<a
		href="#main"
		class="sr-only z-50 rounded-lg bg-ink px-3 py-2 text-paper focus:not-sr-only focus:fixed focus:top-3 focus:left-3"
		>Skip to content</a
	>

	{#if auth.status === 'signed-in'}
		<div class="flex min-h-dvh">
			<Sidebar bind:open={sidebarOpen} />
			<div class="flex min-w-0 flex-1 flex-col">
				<TopBar bind:sidebarOpen />
				<main id="main" class="mx-auto w-full max-w-7xl flex-1 px-4 py-6 sm:px-6 lg:px-8">
					{@render children()}
				</main>
			</div>
		</div>
	{:else if auth.status === 'signed-out'}
		<AuthScreen kind="signed-out" />
	{:else if auth.status === 'forbidden' || auth.status === 'unauthorized' || auth.status === 'error'}
		<AuthScreen kind={auth.status} />
	{:else}
		<AuthScreen kind="loading" />
	{/if}

	<Toasts />
</div>
