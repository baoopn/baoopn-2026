<script lang="ts">
	import ArrowUpRight from '@lucide/svelte/icons/arrow-up-right';
	import Seo from '#lib/site/components/Seo.svelte';
	import SocialIcon from '#lib/site/components/SocialIcon.svelte';
	import AlbumArt from '#lib/site/components/AlbumArt.svelte';
	import { absolute, pad2, prettyUrl, safeImage, safeUrl, yearMonth } from '#lib/site/format.js';
	import { reveal } from '#lib/site/motion.js';
	import { page } from '$app/state';
	import { graph, personNode, profilePageNode, sameAs } from '#lib/site/seo.js';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const profile = $derived(data.profile);
	const portrait = $derived(safeImage(profile.cover_url) ?? safeImage(profile.avatar_url));
	const resume = $derived(safeUrl(profile.resume_url));
	const socials = $derived(
		profile.socials
			.map((s) => ({ ...s, href: safeUrl(s.url) }))
			.filter((s): s is typeof s & { href: string } => !!s.href)
	);
	let portraitFailed = $state(false);

	const schema = $derived.by(() => {
		const origin = page.url.origin;
		const person = personNode(origin, {
			name: profile.name,
			headline: profile.headline,
			image: absolute(origin, profile.avatar_url),
			email: profile.email,
			location: profile.location,
			sameAs: sameAs(profile.socials)
		});
		return graph([profilePageNode(origin, person, profile.updated_at)]);
	});
</script>

<Seo
	title="About"
	description={`About ${profile.name}, ${profile.headline}: work history, skills and favorite artists.`}
	type="profile"
	{schema}
/>

<div class="mx-auto max-w-6xl px-4 sm:px-6">
	<header class="border-b border-ink pt-12 pb-6 sm:pt-16">
		<p class="label text-ink-soft">Liner notes · About</p>
		<h1 class="mt-3 font-display text-5xl font-semibold tracking-[-0.035em] text-balance sm:text-6xl">
			Hi, I'm {profile.name.split(' ')[0]}.
		</h1>
		<p class="mt-3 max-w-2xl text-lg text-ink-soft sm:text-xl">{profile.headline}</p>
	</header>

	<section aria-label="Bio" class="grid gap-10 py-10 lg:grid-cols-[minmax(0,7fr)_minmax(0,4fr)]">
		<div class="min-w-0">
			{#if profile.bio_html}
				<div class="prose max-w-[68ch]">
					<!-- eslint-disable-next-line svelte/no-at-html-tags -- admin-authored Markdown; the API keeps its raw HTML as-is (trusted), it is not sanitized -->
					{@html profile.bio_html}
				</div>
			{:else}
				<div class="prose max-w-[68ch]">
					<p>
						I'm a full-stack developer who likes building things end to end: interfaces people enjoy
						using, and the APIs and databases behind them.
					</p>
					<p>Have a look at the projects, read the blog, or say hello.</p>
				</div>
			{/if}
			<div class="mt-8 flex flex-wrap gap-3">
				<a href="/contact" class="btn btn-primary">Get in touch</a>
				{#if resume}
					<a href={resume} target="_blank" rel="noopener noreferrer" class="btn btn-secondary">
						Résumé <ArrowUpRight class="size-4" aria-hidden="true" />
					</a>
				{/if}
			</div>
		</div>
		<div>
			{#if portrait && !portraitFailed}
				<img
					src={portrait}
					alt="Photo of {profile.name}"
					width="800"
					height="1000"
					class="print aspect-[4/5] w-full max-w-sm rounded-brand bg-paper-2 object-cover [--print:var(--spot-now)]"
					onerror={() => (portraitFailed = true)}
				/>
			{/if}
			{#if socials.length}
				<ul class={['border-t border-ink', portrait && !portraitFailed && 'mt-8']} aria-label="Elsewhere">
					{#each socials as s (s.href)}
						{@const external = s.href.startsWith('http')}
						<li class="border-b border-rule">
							<a
								href={s.href}
								class="flex min-h-12 items-center gap-3 py-2 hover:bg-paper-2"
								target={external ? '_blank' : undefined}
								rel={external ? 'me noopener noreferrer' : undefined}
							>
								<SocialIcon icon={s.icon} />
								<span class="font-medium">{s.label}</span>
								<span class="ml-auto truncate font-mono text-sm text-ink-soft">{s.handle || prettyUrl(s.href)}</span>
							</a>
						</li>
					{/each}
				</ul>
			{/if}
		</div>
	</section>

	{#if profile.experience.length}
		<section aria-labelledby="exp-h" class="py-10">
			<h2 id="exp-h" class="flex items-baseline gap-3 border-b border-ink pb-3 font-display text-3xl font-semibold tracking-[-0.02em]">
				<span class="label text-ink-soft" aria-hidden="true">01</span> Experience
			</h2>
			<ol class="timeline">
				{#each profile.experience as job, i (i)}
					{@const orgUrl = safeUrl(job.url)}
					<li class="job" data-reveal {@attach reveal()}>
						<p class="label text-ink-soft">
							<time datetime={job.start}>{yearMonth(job.start)}</time>
							–
							{#if job.end}<time datetime={job.end}>{yearMonth(job.end)}</time>{:else}Present{/if}
						</p>
						<div class="min-w-0">
							<h3 class="font-display text-xl font-semibold tracking-[-0.01em]">{job.role}</h3>
							<p class="text-ink-soft">
								{#if orgUrl}
									<a href={orgUrl} target="_blank" rel="noopener noreferrer" class="link font-medium text-ink">{job.org}</a>
								{:else}
									<span class="font-medium text-ink">{job.org}</span>
								{/if}
								{#if job.location}<span> · {job.location}</span>{/if}
							</p>
							{#if job.summary_html}
								<div class="prose mt-2 text-base!">
									<!-- eslint-disable-next-line svelte/no-at-html-tags -- admin-authored Markdown; the API keeps its raw HTML as-is (trusted), it is not sanitized -->
									{@html job.summary_html}
								</div>
							{/if}
						</div>
					</li>
				{/each}
			</ol>
		</section>
	{/if}

	{#if profile.skills.length}
		<section aria-labelledby="skills-h" class="py-10">
			<h2 id="skills-h" class="flex items-baseline gap-3 border-b border-ink pb-3 font-display text-3xl font-semibold tracking-[-0.02em]">
				<span class="label text-ink-soft" aria-hidden="true">02</span> Toolkit
			</h2>
			<dl class="mt-2 grid gap-x-10 sm:grid-cols-2">
				{#each profile.skills as g (g.group)}
					<div class="border-b border-rule py-4">
						<dt class="label text-ink-soft">{g.group}</dt>
						<dd class="mt-2 flex flex-wrap gap-1.5">
							{#each g.items as item (item)}
								<span class="rounded-full border border-rule-strong px-2.5 py-1 text-sm">{item}</span>
							{/each}
						</dd>
					</div>
				{/each}
			</dl>
		</section>
	{/if}

	<section aria-labelledby="artists-h" class="py-10">
		<div class="flex flex-wrap items-end justify-between gap-2 border-b border-ink pb-3">
			<h2 id="artists-h" class="flex items-baseline gap-3 font-display text-3xl font-semibold tracking-[-0.02em]">
				<span class="label text-ink-soft" aria-hidden="true">03</span> On repeat
			</h2>
			<p class="label text-ink-soft">Top artists · last 30 days</p>
		</div>
		{#if data.topArtists.length === 0}
			<p class="py-6 text-ink-soft">The charts are quiet right now.</p>
		{:else}
			<ol class="mt-6 grid grid-cols-2 gap-x-6 gap-y-8 sm:grid-cols-3 lg:grid-cols-6">
				{#each data.topArtists as a, i (a.artist.id)}
					{@const href = safeUrl(a.artist.external_url)}
					<li class="min-w-0" data-reveal {@attach reveal()}>
						<AlbumArt
							src={a.artist.image_url}
							alt=""
							class="w-full rounded-full border border-ink"
						/>
						<p class="label mt-3 text-ink-soft">{pad2(i + 1)}</p>
						<p class="truncate font-semibold">
							{#if href}
								<a {href} target="_blank" rel="noopener noreferrer" class="link">{a.artist.name}</a>
							{:else}
								{a.artist.name}
							{/if}
						</p>
						<p class="font-mono text-xs text-ink-soft tabular-nums">{a.plays} plays</p>
					</li>
				{/each}
			</ol>
		{/if}
	</section>
</div>

<style>
	.timeline {
		margin-top: 0.5rem;
	}
	.job {
		display: grid;
		gap: 0.35rem 2rem;
		padding-block: 1.25rem;
		border-bottom: 1px solid var(--rule);
	}
	@media (min-width: 40rem) {
		.job {
			grid-template-columns: 11rem minmax(0, 1fr);
		}
		.job > p:first-child {
			padding-top: 0.4rem;
		}
	}
</style>
