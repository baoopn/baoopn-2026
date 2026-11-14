<script lang="ts">
	import Menu from '@lucide/svelte/icons/menu';
	import X from '@lucide/svelte/icons/x';
	import { page } from '$app/state';
	import { afterNavigate } from '$app/navigation';
	import ThemeMenu from './ThemeMenu.svelte';
	import ThemeToggle from './ThemeToggle.svelte';
	import { NAV } from '#lib/site/defaults.js';
	import { brandSize, brandSrc } from '#lib/brand.js';

	let { name }: { name: string } = $props();

	const wordmark = $derived(brandSize(page.data.brand, 'wordmark_primary', 78, 32));

	let open = $state(false);
	afterNavigate(() => (open = false));

	function current(href: string): boolean {
		const p = page.url.pathname;
		return href === '/' ? p === '/' : p === href || p.startsWith(href + '/');
	}
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && open && (open = false)} />

<header class="site-header sticky top-0 z-40 border-b border-ink bg-paper">
	<div class="mx-auto flex max-w-6xl items-center gap-4 px-4 py-3 sm:px-6">
		<a href="/" class="flex min-h-11 shrink-0 items-center gap-3 rounded-[8px]" aria-label="{name}, home">
			<img
				src={brandSrc(page.data.brand, 'wordmark_primary')}
				alt=""
				width={wordmark.width}
				height={wordmark.height}
				class="h-8 w-auto"
			/>
			<span class="hidden font-display text-lg font-semibold tracking-tight sm:inline">{name}</span>
		</a>

		<nav aria-label="Main" class="ml-auto hidden md:block">
			<ul class="flex items-center gap-1">
				{#each NAV as item (item.href)}
					<li>
						<a
							href={item.href}
							class="navlink"
							aria-current={current(item.href) ? 'page' : undefined}>{item.label}</a
						>
					</li>
				{/each}
			</ul>
		</nav>

		<div class="hidden md:ml-2 md:block"><ThemeMenu /></div>

		<button
			type="button"
			class="btn btn-secondary btn-icon ml-auto md:hidden"
			aria-expanded={open}
			aria-controls="mobile-nav"
			onclick={() => (open = !open)}
		>
			{#if open}<X class="size-5" aria-hidden="true" />{:else}<Menu
					class="size-5"
					aria-hidden="true"
				/>{/if}
			<span class="sr-only">{open ? 'Close menu' : 'Open menu'}</span>
		</button>
	</div>

	<nav
		id="mobile-nav"
		aria-label="Main"
		class={[
			'max-h-[calc(100dvh-var(--header-h))] overflow-y-auto overscroll-contain border-t border-rule md:hidden',
			!open && 'hidden'
		]}
	>
		<ul class="mx-auto max-w-6xl px-4 py-2">
			{#each NAV as item, i (item.href)}
				<li class="border-b border-rule last:border-0">
					<a
						href={item.href}
						class="flex min-h-12 items-center gap-4 font-display text-xl font-semibold"
						aria-current={current(item.href) ? 'page' : undefined}
					>
						<span class="label w-6 text-ink-soft" aria-hidden="true">{String(i + 1).padStart(2, '0')}</span>
						<span class="mobile-label">{item.label}</span>
					</a>
				</li>
			{/each}
		</ul>
		<div class="mx-auto flex max-w-6xl items-center justify-between px-4 pb-4">
			<span class="label text-ink-soft">Theme</span>
			<ThemeToggle />
		</div>
	</nav>
</header>

<style>
	/* Its own layer in the page cross-fade, so the bar stays crisp. */
	.site-header {
		view-transition-name: site-header;
	}
	.navlink {
		display: inline-flex;
		align-items: center;
		min-height: 2.75rem;
		padding: 0 0.85rem;
		border-radius: 999px;
		font-weight: 500;
		font-size: 0.9375rem;
		transition: background-color 160ms ease-out;
	}
	.navlink:hover {
		background: var(--paper-2);
	}
	.navlink[aria-current='page'] {
		background: var(--spot-now);
		color: var(--on-spot);
		box-shadow: inset 0 0 0 1px var(--on-spot);
	}
	a[aria-current='page'] .mobile-label {
		text-decoration: underline;
		text-decoration-thickness: 2px;
		text-underline-offset: 0.25em;
		text-decoration-color: var(--spot-now);
	}
</style>
