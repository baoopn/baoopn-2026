<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import MapPin from '@lucide/svelte/icons/map-pin';
	import { page } from '$app/state';
	import Seo from '#lib/site/components/Seo.svelte';
	import ProjectCard from '#lib/site/components/ProjectCard.svelte';
	import PostItem from '#lib/site/components/PostItem.svelte';
	import NowPlayingCard from '#lib/site/components/NowPlayingCard.svelte';
	import RecentPlays from '#lib/site/components/RecentPlays.svelte';
	import TopPlaylists from '#lib/site/components/TopPlaylists.svelte';
	import { absolute, safeImage, safeUrl } from '#lib/site/format.js';
	import { LISTENING_URL } from '#lib/links.js';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const profile = $derived(data.profile);
	const avatar = $derived(safeImage(profile.avatar_url));
	const resume = $derived(safeUrl(profile.resume_url));
	let avatarFailed = $state(false);

	const person = $derived({
		'@context': 'https://schema.org',
		'@type': 'Person',
		name: profile.name,
		jobTitle: profile.headline,
		url: page.url.origin + '/',
		image: absolute(page.url.origin, profile.avatar_url),
		email: profile.email ? `mailto:${profile.email}` : undefined,
		address: profile.location
			? { '@type': 'PostalAddress', addressLocality: profile.location }
			: undefined,
		sameAs: profile.socials.map((s) => safeUrl(s.url)).filter((u) => u?.startsWith('http'))
	});
</script>

<Seo schema={person} type="profile" />

{#snippet sectionHead(no: string, title: string, id: string, href?: string, cta?: string)}
	<div class="flex flex-wrap items-end justify-between gap-x-6 gap-y-2 border-b border-ink pb-3">
		<h2 {id} class="flex items-baseline gap-3 font-display text-3xl font-semibold tracking-[-0.02em] sm:text-4xl">
			<span class="label text-ink-soft" aria-hidden="true">{no}</span>
			{title}
		</h2>
		{#if href && cta}
			<a {href} class="link inline-flex min-h-11 items-center gap-1.5 font-medium">
				{cta}<ArrowRight class="size-4" aria-hidden="true" />
			</a>
		{/if}
	</div>
{/snippet}

<div class="mx-auto max-w-6xl px-4 sm:px-6">
	<!-- Hero: a record-sleeve front. -->
	<section aria-labelledby="hero-name" class="hero pt-12 pb-16 sm:pt-16 lg:pt-20">
		<div class="min-w-0">
			<p class="label flex flex-wrap gap-x-3 text-ink-soft">
				<span>baoopn.com</span><span aria-hidden="true">/</span><span>Liner notes</span><span
					aria-hidden="true">/</span
				><span>Vol. {new Date().getFullYear()}</span>
			</p>
			<h1
				id="hero-name"
				class="mt-5 font-display text-[clamp(3rem,11vw,7.5rem)] leading-[0.92] font-semibold tracking-[-0.045em] text-balance"
			>
				{profile.name}
			</h1>
			<p class="mt-5 max-w-xl font-display text-2xl leading-snug font-normal tracking-[-0.01em] sm:text-3xl">
				{profile.headline}
			</p>

			<dl class="credits mt-8 max-w-xl border-y border-rule py-4 text-sm">
				{#if profile.location}
					<dt class="label text-ink-soft">Based in</dt>
					<dd class="flex items-center gap-1.5"><MapPin class="size-4" aria-hidden="true" />{profile.location}</dd>
				{/if}
				{#if profile.pronouns}
					<dt class="label text-ink-soft">Pronouns</dt>
					<dd>{profile.pronouns}</dd>
				{/if}
				<dt class="label text-ink-soft">Status</dt>
				<dd>
					{#if profile.available_for_work}
						<span class="chip label [--chip:var(--spot-mint)]">
							<span class="size-1.5 rounded-full bg-on-spot" aria-hidden="true"></span>
							Available for work
						</span>
					{:else}
						Building things, open to a good conversation
					{/if}
				</dd>
			</dl>

			<div class="mt-8 flex flex-wrap gap-3">
				<a href="/projects" class="btn btn-primary">See the work <ArrowRight class="size-4" aria-hidden="true" /></a>
				<a href="/contact" class="btn btn-secondary">Say hello</a>
				{#if resume}
					<a href={resume} class="btn btn-secondary" target="_blank" rel="noopener noreferrer">Résumé</a>
				{/if}
			</div>
		</div>

		<div class="sleeve">
			{#if avatar && !avatarFailed}
				<img
					src={avatar}
					alt="Portrait of {profile.name}"
					width="640"
					height="640"
					fetchpriority="high"
					class="print aspect-square w-full rounded-brand bg-paper-2 object-cover [--print:var(--spot-now)]"
					onerror={() => (avatarFailed = true)}
				/>
			{:else}
				<img
					src="/logo/baoopn-monogram-primary.svg"
					alt=""
					width="640"
					height="640"
					class="print aspect-square w-full rounded-brand [--print:var(--spot-now)]"
				/>
			{/if}
			{#if profile.available_for_work}
				<span class="sticker label" aria-hidden="true">Open<br />for<br />work</span>
			{/if}
		</div>
	</section>

	<!-- 01 Selected work -->
	<section aria-labelledby="work" class="py-10">
		{@render sectionHead('01', 'Selected work', 'work', '/projects', 'All projects')}
		{#if data.projects.length === 0}
			<p class="py-8 text-ink-soft">Projects are being re-pressed. Check back soon.</p>
		{:else}
			<ul class="mt-8 grid gap-x-8 gap-y-12 sm:grid-cols-2 lg:grid-cols-3">
				{#each data.projects as project, i (project.slug)}
					<li><ProjectCard {project} index={i} /></li>
				{/each}
			</ul>
		{/if}
	</section>

	<!-- 02 Writing -->
	<section aria-labelledby="writing" class="py-10">
		{@render sectionHead('02', 'Latest writing', 'writing', '/blog', 'All posts')}
		{#if data.posts.length === 0}
			<p class="py-8 text-ink-soft">No posts yet. The first one is in the press.</p>
		{:else}
			<ul>
				{#each data.posts as post (post.slug)}
					<li><PostItem {post} /></li>
				{/each}
			</ul>
		{/if}
	</section>

	<!-- 03 Listening -->
	<section id="listening" aria-labelledby="listening-title" class="scroll-mt-6 py-10">
		{@render sectionHead('03', 'On the turntable', 'listening-title')}
		<div class="mt-8 grid gap-10 lg:grid-cols-[minmax(0,7fr)_minmax(0,5fr)]">
			<div>
				<NowPlayingCard initial={data.nowPlaying} />
				<a href={LISTENING_URL} class="btn btn-secondary mt-4">
					See what I'm listening to <ArrowRight class="size-4" aria-hidden="true" />
				</a>
			</div>
			<div>
				<h3 class="label mb-3 text-ink-soft">Recently played</h3>
				<RecentPlays items={data.recent} />
			</div>
		</div>
		<TopPlaylists data={data.topPlaylists} />
	</section>
</div>

<style>
	.hero {
		display: grid;
		gap: 3rem;
		align-items: center;
	}
	@media (min-width: 56rem) {
		.hero {
			grid-template-columns: minmax(0, 7fr) minmax(0, 4fr);
			gap: 4rem;
		}
	}
	.credits {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: 0.6rem 1.5rem;
		align-items: center;
	}
	.sleeve {
		position: relative;
		max-width: 22rem;
		width: 100%;
		margin-inline: auto;
	}
	@media (min-width: 56rem) {
		.sleeve {
			max-width: none;
			transform: rotate(1.5deg);
		}
	}
	/* A round price-sticker on the sleeve. */
	.sticker {
		position: absolute;
		left: -1rem;
		bottom: -1.25rem;
		display: grid;
		place-items: center;
		width: 6.5rem;
		height: 6.5rem;
		border-radius: 999px;
		border: 1px solid var(--on-spot);
		background: var(--spot-mint);
		color: var(--on-spot);
		text-align: center;
		line-height: 1.25;
		transform: rotate(-10deg);
	}
</style>
