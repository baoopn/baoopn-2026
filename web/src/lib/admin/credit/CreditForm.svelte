<script lang="ts">
	import { tick } from 'svelte';
	import { LoaderCircle, Save } from '@lucide/svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import type { AdminCredit, CreditInput, CreditLogoKind } from '#lib/api/types.js';
	import Segmented from '../components/Segmented.svelte';
	import { ApiRequestError } from '../api';
	import { guardUnsaved } from '../guard.svelte';
	import { errorMessage } from '../format';
	import { toasts } from '../toast.svelte';
	import OriginsEditor from './OriginsEditor.svelte';
	import { MESSAGES, fieldFromMessage, validateForm } from './rules.js';
	import { saveCreditMutation } from './queries';

	let {
		credit,
		form,
		edits = $bindable()
	}: {
		credit: AdminCredit;
		/** Saved values with the local edits on top. */
		form: CreditInput;
		edits: Partial<CreditInput>;
	} = $props();

	const qc = useQueryClient();
	const saveM = createMutation(() => saveCreditMutation(qc));

	let errors = $state<Record<string, string>>({});
	let formError = $state<string | null>(null);

	const dirty = $derived(
		form.text !== credit.text ||
			form.url !== credit.url ||
			form.logo !== credit.logo ||
			form.origins.join('\n') !== credit.origins.join('\n')
	);
	guardUnsaved(() => dirty && !saveM.isPending);

	const textLength = $derived([...form.text.trim()].length);
	const rowErrors = $derived(
		Object.fromEntries(Object.entries(errors).filter(([k]) => k.startsWith('origins[')))
	);

	const logoOptions: { value: CreditLogoKind; label: string }[] = [
		{ value: 'wordmark', label: 'Wordmark' },
		{ value: 'mark', label: 'Mark' }
	];

	function edit<K extends keyof CreditInput>(k: K, value: CreditInput[K]) {
		edits = { ...edits, [k]: value };
		const next = { ...errors };
		for (const key of Object.keys(next)) {
			// Row indexes shift when the list changes, so drop every origin error.
			if (key === k || (k === 'origins' && key.startsWith('origins'))) delete next[key];
		}
		errors = next;
	}

	/** Field errors from the API are about these inputs. */
	const ids: Record<string, string> = {
		text: 'credit-text',
		url: 'credit-url',
		origins: 'credit-origin-new'
	};

	function focusField(key: string) {
		if (key === 'logo') {
			document.querySelector<HTMLElement>('#credit-logo-group [aria-checked="true"]')?.focus();
			return;
		}
		document.getElementById(ids[key.startsWith('origins') ? 'origins' : key])?.focus();
	}

	function discard() {
		edits = {};
		errors = {};
		formError = null;
	}

	/** Plain-English copy for a field the API rejected. */
	function messageFor(key: string, apiMessage: string): string {
		if (key.startsWith('origins[')) return MESSAGES.origin;
		if (key === 'text' || key === 'url' || key === 'logo' || key === 'origins') return MESSAGES[key];
		return apiMessage;
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (saveM.isPending) return;
		formError = null;
		const r = validateForm(form);
		errors = r.errors;
		const first = Object.keys(r.errors)[0];
		if (first) {
			focusField(first);
			return;
		}
		try {
			await saveM.mutateAsync(r.input);
			edits = {};
			toasts.success('Credit saved', 'Client sites show it within 5 minutes.');
		} catch (err) {
			if (err instanceof ApiRequestError && err.status === 422) {
				const f = fieldFromMessage(err.message);
				if (f) {
					errors = { [f]: messageFor(f, err.message) };
					// Wait for the fieldset to re-enable, or focus() is ignored.
					await tick();
					focusField(f);
				} else formError = err.message;
			} else toasts.error('Could not save the credit', errorMessage(err));
		}
	}
</script>

<section class="card flex min-w-0 flex-col gap-5 p-4 sm:p-5" aria-labelledby="credit-form-h">
	<div>
		<h2 id="credit-form-h" class="eyebrow">Credit line</h2>
		<p class="hint mt-1">Client sites show this text and logo, linked to the URL below.</p>
	</div>

	<form class="flex min-w-0 flex-col gap-5" onsubmit={save} novalidate aria-labelledby="credit-form-h">
		<!-- Locked while saving, so no edit typed mid-request is lost when the save clears the edits. -->
		<fieldset class="flex min-w-0 flex-col gap-5" disabled={saveM.isPending}>
			<div>
				<div class="flex items-center justify-between gap-2">
					<label class="field-label" for="credit-text">Text</label>
					<span
						class={[
							'font-mono text-xs',
							textLength > credit.limits.text_max ? 'text-danger' : 'text-ink-soft'
						]}
					>
						{textLength} / {credit.limits.text_max}
					</span>
				</div>
				<input
					id="credit-text"
					class="input"
					type="text"
					autocomplete="off"
					value={form.text}
					oninput={(e) => edit('text', e.currentTarget.value)}
					aria-invalid={!!errors.text}
					aria-describedby="credit-text-msg"
				/>
				{#if errors.text}
					<p id="credit-text-msg" class="field-error" role="alert">{errors.text}</p>
				{:else}
					<p id="credit-text-msg" class="hint">Appears before the logo, for example "Designed &amp; built by".</p>
				{/if}
			</div>

			<div>
				<label class="field-label" for="credit-url">Link</label>
				<input
					id="credit-url"
					class="input font-mono"
					type="url"
					inputmode="url"
					autocomplete="off"
					spellcheck="false"
					maxlength={credit.limits.url_max}
					value={form.url}
					oninput={(e) => edit('url', e.currentTarget.value)}
					aria-invalid={!!errors.url}
					aria-describedby="credit-url-msg"
				/>
				{#if errors.url}
					<p id="credit-url-msg" class="field-error" role="alert">{errors.url}</p>
				{:else}
					<p id="credit-url-msg" class="hint">The logo links here. It must start with https://.</p>
				{/if}
			</div>

			<div>
				<p class="field-label">Logo</p>
				<div id="credit-logo-group">
					<Segmented
						options={logoOptions}
						value={form.logo}
						label="Logo"
						onchange={(v) => edit('logo', v)}
					/>
				</div>
				{#if errors.logo}
					<p class="field-error" role="alert">{errors.logo}</p>
				{:else}
					<p class="hint">The wordmark spells out the name. The mark is the small badge.</p>
				{/if}
			</div>

			<OriginsEditor
				origins={form.origins}
				max={credit.limits.origins_max}
				errors={rowErrors}
				listError={errors.origins}
				onchange={(list) => edit('origins', list)}
			/>

			{#if formError}<p class="field-error" role="alert">{formError}</p>{/if}

			<div class="flex flex-wrap items-center gap-3">
				<button type="submit" class="btn btn-primary" disabled={!dirty || saveM.isPending}>
					{#if saveM.isPending}<LoaderCircle
							class="size-4 animate-spin"
							aria-hidden="true"
						/>{:else}<Save class="size-4" aria-hidden="true" />{/if}
					Save
				</button>
				{#if dirty}
					<button type="button" class="btn btn-secondary" onclick={discard}>
						Discard changes
					</button>
				{/if}
				<p class="font-mono text-xs" role="status">
					{#if dirty}<span class="text-warn">Unsaved changes</span>{/if}
				</p>
			</div>
		</fieldset>
	</form>
</section>
