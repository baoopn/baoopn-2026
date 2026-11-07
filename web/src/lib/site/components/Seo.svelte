<script lang="ts">
	import { page } from '$app/state';
	import { absolute, jsonLd } from '#lib/site/format.js';
	import { DEFAULT_PROFILE, SITE_DESCRIPTION } from '#lib/site/defaults.js';
	import type { Profile } from '#lib/api/types.js';

	let {
		title,
		description,
		image,
		type = 'website',
		path,
		published,
		modified,
		schema,
		noindex = false
	}: {
		/** Page title; the site name is appended (omit for the home page). */
		title?: string;
		description?: string;
		image?: string;
		type?: 'website' | 'article' | 'profile';
		/** Canonical path; defaults to the current pathname. */
		path?: string;
		published?: string;
		modified?: string;
		/** JSON-LD object(s). */
		schema?: unknown;
		noindex?: boolean;
	} = $props();

	const profile = $derived<Profile>(page.data.profile ?? DEFAULT_PROFILE);
	const origin = $derived(page.url.origin);
	const siteName = $derived(profile.seo.title || profile.name);
	const fullTitle = $derived(title ? `${title} · ${profile.name}` : siteName);
	const desc = $derived(
		(description || profile.seo.description || SITE_DESCRIPTION).replace(/\s+/g, ' ').trim().slice(0, 300)
	);
	const canonical = $derived(origin + (path ?? page.url.pathname));
	const img = $derived(
		absolute(origin, image) ?? absolute(origin, profile.seo.og_image) ?? absolute(origin, profile.avatar_url)
	);
</script>

<svelte:head>
	<title>{fullTitle}</title>
	<meta name="description" content={desc} />
	<link rel="canonical" href={canonical} />
	{#if noindex}<meta name="robots" content="noindex" />{/if}
	<meta property="og:site_name" content={siteName} />
	<meta property="og:type" content={type} />
	<meta property="og:title" content={title ?? siteName} />
	<meta property="og:description" content={desc} />
	<meta property="og:url" content={canonical} />
	<meta property="og:locale" content="en_US" />
	{#if img}
		<meta property="og:image" content={img} />
		<meta name="twitter:image" content={img} />
	{/if}
	{#if published}<meta property="article:published_time" content={published} />{/if}
	{#if modified}<meta property="article:modified_time" content={modified} />{/if}
	<meta name="twitter:card" content={img ? 'summary_large_image' : 'summary'} />
	<meta name="twitter:title" content={title ?? siteName} />
	<meta name="twitter:description" content={desc} />
	<link rel="alternate" type="application/rss+xml" title="{profile.name} · Blog" href="/rss.xml" />
	{#if schema}
		<!-- eslint-disable-next-line svelte/no-at-html-tags -- JSON.stringify output with < escaped -->
		{@html jsonLd(schema)}
	{/if}
</svelte:head>
