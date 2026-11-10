<script module lang="ts">
	import type { CreditInput, CreditLogoKind, CreditVariant } from '#lib/api/types.js';

	/** Unsaved changes; each variant field is kept on its own. */
	export type CreditEdits = Partial<Omit<CreditInput, 'variants'>> & {
		variants?: Partial<Record<CreditLogoKind, Partial<CreditVariant>>>;
	};
</script>

<script lang="ts">
	import { tick } from 'svelte';
	import { LoaderCircle, Save } from '@lucide/svelte';
	import { createMutation, useQueryClient } from '@tanstack/svelte-query';
	import type { AdminCredit } from '#lib/api/types.js';
	import Segmented from '../components/Segmented.svelte';
	import { ApiRequestError } from '../api';
	import { guardUnsaved } from '../guard.svelte';
	import { errorMessage } from '../format';
	import { toasts } from '../toast.svelte';
	import OriginsEditor from './OriginsEditor.svelte';
	import VariantCard from './VariantCard.svelte';
	import { LOGOS, MESSAGES, fieldFromMessage, validateForm } from './rules.js';
	import { saveCreditMutation } from './queries';

	let {
		credit,
		form,
		alt,
		edits = $bindable()
	}: {
		credit: AdminCredit;
		/** Saved values with the local edits on top. */
		form: CreditInput;
		/** Alt text the public endpoint sends with the logo. */
		alt: string;
		edits: CreditEdits;
	} = $props();

	const qc = useQueryClient();
	const saveM = createMutation(() => saveCreditMutation(qc));

	let errors = $state<Record<string, string>>({});
	let formError = $state<string | null>(null);

	const variantDirty = (kind: CreditLogoKind) => {
		const a = form.variants[kind];
		const b = credit.variants[kind];
		return a.prefix !== b.prefix || a.suffix !== b.suffix || a.style !== b.style;
	};
	const dirty = $derived(
		form.url !== credit.url ||
			form.logo !== credit.logo ||
			LOGOS.some(variantDirty) ||
			form.origins.join('\n') !== credit.origins.join('\n')
	);
	guardUnsaved(() => dirty && !saveM.isPending);

	const rowErrors = $derived(
		Object.fromEntries(Object.entries(errors).filter(([k]) => k.startsWith('origins[')))
	);

	const variantLabels: Record<CreditLogoKind, string> = { wordmark: 'Wordmark', mark: 'Mark' };
	const logoOptions = LOGOS.map((value) => ({ value, label: variantLabels[value] }));

	/** Errors for one variant card, keyed by field. */
	function variantErrors(kind: CreditLogoKind): Partial<Record<keyof CreditVariant, string>> {
		const p = `variants.${kind}.`;
		return {
			prefix: errors[p + 'prefix'],
			suffix: errors[p + 'suffix'],
			style: errors[p + 'style']
		};
	}

	function clearErrors(match: (key: string) => boolean) {
		errors = Object.fromEntries(Object.entries(errors).filter(([k]) => !match(k)));
	}

	function edit<K extends 'url' | 'logo' | 'origins'>(k: K, value: CreditInput[K]) {
		edits = { ...edits, [k]: value };
		// Row indexes shift when the list changes, so drop every origin error.
		clearErrors((key) => key === k || (k === 'origins' && key.startsWith('origins')));
	}

	function editVariant<K extends keyof CreditVariant>(
		kind: CreditLogoKind,
		field: K,
		value: CreditVariant[K]
	) {
		edits = {
			...edits,
			variants: { ...edits.variants, [kind]: { ...edits.variants?.[kind], [field]: value } }
		};
		clearErrors((key) => key === `variants.${kind}.${field}`);
	}

	/** Field errors from the API are about these inputs. */
	const ids: Record<string, string> = {
		url: 'credit-url',
		origins: 'credit-origin-new'
	};

	function focusField(key: string) {
		if (key === 'logo') {
			document.querySelector<HTMLElement>('#credit-logo-group [aria-checked="true"]')?.focus();
			return;
		}
		const v = /^variants\.(wordmark|mark)\.(prefix|suffix|style)$/.exec(key);
		if (v && v[2] === 'style') {
			document
				.querySelector<HTMLElement>(`#credit-${v[1]}-style-group [aria-checked="true"]`)
				?.focus();
			return;
		}
		const id = v ? `credit-${v[1]}-${v[2]}` : ids[key.startsWith('origins') ? 'origins' : key];
		document.getElementById(id)?.focus();
	}

	function discard() {
		edits = {};
		errors = {};
		formError = null;
	}

	/** Plain-English copy for a field the API rejected. */
	function messageFor(key: string, apiMessage: string): string {
		// A "variants ..." or "text ..." message is about the shape of the
		// body, not the prefix length, so show the API's own words.
		const token = apiMessage.split(' ', 1)[0];
		if (token === 'variants' || token === 'text') return apiMessage;
		if (key.startsWith('origins[')) return MESSAGES.origin;
		if (key === 'url' || key === 'logo' || key === 'origins') return MESSAGES[key];
		const field = key.split('.')[2];
		if (field === 'prefix' || field === 'suffix' || field === 'style') return MESSAGES[field];
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
		<p class="hint mt-1">
			Client sites show a prefix, the logo and a suffix. The logo and suffix link to the URL
			below. Each logo variant has its own text and style.
		</p>
	</div>

	<form class="flex min-w-0 flex-col gap-5" onsubmit={save} novalidate aria-labelledby="credit-form-h">
		<!-- Locked while saving, so no edit typed mid-request is lost when the save clears the edits. -->
		<fieldset class="flex min-w-0 flex-col gap-5" disabled={saveM.isPending}>
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
				<p class="field-label">Default logo</p>
				<div id="credit-logo-group">
					<Segmented
						options={logoOptions}
						value={form.logo}
						label="Default logo"
						onchange={(v) => edit('logo', v)}
					/>
				</div>
				{#if errors.logo}
					<p class="field-error" role="alert">{errors.logo}</p>
				{:else}
					<p class="hint">Used when a site does not add ?logo=.</p>
				{/if}
			</div>

			{#each LOGOS as kind (kind)}
				<VariantCard
					{kind}
					label={variantLabels[kind]}
					variant={form.variants[kind]}
					logos={credit.logos[kind]}
					url={form.url}
					{alt}
					prefixMax={credit.limits.prefix_max}
					suffixMax={credit.limits.suffix_max}
					errors={variantErrors(kind)}
					isDefault={form.logo === kind}
					onedit={(field, value) => editVariant(kind, field, value)}
				/>
			{/each}

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
