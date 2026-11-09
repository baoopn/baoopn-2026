<script lang="ts">
	import { untrack } from 'svelte';
	import { createMutation, createQuery, useQueryClient } from '@tanstack/svelte-query';
	import type { BackupRun } from '#lib/api/types.js';
	import PageHeader from '#lib/admin/components/PageHeader.svelte';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import StatusCard from '#lib/admin/backups/StatusCard.svelte';
	import RunsTable from '#lib/admin/backups/RunsTable.svelte';
	import ScheduleForm from '#lib/admin/backups/ScheduleForm.svelte';
	import { backupsQuery, runNowMutation } from '#lib/admin/backups/queries.js';
	import { finishedRun } from '#lib/admin/backups/copy.js';
	import { ApiRequestError } from '#lib/admin/api.js';
	import { bytes, errorMessage } from '#lib/admin/format.js';
	import { toasts } from '#lib/admin/toast.svelte.js';

	const qc = useQueryClient();
	const status = createQuery(() => backupsQuery());
	const runM = createMutation(() => runNowMutation(qc));

	async function runNow() {
		try {
			await runM.mutateAsync();
			toasts.info('Backup queued', 'The backup service starts it within 30 seconds.');
		} catch (err) {
			const busy = err instanceof ApiRequestError && err.code === 'busy';
			toasts.error(
				busy ? 'A backup is already running' : 'Could not start a backup',
				errorMessage(err)
			);
		}
	}

	// Toast the result when the active run disappears (finished, failed or
	// expired). The effect only reads the query; the toast store is untracked.
	let watched: BackupRun | null = null;
	$effect(() => {
		const d = status.data;
		if (!d) return;
		const done = finishedRun(watched, d);
		watched = d.active_run;
		if (done) untrack(() => announce(done));
	});

	function announce(run: BackupRun) {
		if (run.status === 'ok') {
			const size = run.size_bytes == null ? '' : ` (${bytes(run.size_bytes)})`;
			toasts.success(`Backup finished${size}`);
		} else {
			toasts.error('Backup failed', run.error ?? undefined);
		}
	}
</script>

<PageHeader
	title="Backups"
	eyebrow="Site"
	description="Encrypted off-site database backups: status, schedule and recent runs."
/>

{#if status.data}
	<div class="flex flex-col gap-6">
		<StatusCard
			status={status.data}
			starting={runM.isPending}
			refreshing={status.isFetching}
			onrun={runNow}
			onrefresh={() => status.refetch()}
		/>
		<div class="grid gap-6 xl:grid-cols-[minmax(0,1fr)_minmax(0,26rem)]">
			<RunsTable runs={status.data.runs} />
			<ScheduleForm status={status.data} />
		</div>
	</div>
{:else if status.isError}
	<ErrorState message={errorMessage(status.error)} onretry={() => status.refetch()} />
{:else}
	<div class="flex flex-col gap-4" aria-busy="true">
		<div class="skeleton h-56"></div>
		<div class="skeleton h-40"></div>
		<span class="sr-only">Loading the backup status</span>
	</div>
{/if}
