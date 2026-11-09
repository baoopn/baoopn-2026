<script lang="ts">
	import { LoaderCircle, RotateCcw, Save } from '@lucide/svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import type { BackupStatus } from '#lib/api/types.js';
	import Switch from '../components/Switch.svelte';
	import ConfirmDialog from '../components/ConfirmDialog.svelte';
	import { ApiRequestError } from '../api';
	import { guardUnsaved } from '../guard.svelte';
	import { errorMessage } from '../format';
	import { toasts } from '../toast.svelte';
	import { FIELDS, MESSAGES, fieldFromMessage, formFromSettings, validateForm } from './rules.js';
	import { utcStamp } from './copy.js';
	import { pauseMutation, resetSettingsMutation, saveSettingsMutation } from './queries';

	type Field = 'backup_time' | 'keep_days' | 'keep_min' | 'max_age_hours';

	let { status }: { status: BackupStatus } = $props();

	const qc = useQueryClient();
	const saveM = createMutation(() => saveSettingsMutation(qc));
	const resetM = createMutation(() => resetSettingsMutation(qc));
	const pauseM = createMutation(() => pauseMutation(qc));

	const s = $derived(status.settings);
	/** The stored overrides as form text ("" = server default). */
	const server = $derived(formFromSettings(s));

	// Local edits on top of the server values, so a refetch (another tab, a
	// reset) updates every field the admin has not touched.
	let edits = $state<Partial<Record<Field, string>>>({});
	let errors = $state<Partial<Record<Field, string>>>({});
	let formError = $state<string | null>(null);

	const form = $derived({ ...server, ...edits });
	const dirty = $derived(FIELDS.some((k) => edits[k] !== undefined && edits[k] !== server[k]));
	guardUnsaved(() => dirty && !saveM.isPending);

	function edit(k: Field, value: string) {
		edits = { ...edits, [k]: value };
		errors = { ...errors, [k]: undefined };
	}

	const fields: {
		key: Field;
		id: string;
		label: string;
		unit: string;
		inputmode: 'numeric' | 'text';
	}[] = [
		{
			key: 'backup_time',
			id: 'b-time',
			label: 'Daily backup time (UTC)',
			unit: '',
			// Numeric keypads have no ":".
			inputmode: 'text'
		},
		{
			key: 'keep_days',
			id: 'b-keep-days',
			label: 'Keep backups for (days)',
			unit: ' days',
			inputmode: 'numeric'
		},
		{
			key: 'keep_min',
			id: 'b-keep-min',
			label: 'Keep at least (backups)',
			unit: ' backups',
			inputmode: 'numeric'
		},
		{
			key: 'max_age_hours',
			id: 'b-max-age',
			label: 'Alert after (hours)',
			unit: ' hours',
			inputmode: 'numeric'
		}
	];

	const hints: Record<Field, string> = {
		backup_time: '24-hour HH:MM.',
		keep_days: '0 keeps every backup.',
		keep_min: 'Never delete below this many.',
		max_age_hours: 'Health turns stale when no backup is this old. 25 to 720.'
	};

	function envText(k: Field, unit: string): string {
		const v = s[k].env;
		return v == null || v === '' ? 'Default: not reported yet' : `Default: ${v}${unit}`;
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (saveM.isPending) return;
		formError = null;
		const r = validateForm(form);
		errors = r.errors;
		const first = fields.find((f) => r.errors[f.key]);
		if (first) {
			document.getElementById(first.id)?.focus();
			return;
		}
		try {
			await saveM.mutateAsync(r.input);
			edits = {};
			toasts.success('Backup settings saved');
		} catch (err) {
			if (err instanceof ApiRequestError && err.status === 422) {
				const f = fieldFromMessage(err.message);
				if (f) {
					errors = { [f]: MESSAGES[f] };
					document.getElementById(fields.find((x) => x.key === f)?.id ?? '')?.focus();
				} else formError = err.message;
			} else toasts.error('Could not save the backup settings', errorMessage(err));
		}
	}

	let confirmReset = $state(false);
	const hasOverrides = $derived(fields.some((f) => s[f.key].source === 'override'));
	async function reset() {
		try {
			await resetM.mutateAsync();
			edits = {};
			errors = {};
			formError = null;
			toasts.success('Backup settings reset to the defaults');
		} catch (err) {
			toasts.error('Could not reset the backup settings', errorMessage(err));
		}
	}

	// Switch shows "scheduled backups on"; follows the server until changed locally.
	let scheduleOn = $derived(!s.paused);
	let confirmPause = $state(false);

	async function setPaused(paused: boolean) {
		try {
			await pauseM.mutateAsync(paused);
			toasts.success(paused ? 'Scheduled backups paused' : 'Scheduled backups resumed');
		} catch (err) {
			scheduleOn = !s.paused;
			toasts.error('Could not change the schedule', errorMessage(err));
		}
	}

	function onswitch(on: boolean) {
		if (on) {
			void setPaused(false);
		} else {
			// Ask first; keep the switch on until confirmed.
			scheduleOn = true;
			confirmPause = true;
		}
	}
</script>

<section class="card flex flex-col gap-5 p-4 sm:p-5" aria-labelledby="schedule-h">
	<div>
		<h2 id="schedule-h" class="eyebrow">Schedule and retention</h2>
		<p class="hint mt-1">
			Leave a field empty to use the backup service's default.
			{#if s.retry_minutes != null}A failed scheduled backup is retried after {s.retry_minutes} minutes.{/if}
		</p>
	</div>

	<div class="border-y border-rule py-2">
		<Switch
			bind:checked={scheduleOn}
			label="Run scheduled backups"
			disabled={pauseM.isPending}
			onchange={onswitch}
		/>
		<p class="hint">
			{#if s.paused}
				Paused{#if s.paused_at}&nbsp;since {utcStamp(s.paused_at)}{/if}. Back up now still works.
			{:else}
				Backups run every day at the time below.
			{/if}
		</p>
	</div>

	<form class="flex flex-col gap-5" onsubmit={save} novalidate aria-labelledby="schedule-h">
		<div class="grid gap-5 sm:grid-cols-2">
			{#each fields as f (f.key)}
				{@const err = errors[f.key]}
				<div>
					<div class="flex items-center justify-between gap-2">
						<label class="field-label" for={f.id}>{f.label}</label>
						{#if s[f.key].source === 'override'}
							<span class="tag border border-on-spot" style:--tag="var(--spot-lilac)">Custom</span>
						{/if}
					</div>
					<input
						id={f.id}
						class="input font-mono"
						type="text"
						inputmode={f.inputmode}
						autocomplete="off"
						placeholder={s[f.key].env == null ? '' : String(s[f.key].env)}
						value={form[f.key]}
						oninput={(e) => edit(f.key, e.currentTarget.value)}
						aria-invalid={!!err}
						aria-describedby="{f.id}-msg"
					/>
					{#if err}
						<p id="{f.id}-msg" class="field-error" role="alert">{err}</p>
					{:else}
						<p id="{f.id}-msg" class="hint">{envText(f.key, f.unit)}. {hints[f.key]}</p>
					{/if}
				</div>
			{/each}
		</div>

		{#if formError}<p class="field-error" role="alert">{formError}</p>{/if}

		<div class="flex flex-wrap items-center gap-3">
			<button type="submit" class="btn btn-primary" disabled={!dirty || saveM.isPending}>
				{#if saveM.isPending}<LoaderCircle
						class="size-4 animate-spin"
						aria-hidden="true"
					/>{:else}<Save class="size-4" aria-hidden="true" />{/if}
				Save
			</button>
			<button
				type="button"
				class="btn btn-secondary"
				onclick={() => (confirmReset = true)}
				disabled={!hasOverrides || resetM.isPending}
			>
				<RotateCcw class="size-4" aria-hidden="true" />
				Reset to defaults
			</button>
			<p class="font-mono text-xs" role="status">
				{#if dirty}<span class="text-warn">Unsaved changes</span>{:else if s.pending}<span
						class="text-ink-soft">Saved; the backup service applies it within a minute.</span
					>{/if}
			</p>
		</div>
	</form>
</section>

<ConfirmDialog
	bind:open={confirmReset}
	title="Reset to defaults?"
	message="The backup time, retention and alert settings go back to the backup service's defaults. Pausing is not changed."
	confirmLabel="Reset"
	onconfirm={reset}
/>

<ConfirmDialog
	bind:open={confirmPause}
	title="Pause scheduled backups?"
	message="No daily backups run until you turn this back on. Back up now still works, and the status shows paused instead of an alert."
	confirmLabel="Pause"
	onconfirm={async () => {
		scheduleOn = false;
		await setPaused(true);
	}}
/>
