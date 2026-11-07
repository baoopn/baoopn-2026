<script lang="ts">
	import { Globe, LogOut, Menu, Monitor, Moon, Sun } from '@lucide/svelte';
	import BrandMark from '../BrandMark.svelte';
	import Popover from './Popover.svelte';
	import { auth } from '../../auth.svelte';
	import { theme, type Mode } from '#lib/theme.svelte.js';

	let { sidebarOpen = $bindable(false) }: { sidebarOpen?: boolean } = $props();

	const initials = $derived(
		(auth.user?.name || auth.user?.email || '?')
			.split(/[\s@.]+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((s) => s[0]?.toUpperCase())
			.join('')
	);

	let avatarFailed = $state(false);

	const order: Mode[] = ['light', 'dark', 'system'];
	const ModeIcon = $derived(theme.mode === 'system' ? Monitor : theme.mode === 'dark' ? Moon : Sun);
	const nextMode = $derived(order[(order.indexOf(theme.mode) + 1) % order.length]);
</script>

<header
	class="sticky top-0 z-20 flex h-16 items-center gap-2 border-b border-rule bg-paper px-4 sm:px-6 lg:px-8"
>
	<button
		type="button"
		class="btn btn-ghost btn-icon lg:hidden"
		aria-label="Open navigation"
		aria-controls="admin-sidebar"
		aria-expanded={sidebarOpen}
		onclick={() => (sidebarOpen = true)}
	>
		<Menu class="size-5" aria-hidden="true" />
	</button>
	<a href="/admin" class="rounded-lg lg:hidden" aria-label="Admin overview">
		<BrandMark class="size-8" />
	</a>

	<div class="flex-1"></div>

	<a href="/" data-sveltekit-reload class="btn btn-ghost gap-2 px-2" title="Open baoopn.com">
		<Globe class="size-5" aria-hidden="true" />
		<span class="hidden text-sm font-medium sm:inline">View site</span>
		<span class="sr-only sm:hidden">View site</span>
	</a>

	<button
		type="button"
		class="btn btn-ghost btn-icon"
		aria-label="Theme: {theme.mode}. Switch to {nextMode}"
		title="Theme: {theme.mode}"
		onclick={() => theme.setMode(nextMode)}
	>
		<ModeIcon class="size-5" aria-hidden="true" />
	</button>

	<Popover label="Account menu" triggerClass="btn btn-ghost gap-2 px-1.5 sm:px-2">
		{#snippet trigger()}
			{#if auth.user?.image_url && !avatarFailed}
				<img
					src={auth.user.image_url}
					alt=""
					class="size-8 rounded-full border border-ink bg-paper-3 object-cover"
					referrerpolicy="no-referrer"
					onerror={() => (avatarFailed = true)}
				/>
			{:else}
				<span
					class="grid size-8 place-items-center rounded-full border border-ink bg-spot-select font-mono text-xs font-semibold text-on-spot"
					aria-hidden="true">{initials}</span
				>
			{/if}
			<span class="hidden max-w-40 truncate text-sm font-medium md:inline">
				{auth.user?.name || auth.user?.email}
			</span>
		{/snippet}
		{#snippet children(close)}
			<div class="px-1 pb-3">
				<p class="truncate text-sm font-medium text-ink">{auth.user?.name || 'Admin'}</p>
				<p class="truncate text-xs text-ink-soft">{auth.user?.email}</p>
			</div>
			{#if auth.config?.version}
				<p class="eyebrow flex items-center gap-2 border-t border-rule px-1 py-2">
					API <span class="font-mono text-ink">{auth.config.version}</span>
				</p>
			{/if}
			<button
				type="button"
				class="btn btn-ghost w-full justify-start"
				onclick={() => {
					close();
					void auth.signOut();
				}}
			>
				<LogOut class="size-4" aria-hidden="true" /> Sign out
			</button>
		{/snippet}
	</Popover>
</header>
