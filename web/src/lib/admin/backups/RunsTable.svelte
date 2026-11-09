<script lang="ts">
	import { History } from '@lucide/svelte';
	import type { BackupRun } from '#lib/api/types.js';
	import RelTime from '../components/RelTime.svelte';
	import EmptyState from '../components/EmptyState.svelte';
	import { bytes, num, timeSpan } from '../format';
	import { runKindShort, runStatusCopy, TONE_SPOT, utcStamp } from './copy.js';

	let { runs }: { runs: BackupRun[] } = $props();

	function when(r: BackupRun): string | null {
		return r.started_at ?? r.requested_at ?? r.finished_at;
	}
	function duration(r: BackupRun): string {
		if (!r.started_at || !r.finished_at) return '–';
		return timeSpan(new Date(r.finished_at).getTime() - new Date(r.started_at).getTime());
	}
	const size = (r: BackupRun) => (r.size_bytes == null ? '–' : bytes(r.size_bytes));
	/** ", 12.3 MB, took 41s, 18 tables" for the stacked list. */
	function details(r: BackupRun): string {
		const parts: string[] = [];
		if (r.size_bytes != null) parts.push(size(r));
		if (r.started_at && r.finished_at) parts.push(`took ${duration(r)}`);
		if (r.tables != null) parts.push(`${num(r.tables)} tables`);
		if (r.pruned) parts.push(`${num(r.pruned)} pruned`);
		return parts.map((p) => `, ${p}`).join('');
	}
	const problem = (r: BackupRun) => r.error ?? (r.prune_error ? `Cleanup: ${r.prune_error}` : null);
</script>

{#snippet chip(r: BackupRun)}
	{@const st = runStatusCopy(r.status)}
	<span class="tag gap-1.5 border border-on-spot" style:--tag={TONE_SPOT[st.tone]}>
		<span class="size-1.5 rounded-full bg-on-spot" aria-hidden="true"></span>
		{st.label}
	</span>
{/snippet}

<section class="card flex min-w-0 flex-col gap-3 p-4 sm:p-5" aria-labelledby="runs-h">
	<div>
		<h2 id="runs-h" class="eyebrow">Recent runs</h2>
		<p class="hint mt-1">
			The newest 20 backups. Times are shown relative; hover for the exact time.
		</p>
	</div>

	{#if runs.length === 0}
		<EmptyState
			icon={History}
			title="No backups yet"
			message="Runs show up here once the backup service starts one."
		/>
	{:else}
		<div class="hidden overflow-x-auto sm:block">
			<table class="data-table">
				<caption class="sr-only">Recent backup runs, newest first</caption>
				<thead>
					<tr>
						<th scope="col">When</th>
						<th scope="col">Kind</th>
						<th scope="col">Status</th>
						<th scope="col" class="num">Size</th>
						<th scope="col" class="num">Duration</th>
						<th scope="col" class="num">Tables</th>
						<th scope="col" class="num">Pruned</th>
						<th scope="col">Error</th>
					</tr>
				</thead>
				<tbody>
					{#each runs as r (r.id)}
						{@const err = problem(r)}
						<tr>
							<td class="font-mono text-sm whitespace-nowrap">
								<RelTime value={when(r)} />
								<span class="sr-only">({utcStamp(when(r))})</span>
							</td>
							<td class="text-sm whitespace-nowrap">{runKindShort(r.kind)}</td>
							<td class="whitespace-nowrap">{@render chip(r)}</td>
							<td class="num font-mono text-sm whitespace-nowrap tabular-nums">{size(r)}</td>
							<td class="num font-mono text-sm whitespace-nowrap tabular-nums">{duration(r)}</td>
							<td class="num font-mono text-sm whitespace-nowrap tabular-nums">{num(r.tables)}</td>
							<td class="num font-mono text-sm whitespace-nowrap tabular-nums">{num(r.pruned)}</td>
							<!-- Takes the leftover width and truncates; full text in the title. -->
							<td class="w-full max-w-0 min-w-[10rem]">
								{#if err}
									<span class="block truncate font-mono text-xs text-danger" title={err}>{err}</span
									>
								{:else}
									<span class="text-ink-soft">–</span>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>

		<ul
			class="flex flex-col divide-y divide-rule sm:hidden"
			aria-label="Recent backup runs, newest first"
		>
			{#each runs as r (r.id)}
				{@const err = problem(r)}
				<li class="flex flex-col gap-1.5 py-3">
					<div class="flex flex-wrap items-center justify-between gap-2">
						<span class="text-sm font-medium text-ink">{runKindShort(r.kind)}</span>
						{@render chip(r)}
					</div>
					<p class="font-mono text-xs text-ink-soft">
						<RelTime value={when(r)} />{details(r)}
					</p>
					{#if err}<p class="font-mono text-xs break-words text-danger">{err}</p>{/if}
				</li>
			{/each}
		</ul>
	{/if}
</section>
