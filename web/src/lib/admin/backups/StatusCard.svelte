<script lang="ts">
	import { CircleCheck, CircleSlash, DatabaseBackup, LoaderCircle } from '@lucide/svelte';
	import type { BackupStatus } from '#lib/api/types.js';
	import RelTime from '../components/RelTime.svelte';
	import { healthCopy, runBlockedReason, TONE_SPOT, utcStamp } from './copy.js';

	let {
		status,
		starting = false,
		onrun
	}: {
		status: BackupStatus;
		/** The run request is in flight. */
		starting?: boolean;
		onrun: () => void;
	} = $props();

	const s = $derived(status.settings);
	const copy = $derived(
		healthCopy(status.health, {
			hours: s.max_age_hours.value,
			since: utcStamp(status.agent?.last_seen_at),
			idleReason: status.agent?.idle_reason
		})
	);
	const active = $derived(status.active_run);
	const blocked = $derived(runBlockedReason(status.health, active));
	const lastError = $derived(status.last_run?.status === 'failed' ? status.last_run : null);
	const pruneError = $derived(status.last_run?.prune_error ?? null);

	const localTime = new Intl.DateTimeFormat(undefined, {
		weekday: 'short',
		hour: 'numeric',
		minute: '2-digit'
	});
	function local(iso: string): string {
		const d = new Date(iso);
		return Number.isNaN(d.getTime()) ? '' : localTime.format(d);
	}

	const dest = $derived.by(() => {
		const d = status.destination;
		if (d.kind === 's3') return d.s3_provider ? `S3 (${d.s3_provider})` : 'S3';
		if (d.kind === 'rclone') return 'rclone remote';
		return null;
	});

	// Only set / not set, never values. S3 rows are hidden for an rclone remote.
	const secrets = $derived.by(() => {
		const x = status.secrets;
		const s3 = status.destination.kind !== 'rclone';
		return [
			{ label: 'Encryption key (age recipient)', set: x.age_recipient, show: true },
			{ label: 'S3 endpoint', set: x.s3_endpoint, show: s3 },
			{ label: 'S3 access key ID', set: x.s3_access_key_id, show: s3 },
			{ label: 'S3 secret access key', set: x.s3_secret_access_key, show: s3 },
			{ label: 'Health check ping URL', set: x.ping_url, show: true }
		].filter((r) => r.show);
	});
</script>

<section class="card flex flex-col gap-5 p-4 sm:p-5" aria-labelledby="backup-status-h">
	<div class="flex flex-wrap items-start justify-between gap-4">
		<div class="flex min-w-0 flex-col gap-2" aria-live="polite">
			<h2 id="backup-status-h" class="eyebrow">Status</h2>
			<span class="tag w-fit gap-1.5 border border-on-spot" style:--tag={TONE_SPOT[copy.tone]}>
				<span class="size-1.5 rounded-full bg-on-spot" aria-hidden="true"></span>
				{copy.label}
			</span>
			<p class="font-display text-xl font-semibold text-ink">{copy.title}</p>
			<p class="text-sm text-ink-soft">{copy.explanation}</p>
			{#if active}
				<p class="inline-flex items-center gap-2 font-mono text-sm text-ink">
					<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />
					{active.status === 'queued' ? 'Queued…' : 'Running…'}
				</p>
			{/if}
		</div>
		<div class="flex w-full flex-col gap-1 sm:w-auto sm:items-end">
			<button
				type="button"
				class="btn btn-primary w-full sm:w-auto"
				onclick={onrun}
				disabled={starting || !!blocked}
				aria-describedby={blocked ? 'run-blocked' : undefined}
			>
				{#if starting || active}
					<LoaderCircle class="size-4 animate-spin" aria-hidden="true" />
				{:else}
					<DatabaseBackup class="size-4" aria-hidden="true" />
				{/if}
				{#if active?.status === 'queued'}Queued…{:else if active}Running…{:else}Back up now{/if}
			</button>
			{#if blocked && !active}
				<p id="run-blocked" class="hint">{blocked}</p>
			{:else if blocked}
				<span id="run-blocked" class="sr-only">{blocked}</span>
			{/if}
		</div>
	</div>

	<dl class="grid gap-x-6 gap-y-4 text-sm sm:grid-cols-2">
		<div>
			<dt class="eyebrow mb-1">Last success</dt>
			<dd>
				{#if status.last_success_at}
					<RelTime value={status.last_success_at} class="text-ink" />
					<span class="block font-mono text-xs text-ink-soft"
						>{utcStamp(status.last_success_at)}</span
					>
				{:else}
					<span class="text-ink-soft">No backup yet</span>
				{/if}
			</dd>
		</div>
		<div>
			<dt class="eyebrow mb-1">Next scheduled run</dt>
			<dd>
				{#if status.agent?.next_run_at}
					<span class="font-mono text-ink">{utcStamp(status.agent.next_run_at)}</span>
					<span class="block text-xs text-ink-soft"
						>{local(status.agent.next_run_at)} your time</span
					>
				{:else if s.paused}
					<span class="text-ink-soft">Paused</span>
				{:else}
					<span class="text-ink-soft">Not scheduled</span>
				{/if}
			</dd>
		</div>
		{#if lastError}
			<div class="sm:col-span-2">
				<dt class="eyebrow mb-1">Last error</dt>
				<dd class="font-mono text-xs break-words text-danger">
					{lastError.error || 'The backup failed without a message.'}
					<span class="block text-ink-soft">{utcStamp(lastError.finished_at)}</span>
				</dd>
			</div>
		{/if}
		{#if pruneError}
			<div class="sm:col-span-2">
				<dt class="eyebrow mb-1">Cleanup error</dt>
				<dd class="font-mono text-xs break-words text-warn">{pruneError}</dd>
			</div>
		{/if}
		<div>
			<dt class="eyebrow mb-1">Destination</dt>
			<dd>
				{#if dest}
					<span class="text-ink">{dest}</span>
					{#if status.destination.s3_bucket}
						<span class="block font-mono text-xs break-all text-ink-soft"
							>Bucket {status.destination.s3_bucket}</span
						>
					{/if}
					{#if status.destination.prefix}
						<span class="block font-mono text-xs break-all text-ink-soft"
							>Prefix {status.destination.prefix}</span
						>
					{/if}
				{:else}
					<span class="text-ink-soft">Not reported</span>
				{/if}
			</dd>
		</div>
		<div>
			<dt class="eyebrow mb-1">Secrets</dt>
			<dd>
				<ul class="flex flex-col gap-1.5">
					{#each secrets as row (row.label)}
						<li class="flex items-start gap-2">
							{#if row.set}
								<CircleCheck class="text-ok mt-0.5 size-4 shrink-0" aria-hidden="true" />
							{:else}
								<CircleSlash class="mt-0.5 size-4 shrink-0 text-ink-soft" aria-hidden="true" />
							{/if}
							<span
								>{row.label}: <span class="text-ink-soft">{row.set ? 'set' : 'not set'}</span></span
							>
						</li>
					{/each}
				</ul>
			</dd>
		</div>
	</dl>
	{#if status.agent}
		<p class="font-mono text-xs text-ink-soft">
			Backup service {status.agent.version || 'unknown version'}, last check-in
			<RelTime value={status.agent.last_seen_at} />
		</p>
	{/if}
</section>
