<script lang="ts" module>
	export type BarRow = {
		key: string;
		label: string;
		/** Small mono prefix, e.g. a flag. */
		prefix?: string;
		href?: string;
		views: number;
		visitors?: number;
	};
</script>

<script lang="ts">
	import { num } from '../format';

	let {
		rows,
		caption,
		keyLabel,
		empty = 'No data for this range yet.'
	}: {
		rows: BarRow[];
		caption: string;
		keyLabel: string;
		empty?: string;
	} = $props();

	const max = $derived(Math.max(0, ...rows.map((r) => r.views)));
	const hasVisitors = $derived(rows.some((r) => r.visitors !== undefined));
</script>

{#if rows.length === 0}
	<p class="py-6 text-sm text-ink-soft">{empty}</p>
{:else}
	<table class="w-full text-sm">
		<caption class="sr-only">{caption}</caption>
		<thead>
			<tr class="border-b border-ink">
				<th scope="col" class="eyebrow py-2 text-left font-medium">{keyLabel}</th>
				<th scope="col" class="eyebrow w-16 py-2 text-right font-medium">Views</th>
				{#if hasVisitors}
					<th scope="col" class="eyebrow w-20 py-2 text-right font-medium">Visitors</th>
				{/if}
			</tr>
		</thead>
		<tbody>
			{#each rows as r (r.key)}
				{@const ratio = max > 0 ? r.views / max : 0}
				<tr class="border-b border-rule last:border-0">
					<td class="max-w-0 py-2 pr-3">
						<span class="flex min-w-0 items-center gap-2">
							{#if r.prefix}<span aria-hidden="true">{r.prefix}</span>{/if}
							{#if r.href}
								<a
									href={r.href}
									data-sveltekit-reload
									class="link truncate"
									target="_blank"
									rel="noopener"
									title={r.label}>{r.label}</a
								>
							{:else}
								<span class="truncate" title={r.label}>{r.label}</span>
							{/if}
						</span>
						<!-- Inline bar: flat spot fill from an ink baseline, 2px ink cap at the data end. -->
						<span class="mt-1 block h-2 border-l border-ink" aria-hidden="true">
							<span
								class="block h-full rounded-r-[4px] border-r-2 border-ink"
								style:width="{Math.max(ratio * 100, r.views > 0 ? 1 : 0)}%"
								style:background="var(--spot-chart)"
							></span>
						</span>
					</td>
					<td class="py-2.5 text-right font-mono tabular-nums">{num(r.views)}</td>
					{#if hasVisitors}
						<td class="py-2.5 text-right font-mono text-ink-soft tabular-nums">{num(r.visitors)}</td
						>
					{/if}
				</tr>
			{/each}
		</tbody>
	</table>
{/if}
