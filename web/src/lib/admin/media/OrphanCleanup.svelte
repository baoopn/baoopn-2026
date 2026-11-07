<script lang="ts">
	import { LoaderCircle, RefreshCw, Trash2 } from '@lucide/svelte';
	import type { OrphanCandidate, OrphanKind, OrphanPlan, OrphanResult } from '#lib/api/types.js';
	import ConfirmDialog from '#lib/admin/components/ConfirmDialog.svelte';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import RelTime from '#lib/admin/components/RelTime.svelte';
	import Switch from '#lib/admin/components/Switch.svelte';
	import { get, post, put } from '#lib/admin/api.js';
	import { draftMediaStems } from '#lib/admin/drafts.js';
	import { bytes, dateTime, errorMessage, num } from '#lib/admin/format.js';
	import { toasts } from '#lib/admin/toast.svelte.js';

	let plan = $state<OrphanPlan | null>(null);
	let loadError = $state<string | null>(null);
	let loading = $state(false);
	let draftStems = $state<Set<string>>(new Set());
	// Local copies are the last local backup: deleted only when ticked.
	let copies = $state<Record<string, boolean>>({});
	let confirmOpen = $state(false);
	let savingAuto = $state(false);

	const kindLabel: Record<OrphanKind, string> = {
		row: 'Media',
		object: 'Bucket file',
		local: 'Disk file',
		local_copy: 'Local copy',
		original: 'Original',
		temp: 'Temp file'
	};
	const kindSpot: Record<OrphanKind, string> = {
		row: 'var(--spot-peach)',
		object: 'var(--spot-sky)',
		local: 'var(--spot-lilac)',
		local_copy: 'var(--spot-mint)',
		original: 'var(--spot-butter)',
		temp: 'var(--spot-rose)'
	};

	async function load() {
		loading = true;
		loadError = null;
		try {
			plan = await get<OrphanPlan>('/media/orphans');
			draftStems = draftMediaStems();
		} catch (err) {
			loadError = errorMessage(err);
		} finally {
			loading = false;
		}
	}
	void load();

	function stemOf(c: OrphanCandidate): string {
		return c.kind === 'original' ? c.name : c.kind === 'temp' ? '' : c.name.slice(0, 24);
	}
	const inDraft = (c: OrphanCandidate) => {
		const st = stemOf(c);
		return st !== '' && draftStems.has(st);
	};
	const selected = (c: OrphanCandidate) =>
		c.eligible && !inDraft(c) && (c.kind !== 'local_copy' || !!copies[c.key]);

	const chosen = $derived(plan ? plan.candidates.filter(selected) : []);
	const chosenBytes = $derived(chosen.reduce((n, c) => n + c.size_bytes, 0));
	const waiting = $derived(plan ? plan.candidates.filter((c) => c.blocked === 'grace').length : 0);
	const graceDays = $derived(plan ? Math.round(plan.grace_seconds / 86400) : 0);

	async function clean() {
		// Never an empty list: the API reads that as "the whole plan".
		const keys = chosen.map((c) => c.key);
		if (keys.length === 0) return;
		try {
			const r = await post<OrphanResult>('/media/orphans', { keys });
			const d = r.deleted;
			const total = d.rows + d.objects + d.local + d.local_copies + d.originals + d.temp;
			const parts = [`${num(total)} removed (${bytes(r.bytes)})`];
			if (r.skipped.length) parts.push(`${num(r.skipped.length)} kept because they changed`);
			if (r.errors) parts.push(`${num(r.errors)} failed (see the API log)`);
			if (r.capped) parts.push(`${num(r.remaining)} left for the next run`);
			(r.errors ? toasts.error : toasts.success)('Cleanup done', parts.join(', ') + '.');
		} catch (err) {
			toasts.error('Cleanup failed', errorMessage(err));
		}
		await load();
	}

	async function setAuto(enabled: boolean) {
		if (!plan) return;
		savingAuto = true;
		try {
			plan = await put<OrphanPlan>('/media/orphans/auto', { enabled });
			toasts.success(enabled ? 'Daily cleanup on' : 'Daily cleanup off');
		} catch (err) {
			toasts.error('Could not change the daily cleanup', errorMessage(err));
			await load();
		} finally {
			savingAuto = false;
		}
	}

	function status(c: OrphanCandidate): string {
		if (c.blocked === 'disabled') return 'Cleanup off';
		if (c.blocked === 'grace') return 'Waits until ' + dateTime(c.eligible_at);
		if (inDraft(c)) return 'In a local draft';
		if (c.kind === 'local_copy') return 'Tick to remove';
		return 'Can be removed';
	}
</script>

<section class="card flex flex-col gap-4 p-4" aria-labelledby="orphans-h">
	<div class="flex flex-wrap items-start justify-between gap-3">
		<div>
			<h2 id="orphans-h" class="eyebrow">Media storage</h2>
			<p class="hint mt-1">
				Files no post, project or setting uses. They are removed only after {graceDays || 7} days
				unused.
			</p>
		</div>
		<button
			type="button"
			class="btn btn-ghost btn-sm"
			onclick={load}
			disabled={loading}
			aria-label="Reload the cleanup preview"
		>
			<RefreshCw class="size-4 {loading ? 'animate-spin' : ''}" aria-hidden="true" />
		</button>
	</div>

	{#if loadError}
		<ErrorState message={loadError} onretry={load} />
	{:else if !plan}
		<div class="flex flex-col gap-2" aria-busy="true">
			<div class="skeleton h-6"></div>
			<div class="skeleton h-24"></div>
			<span class="sr-only">Loading the cleanup preview</span>
		</div>
	{:else}
		{#if !plan.enabled}
			<p class="text-warn text-sm" role="status">Cleanup is off (MEDIA_ORPHAN_GRACE=0).</p>
		{/if}
		{#if plan.truncated}
			<p class="text-warn text-sm" role="status">
				The bucket has more than 10,000 files, so bucket files are not cleaned up.
			</p>
		{/if}
		<p class="text-sm">
			<span class="font-medium">{num(chosen.length)} files, {bytes(chosenBytes)}</span> can be removed;
			{num(waiting)} waiting for the {graceDays || 7}-day grace.
			<span class="text-ink-soft">
				{num(plan.kept.referenced)} in use, {num(plan.kept.processing)} processing, {num(
					plan.kept.foreign
				)} other files left alone.
			</span>
		</p>

		{#if plan.candidates.length}
			<div class="overflow-x-auto">
				<table class="data-table">
					<caption class="sr-only">Unused files</caption>
					<thead>
						<tr>
							<th scope="col">Kind</th>
							<th scope="col">File</th>
							<th scope="col" class="num">Size</th>
							<th scope="col">Last used</th>
							<th scope="col">Status</th>
						</tr>
					</thead>
					<tbody>
						{#each plan.candidates as c (c.key)}
							<tr>
								<td>
									<span class="tag border border-on-spot" style:--tag={kindSpot[c.kind]}
										>{kindLabel[c.kind]}</span
									>
								</td>
								<td class="max-w-[22rem]">
									{#if c.url}
										<a href={c.url} class="link font-mono text-xs break-all" target="_blank" rel="noopener"
											>{c.name}</a
										>
									{:else}
										<span class="font-mono text-xs break-all">{c.name}</span>
									{/if}
									<p class="text-xs text-ink-soft">{c.original ? c.original + ' · ' : ''}{c.reason}</p>
								</td>
								<td class="num font-mono text-sm tabular-nums">{bytes(c.size_bytes)}</td>
								<td class="font-mono text-sm text-ink-soft"><RelTime value={c.modified_at} /></td>
								<td class="text-sm">
									{#if c.kind === 'local_copy' && c.eligible && !inDraft(c)}
										<label class="flex items-center gap-2">
											<input type="checkbox" bind:checked={copies[c.key]} />
											<span>{copies[c.key] ? 'Will be removed' : 'Tick to remove'}</span>
										</label>
									{:else}
										<span class={selected(c) ? '' : 'text-ink-soft'}>{status(c)}</span>
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{:else}
			<p class="text-sm text-ink-soft">Nothing to clean up.</p>
		{/if}

		{#if plan.missing.length}
			<details class="text-sm">
				<summary>{num(plan.missing.length)} broken media links</summary>
				<ul class="mt-2 font-mono text-xs">
					{#each plan.missing as m (m.stem)}
						<li>{m.stem} <span class="text-ink-soft">in {m.where}</span></li>
					{/each}
				</ul>
			</details>
		{/if}

		<div class="flex flex-wrap items-center justify-between gap-3 border-t border-rule pt-3">
			<Switch
				checked={plan.auto}
				label="Daily cleanup"
				disabled={!plan.enabled || savingAuto}
				onchange={setAuto}
			/>
			<button
				type="button"
				class="btn btn-secondary"
				onclick={() => (confirmOpen = true)}
				disabled={!plan.enabled || chosen.length === 0}
			>
				{#if loading}<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />{:else}<Trash2
						class="size-4"
						aria-hidden="true"
					/>{/if}
				Clean up now
			</button>
		</div>
		<p class="hint">
			{#if plan.last_run}
				Last run <RelTime value={plan.last_run.at} /> ({plan.last_run.trigger}): {num(
					plan.last_run.deleted
				)} removed, {bytes(plan.last_run.bytes)}{plan.last_run.errors
					? `, ${num(plan.last_run.errors)} failed`
					: ''}.
			{:else}
				No cleanup has run yet.
			{/if}
			Files in this browser's unsaved drafts are kept.
		</p>
	{/if}
</section>

<ConfirmDialog
	bind:open={confirmOpen}
	title="Remove unused files?"
	message={`${num(chosen.length)} files (${bytes(chosenBytes)}) will be deleted for good. Bucket deletes cannot be undone.`}
	confirmLabel="Remove files"
	danger
	onconfirm={clean}
/>
