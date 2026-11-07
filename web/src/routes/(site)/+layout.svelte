<script lang="ts">
	import '#lib/site/site.css';
	import { onMount } from 'svelte';
	import { afterNavigate, onNavigate } from '$app/navigation';
	import Header from '#lib/site/components/Header.svelte';
	import Footer from '#lib/site/components/Footer.svelte';
	import { startEngagement, trackPageview } from '#lib/site/beacon.js';
	import { theme } from '#lib/theme.svelte.js';
	import { reducedMotion } from '#lib/site/motion.js';
	import type { LayoutProps } from './$types';

	let { data, children }: LayoutProps = $props();

	onMount(() => {
		const stopTheme = theme.start();
		const stopEngagement = startEngagement();
		return () => {
			stopTheme();
			stopEngagement();
		};
	});
	// Mirror the resolved theme onto <html> (DOM sync only).
	$effect(() => theme.apply());

	afterNavigate(({ from, to }) => {
		if (!to?.url) return;
		const referrer = from?.url ? from.url.href : document.referrer;
		trackPageview(to.url.pathname, referrer);
	});

	// Soft cross-fade between site pages. Skipped for same-path changes (?tech=
	// filters, hash jumps), routes outside the site, reduced motion, and browsers
	// without View Transitions (navigation is then instant).
	onNavigate((nav) => {
		// After the first client navigation the hero intro (home page) stays still.
		// Set before the new page renders so its CSS animation never starts.
		if (nav.from) document.documentElement.dataset.navigated = '';
		if (!document.startViewTransition || reducedMotion()) return;
		if (nav.from?.url.pathname === nav.to?.url.pathname) return;
		if (!nav.to?.route.id?.startsWith('/(site)')) return;
		return new Promise((resolve) => {
			document.startViewTransition(async () => {
				resolve();
				await nav.complete;
			});
		});
	});
</script>

<a href="#main" class="skip">Skip to content</a>

<div class="flex min-h-dvh flex-col">
	<Header name={data.profile.name} />
	<main id="main" tabindex="-1" class="flex-1 focus:outline-none">
		{@render children()}
	</main>
	<Footer profile={data.profile} />
</div>

<style>
	.skip {
		position: fixed;
		top: 0.75rem;
		left: 0.75rem;
		z-index: 60;
		padding: 0.75rem 1rem;
		border-radius: 8px;
		border: 1px solid var(--ink);
		background: var(--ink);
		color: var(--paper);
		font-weight: 600;
		transform: translateY(-200%);
	}
	.skip:focus-visible {
		transform: none;
	}
</style>
