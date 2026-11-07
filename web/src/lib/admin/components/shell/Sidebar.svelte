<script lang="ts">
	import {
		ChartLine,
		ExternalLink,
		FolderGit2,
		Headphones,
		Inbox,
		LayoutDashboard,
		NotebookPen,
		Settings,
		UserRound,
		X
	} from '@lucide/svelte';
	import { page } from '$app/state';
	import { MediaQuery } from 'svelte/reactivity';
	import { LISTENING_ADMIN_URL } from '#lib/links.js';
	import BrandMark from '../BrandMark.svelte';
	import { auth } from '../../auth.svelte';
	import { inbox } from '../../inbox.svelte';

	let { open = $bindable(false) }: { open?: boolean } = $props();

	const desktop = new MediaQuery('min-width: 1024px');

	const items = [
		{ href: '/admin', label: 'Overview', icon: LayoutDashboard, exact: true },
		{ href: '/admin/inbox', label: 'Inbox', icon: Inbox, exact: false, badge: true },
		{ href: '/admin/posts', label: 'Posts', icon: NotebookPen, exact: false },
		{ href: '/admin/projects', label: 'Projects', icon: FolderGit2, exact: false },
		{ href: '/admin/analytics', label: 'Analytics', icon: ChartLine, exact: false },
		{ href: '/admin/profile', label: 'Profile', icon: UserRound, exact: false },
		{ href: '/admin/settings', label: 'Settings', icon: Settings, exact: false }
	];

	function isActive(href: string, exact: boolean): boolean {
		const path = page.url.pathname.replace(/\/$/, '');
		return exact ? path === href : path === href || path.startsWith(href + '/');
	}
</script>

{#if open && !desktop.current}
	<button
		type="button"
		class="fixed inset-0 z-30 bg-ink/30"
		aria-label="Close navigation"
		onclick={() => (open = false)}
	></button>
{/if}

<aside
	id="admin-sidebar"
	class={[
		'fixed inset-y-0 left-0 z-40 flex w-64 flex-col border-r border-rule bg-paper transition-transform duration-200 lg:sticky lg:top-0 lg:h-dvh lg:translate-x-0',
		open ? 'translate-x-0' : '-translate-x-full'
	]}
	aria-label="Admin navigation"
	inert={!open && !desktop.current}
>
	<div class="flex h-16 items-center justify-between gap-2 px-5">
		<a href="/admin" class="flex items-center gap-3 rounded-xl">
			<BrandMark class="size-8" />
			<span class="font-display text-lg font-semibold">baoopn</span>
			<span class="eyebrow">Admin</span>
		</a>
		<button
			type="button"
			class="btn btn-ghost btn-icon btn-sm lg:hidden"
			aria-label="Close navigation"
			onclick={() => (open = false)}
		>
			<X class="size-4" aria-hidden="true" />
		</button>
	</div>

	<nav class="flex-1 overflow-y-auto px-3 pt-2 pb-6">
		<ul class="flex flex-col gap-0.5">
			{#each items as item (item.href)}
				{@const active = isActive(item.href, item.exact)}
				<li>
					<a
						href={item.href}
						aria-current={active ? 'page' : undefined}
						onclick={() => (open = false)}
						class={[
							'flex min-h-11 items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors duration-150',
							active
								? 'bg-spot-select text-on-spot'
								: 'text-ink-soft hover:bg-paper-3 hover:text-ink'
						]}
					>
						<item.icon class="size-[1.125rem] shrink-0" aria-hidden="true" />
						<span class="flex-1">{item.label}</span>
						{#if item.badge && inbox.unread > 0}
							<span
								class="rounded-full border border-on-spot bg-spot-now px-2 font-mono text-xs text-on-spot"
								>{inbox.unread}<span class="sr-only"> unread</span></span
							>
						{/if}
					</a>
				</li>
			{/each}
			<li class="mt-2 border-t border-rule pt-2">
				<a
					href={LISTENING_ADMIN_URL}
					target="_blank"
					rel="noopener"
					onclick={() => (open = false)}
					class="flex min-h-11 items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium text-ink-soft transition-colors duration-150 hover:bg-paper-3 hover:text-ink"
				>
					<Headphones class="size-[1.125rem] shrink-0" aria-hidden="true" />
					<span class="flex-1">Listening admin</span>
					<ExternalLink class="size-3.5 shrink-0" aria-hidden="true" />
					<span class="sr-only">(opens in a new tab)</span>
				</a>
			</li>
		</ul>
	</nav>

	<div class="flex flex-col gap-1 border-t border-rule px-5 py-3">
		<a
			href="/"
			data-sveltekit-reload
			class="inline-flex min-h-11 items-center gap-2 font-mono text-xs text-ink-soft hover:text-ink"
		>
			<ExternalLink class="size-3.5" aria-hidden="true" /> baoopn.com
		</a>
		{#if auth.config?.version}
			<p class="eyebrow">API {auth.config.version}</p>
		{/if}
	</div>
</aside>
