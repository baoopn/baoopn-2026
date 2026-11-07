<script lang="ts">
	import type { Snippet } from 'svelte';
	import { RefreshCw, RotateCw, X } from '@lucide/svelte';
	import { slide } from 'svelte/transition';
	import { MediaQuery } from 'svelte/reactivity';
	import { bytes } from '../format';
	import type { UploadJob } from '../media/upload.svelte';

	let {
		job,
		onfinished,
		onremove
	}: {
		job: UploadJob;
		/** Called shortly after a clean Ready, once the check mark has played. */
		onfinished?: () => void;
		/** Offers Remove (failed) / Dismiss (ready with a warning). */
		onremove?: () => void;
	} = $props();

	const uid = $props.id();
	const reduced = new MediaQuery('prefers-reduced-motion: reduce');

	const steps = [
		{ n: '01', label: 'Upload', stage: 'uploading' },
		{ n: '02', label: 'Convert', stage: 'converting' },
		{ n: '03', label: 'Done', stage: 'ready' }
	] as const;

	const current = $derived(
		job.stage === 'failed'
			? job.failKind === 'conversion'
				? 'converting'
				: 'uploading'
			: job.stage
	);
	const caution = $derived(!!job.warning || job.slow || job.retrying);
	const tone = $derived(
		job.stage === 'failed' ? 'bg-spot-rose' : caution ? 'bg-spot-butter' : 'bg-spot-select'
	);

	const name = $derived(job.file.name);
	const status = $derived.by(() => {
		if (job.stage === 'failed') return `${name}: failed. ${job.error}`;
		if (job.retrying) return `${name}: retrying upload (${job.attempt}/3)…`;
		if (job.stage === 'uploading') return `Uploading ${name}`;
		if (job.slow) return `${name}: still converting. It will finish in the background.`;
		if (job.stage === 'converting') return `Converting ${name} to WebP`;
		return job.warning ? `${name}: ${job.warning}` : `${name} ready`;
	});

	const sizes = $derived.by(() => {
		const m = job.media;
		if (job.stage !== 'ready' || !m) return bytes(job.file.size);
		const kind = m.converted ? 'WebP' : 'original';
		return `${bytes(job.file.size)} → ${bytes(m.size_bytes)} ${kind}`;
	});

	$effect(() => {
		if (job.stage !== 'ready' || job.warning || !onfinished) return;
		const t = setTimeout(onfinished, reduced.current ? 400 : 1200);
		return () => clearTimeout(t);
	});
</script>

<div
	class="grid gap-2 rounded-lg border border-ink bg-paper px-3 py-2.5"
	transition:slide={{ duration: reduced.current ? 0 : 180 }}
>
	<div class="flex items-baseline justify-between gap-3">
		<p class="min-w-0 truncate text-sm font-medium text-ink" title={name}>{name}</p>
		<p class="shrink-0 font-mono text-xs text-ink-soft">{sizes}</p>
	</div>

	<ol
		class="flex flex-wrap items-center gap-x-1 font-mono text-[0.6875rem] tracking-[0.06em] uppercase"
	>
		{#each steps as s, i (s.n)}
			{@const active = s.stage === current}
			<li
				class={['rounded px-1.5 py-0.5', active ? `${tone} text-on-spot` : 'text-ink-soft']}
				aria-current={active ? 'step' : undefined}
			>
				{s.n}
				{s.label}
			</li>
			{#if i < steps.length - 1}<li class="text-ink-soft" aria-hidden="true">·</li>{/if}
		{/each}
	</ol>

	<div class="flex items-center gap-2">
		{#if job.stage === 'uploading'}
			<div
				class="bar"
				role="progressbar"
				aria-labelledby="{uid}-status"
				aria-valuemin={0}
				aria-valuemax={100}
				aria-valuenow={job.percent}
			>
				<span
					class={['fill', job.retrying ? 'bg-spot-butter' : 'bg-spot-sky']}
					style:width="{job.percent}%"
				></span>
			</div>
			<span class="w-10 text-right font-mono text-xs text-ink" aria-hidden="true">
				{job.percent}%
			</span>
		{:else if job.stage === 'converting'}
			<div class="bar" role="progressbar" aria-labelledby="{uid}-status">
				<span class={['fill bg-spot-butter', reduced.current || job.slow ? 'full' : 'printing']}
				></span>
			</div>
			<span class="w-10 text-right font-mono text-xs text-ink" aria-hidden="true">
				{Math.floor(job.elapsed / 1000)}s
			</span>
		{:else if job.stage === 'ready'}
			<div class="bar">
				<span class={['fill full', job.warning ? 'bg-spot-butter' : 'bg-spot-mint']}></span>
			</div>
			<svg class="check size-5 shrink-0 text-ink" viewBox="0 0 24 24" aria-hidden="true">
				<path d="M5 12.5l4.5 4.5L19 7.5" fill="none" stroke="currentColor" stroke-width="2.5" />
			</svg>
		{:else}
			<div class="bar"><span class="fill full bg-spot-rose"></span></div>
		{/if}
	</div>

	<p id="{uid}-status" class="sr-only" aria-live="polite">{status}</p>

	{#if job.retrying}
		<p class="text-sm text-ink-soft" aria-hidden="true">Retrying upload ({job.attempt}/3)…</p>
	{:else if job.slow}
		{@render notice('Still converting. It will finish in the background.', checkAgain)}
	{:else if job.stage === 'failed'}
		{@render notice(job.error, job.failKind === 'conversion' ? retryConversion : retryUpload)}
	{:else if job.stage === 'ready' && job.warning}
		{@render notice(job.warning, retryConversion)}
	{/if}
</div>

{#snippet checkAgain()}
	<button type="button" class="btn btn-secondary btn-sm" onclick={() => void job.checkAgain()}>
		<RefreshCw class="size-4" aria-hidden="true" /> Check again
	</button>
{/snippet}

{#snippet retryUpload()}
	<button type="button" class="btn btn-secondary btn-sm" onclick={() => void job.start()}>
		<RotateCw class="size-4" aria-hidden="true" /> Retry
	</button>
{/snippet}

{#snippet retryConversion()}
	<button type="button" class="btn btn-secondary btn-sm" onclick={() => void job.retryConversion()}>
		<RotateCw class="size-4" aria-hidden="true" />
		{job.stage === 'ready' ? 'Try converting again' : 'Retry conversion'}
	</button>
{/snippet}

{#snippet notice(text: string, action: Snippet)}
	<div class="flex flex-wrap items-center justify-between gap-2">
		<p class="min-w-0 text-sm text-ink" aria-hidden="true">{text}</p>
		<div class="flex flex-wrap gap-2">
			{#if onremove && !job.slow}
				<button type="button" class="btn btn-ghost btn-sm" onclick={onremove}>
					<X class="size-4" aria-hidden="true" />
					{job.stage === 'ready' ? 'Dismiss' : 'Remove'}
				</button>
			{/if}
			{@render action()}
		</div>
	</div>
{/snippet}

<style>
	.bar {
		position: relative;
		flex: 1;
		height: 0.75rem;
		overflow: hidden;
		border: 1.5px solid var(--ink);
		border-radius: 999px;
		background: var(--paper-2);
	}
	.fill {
		position: absolute;
		inset: 0 auto 0 0;
		border-right: 1.5px solid var(--ink);
		transition: width 180ms ease-out;
	}
	.fill.full {
		width: 100%;
		border-right: 0;
	}
	/* A flat spot block "printing" across the bar: no gradients. */
	.printing {
		width: 35%;
		border-left: 1.5px solid var(--ink);
		animation: print 1.1s ease-in-out infinite;
	}
	@keyframes print {
		from {
			transform: translateX(-100%);
		}
		to {
			transform: translateX(300%);
		}
	}
	.check path {
		stroke-dasharray: 24;
		stroke-dashoffset: 24;
		animation: draw 220ms ease-out forwards;
	}
	@keyframes draw {
		to {
			stroke-dashoffset: 0;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.fill {
			transition: none;
		}
		.check path {
			animation: none;
			stroke-dashoffset: 0;
		}
	}
</style>
