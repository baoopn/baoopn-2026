<script lang="ts" module>
	export type Kpi = {
		label: string;
		value: string;
		sub?: string;
		/** Live dot before the value: true = active (pulse), false = idle. */
		live?: boolean;
		/** Change versus the previous period. */
		delta?: { text: string; dir: 'up' | 'down' | 'flat' } | null;
		href?: string;
	};
</script>

<script lang="ts">
	import { ArrowDownRight, ArrowRight, ArrowUpRight } from '@lucide/svelte';

	let {
		items,
		loading = false,
		label
	}: {
		items: Kpi[];
		loading?: boolean;
		/** Accessible name of the group. */
		label: string;
	} = $props();
</script>

<!-- Liner-note credits: large numbers on paper, separated by hairline rules. -->
<dl aria-label={label} aria-busy={loading} class="kpis border-y border-rule">
	{#each items as k (k.label)}
		<div class="kpi flex flex-col gap-1 px-4 py-5">
			<dt class="eyebrow">
				{#if k.href}<a href={k.href} class="link">{k.label}</a>{:else}{k.label}{/if}
			</dt>
			<dd
				class="order-first font-display text-3xl leading-tight font-semibold tracking-tight text-ink"
			>
				{#if loading}
					<span class="skeleton block h-8 w-20" aria-hidden="true"></span>
					<span class="sr-only">Loading</span>
				{:else}
					<span class="inline-flex items-center gap-2.5">
						{#if k.live !== undefined}
							<span class={['live-dot', k.live && 'on']} aria-hidden="true"></span>
						{/if}
						{k.value}
					</span>
				{/if}
			</dd>
			{#if !loading && (k.sub || k.delta)}
				<dd class="flex flex-wrap items-center gap-2 font-mono text-xs text-ink-soft">
					{#if k.delta}
						{@const Icon =
							k.delta.dir === 'up'
								? ArrowUpRight
								: k.delta.dir === 'down'
									? ArrowDownRight
									: ArrowRight}
						<span
							class={[
								'inline-flex items-center gap-0.5 font-semibold',
								k.delta.dir === 'up' && 'text-ok',
								k.delta.dir === 'down' && 'text-danger'
							]}
						>
							<Icon class="size-3.5" aria-hidden="true" />
							{k.delta.text}
						</span>
					{/if}
					{#if k.sub}<span>{k.sub}</span>{/if}
				</dd>
			{/if}
		</div>
	{/each}
</dl>

<style>
	.kpis {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
	}
	/* Hairline between neighbours; each tile carries its own left rule and the grid clips the first. */
	.kpis {
		overflow: hidden;
	}
	.kpi {
		border-left: 1px solid var(--rule);
		border-top: 1px solid var(--rule);
		margin-left: -1px;
		margin-top: -1px;
	}
	.live-dot {
		width: 0.625rem;
		height: 0.625rem;
		flex: none;
		border-radius: 999px;
		border: 1px solid var(--ink);
		background: var(--paper-4);
	}
	.live-dot.on {
		background: var(--spot-mint);
		animation: admin-pulse 1.6s ease-in-out infinite;
	}
</style>
