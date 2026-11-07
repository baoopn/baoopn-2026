<script lang="ts">
	import NowPlayingChip from './NowPlayingChip.svelte';
	import SocialIcon from './SocialIcon.svelte';
	import { NAV } from '#lib/site/defaults.js';
	import { safeUrl } from '#lib/site/format.js';
	import type { Profile } from '#lib/api/types.js';

	let { profile }: { profile: Profile } = $props();

	const links = $derived(
		[
			...profile.socials.map((s) => ({ ...s, href: safeUrl(s.url) })),
			...(profile.email && !profile.socials.some((s) => s.icon === 'email')
				? [{ label: 'Email', icon: 'email', url: '', handle: '', href: `mailto:${profile.email}` }]
				: []),
			{ label: 'RSS', icon: 'rss', url: '', handle: '', href: '/rss.xml' }
		].filter((l) => l.href)
	);
	const year = new Date().getFullYear();
</script>

<footer class="mt-24 border-t border-ink">
	<div class="mx-auto grid max-w-6xl gap-8 px-4 py-10 sm:px-6 md:grid-cols-[1fr_auto]">
		<div class="min-w-0 space-y-5">
			<p class="font-display text-2xl font-semibold tracking-tight text-balance">
				Thanks for reading the liner notes.
			</p>
			<div class="max-w-md"><NowPlayingChip /></div>
		</div>

		<nav aria-label="Footer" class="grid grid-cols-2 gap-x-10 gap-y-1 sm:grid-cols-[auto_auto]">
			<div>
				<p class="label mb-2 text-ink-soft">Pages</p>
				<ul>
					{#each NAV as item (item.href)}
						<li><a href={item.href} class="link inline-flex min-h-11 items-center">{item.label}</a></li>
					{/each}
				</ul>
			</div>
			<div>
				<p class="label mb-2 text-ink-soft">Elsewhere</p>
				<ul>
					{#each links as l (l.href)}
						{@const external = !!l.href?.startsWith('http')}
						<li>
							<a
								href={l.href}
								class="link inline-flex min-h-11 items-center gap-2"
								target={external ? '_blank' : undefined}
								rel={external ? 'me noopener noreferrer' : undefined}
							>
								<SocialIcon icon={l.icon} class="size-4" />
								{l.label}
							</a>
						</li>
					{/each}
				</ul>
			</div>
		</nav>
	</div>
	<div class="border-t border-rule">
		<p
			class="label mx-auto flex max-w-6xl flex-wrap justify-between gap-2 px-4 py-4 text-ink-soft sm:px-6"
		>
			<span>© {year} {profile.name}</span>
			<span>Printed on baoopn.com</span>
		</p>
	</div>
</footer>
