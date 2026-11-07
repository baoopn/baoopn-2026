<script lang="ts">
	import { CircleAlert, LoaderCircle, LogOut, RefreshCw, ShieldX } from '@lucide/svelte';
	import BrandMark from '../BrandMark.svelte';
	import { auth } from '../../auth.svelte';

	let { kind }: { kind: 'loading' | 'signed-out' | 'forbidden' | 'unauthorized' | 'error' } =
		$props();

	const email = $derived(auth.clerk?.user?.primaryEmailAddress?.emailAddress ?? null);

	/** The "tracklist" shown beside the sign-in card on wider screens. */
	const INSIDE = [
		{ title: 'Posts', note: 'Markdown, drafts, schedule' },
		{ title: 'Projects', note: 'Synced from GitHub' },
		{ title: 'Inbox', note: 'Contact threads by email' },
		{ title: 'Analytics', note: 'Cookieless page views' }
	];

	/** Mounts Clerk's <SignIn /> into the element; unmounts on cleanup. */
	function clerkSignIn(node: HTMLDivElement) {
		const clerk = auth.clerk;
		if (!clerk) return;
		const back = window.location.href;
		clerk.mountSignIn(node, {
			routing: 'hash',
			forceRedirectUrl: back,
			fallbackRedirectUrl: back
		});
		return () => clerk.unmountSignIn(node);
	}
</script>

<svelte:head>
	<title>{kind === 'signed-out' ? 'Sign in' : 'Admin'} · baoopn</title>
</svelte:head>

<main id="main" class="grid min-h-dvh place-items-center px-4 py-10 sm:px-6">
	{#if kind === 'loading'}
		<div class="flex flex-col items-center gap-4" role="status" aria-live="polite">
			<BrandMark class="size-14" />
			<span class="eyebrow flex items-center gap-2">
				<LoaderCircle class="size-4 animate-spin" aria-hidden="true" /> Loading dashboard…
			</span>
		</div>
	{:else if kind === 'signed-out'}
		<!--
			Mobile: one centred column (brand, short title, card, back link).
			md+: an editorial two-column spread: liner-note copy on the left with
			a numbered "tracklist" of what is inside, the sign-in card on the right.
		-->
		<div
			class="grid w-full max-w-[60rem] gap-8 md:grid-cols-[minmax(0,1fr)_minmax(0,23rem)] md:items-center md:gap-12 lg:gap-20"
		>
			<section
				aria-labelledby="signin-title"
				class="flex flex-col items-center text-center md:items-start md:text-left"
			>
				<BrandMark class="size-12 md:size-14" />
				<p class="eyebrow mt-5">baoopn.com · admin</p>
				<h1
					id="signin-title"
					class="mt-2 font-display text-4xl leading-[0.95] font-semibold tracking-[-0.03em] text-ink sm:text-5xl lg:text-6xl"
				>
					Behind the<br />portfolio.
				</h1>
				<p class="mt-4 max-w-sm text-ink-soft">
					Posts, projects, the inbox and the numbers. Sign in with your admin account.
				</p>

				<ol class="mt-8 hidden w-full max-w-sm border-t border-ink md:block" aria-label="In the dashboard">
					{#each INSIDE as item, i (item.title)}
						<li class="flex items-baseline gap-4 border-b border-rule py-2.5">
							<span class="eyebrow w-6 shrink-0 tabular-nums">{String(i + 1).padStart(2, '0')}</span>
							<span class="font-medium text-ink">{item.title}</span>
							<span class="ml-auto truncate text-sm text-ink-soft">{item.note}</span>
						</li>
					{/each}
				</ol>
			</section>

			<div class="mx-auto flex w-full max-w-[23rem] flex-col items-center gap-6 md:mx-0">
				<div {@attach clerkSignIn} class="w-full min-h-[20rem]"></div>
				<a
					href="/"
					data-sveltekit-reload
					class="link inline-flex min-h-11 items-center font-mono text-sm text-ink-soft"
					>← Back to baoopn.com</a
				>
			</div>
		</div>
	{:else}
		<div class="card-print w-full max-w-md p-8">
			<div
				class={[
					'inline-flex items-center gap-2 rounded-full px-3 py-1 text-on-spot',
					kind === 'forbidden' ? 'bg-spot-butter' : 'bg-spot-rose'
				]}
			>
				{#if kind === 'forbidden'}
					<ShieldX class="size-4" aria-hidden="true" /><span class="eyebrow text-on-spot">403</span>
				{:else}
					<CircleAlert class="size-4" aria-hidden="true" /><span class="eyebrow text-on-spot"
						>Error</span
					>
				{/if}
			</div>

			{#if kind === 'forbidden'}
				<h1 class="mt-5 font-display text-3xl font-semibold text-ink">Not an admin</h1>
				<p class="mt-2 text-sm text-ink-soft">
					This account{#if email}&nbsp;(<span class="font-medium text-ink">{email}</span>){/if} is signed
					in but does not have the admin role. Sign in with a different account.
				</p>
			{:else if kind === 'unauthorized'}
				<h1 class="mt-5 font-display text-3xl font-semibold text-ink">Session not accepted</h1>
				<p class="mt-2 text-sm text-ink-soft">
					You are signed in, but the API rejected the session token. Check that the API's Clerk
					configuration matches this instance.
				</p>
				{#if auth.error}<p class="mt-2 font-mono text-xs break-words text-ink-soft">
						{auth.error}
					</p>{/if}
			{:else}
				<h1 class="mt-5 font-display text-3xl font-semibold text-ink">
					Could not start the dashboard
				</h1>
				<p class="mt-2 text-sm break-words text-ink-soft">{auth.error ?? 'Unknown error'}</p>
			{/if}

			<div class="mt-6 flex flex-wrap gap-2">
				{#if kind === 'error'}
					<button type="button" class="btn btn-primary" onclick={() => auth.retry()}>
						<RefreshCw class="size-4" aria-hidden="true" /> Try again
					</button>
				{:else}
					<button type="button" class="btn btn-secondary" onclick={() => auth.refresh()}>
						<RefreshCw class="size-4" aria-hidden="true" /> Retry
					</button>
				{/if}
				{#if auth.clerk?.user}
					<button type="button" class="btn btn-primary" onclick={() => auth.signOut()}>
						<LogOut class="size-4" aria-hidden="true" /> Sign out
					</button>
				{/if}
			</div>
		</div>
	{/if}
</main>
