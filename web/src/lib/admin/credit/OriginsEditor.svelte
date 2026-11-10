<script lang="ts">
	import { Plus, X } from '@lucide/svelte';
	import { MESSAGES, normalizeOrigin } from './rules.js';

	let {
		origins,
		max,
		errors = {},
		listError,
		onchange
	}: {
		origins: string[];
		max: number;
		/** Row errors keyed origins[<i>], from the form check or the API. */
		errors?: Record<string, string>;
		listError?: string;
		onchange: (origins: string[]) => void;
	} = $props();

	let draft = $state('');
	let addError = $state<string | null>(null);
	let added = $state<string | null>(null);

	const full = $derived(origins.length >= max);

	function add() {
		added = null;
		if (full) {
			addError = MESSAGES.origins;
			return;
		}
		const r = normalizeOrigin(draft);
		if ('error' in r) {
			addError = r.error;
			return;
		}
		if (origins.includes(r.origin)) {
			addError = MESSAGES.duplicate;
			return;
		}
		onchange([...origins, r.origin]);
		added = r.origin;
		addError = null;
		draft = '';
	}

	function remove(i: number) {
		added = null;
		onchange(origins.filter((_, j) => j !== i));
	}
</script>

<fieldset class="flex min-w-0 flex-col gap-3">
	<legend class="field-label mb-0 flex w-full items-center justify-between gap-2">
		Allowed client origins
		<span class="font-mono text-xs font-normal text-ink-soft">{origins.length} of {max}</span>
	</legend>
	<p class="hint mt-0">
		Only these sites can read the credit from the browser. Use the scheme and host, for example
		https://example.com.
	</p>

	<div class="flex min-w-0 gap-2">
		<label class="sr-only" for="credit-origin-new">New origin</label>
		<input
			id="credit-origin-new"
			class="input min-w-0 flex-1 font-mono"
			type="url"
			inputmode="url"
			autocomplete="off"
			spellcheck="false"
			placeholder="https://example.com"
			bind:value={draft}
			oninput={() => (addError = null)}
			onkeydown={(e) => {
				if (e.key === 'Enter') {
					// Add the row instead of submitting the whole form.
					e.preventDefault();
					add();
				}
			}}
			aria-invalid={!!addError}
			aria-describedby="credit-origin-new-msg"
		/>
		<button
			type="button"
			class="btn btn-secondary shrink-0"
			onclick={add}
			disabled={!draft.trim()}
		>
			<Plus class="size-4" aria-hidden="true" />
			Add
		</button>
	</div>
	<p id="credit-origin-new-msg" class={addError ? 'field-error mt-0' : 'hint mt-0'} role="status">
		{#if addError}{addError}{:else if added}Added {added}.{/if}
	</p>

	{#if origins.length}
		<ul class="flex min-w-0 flex-col divide-y divide-rule border-y border-rule">
			{#each origins as origin, i (origin)}
				{@const err = errors[`origins[${i}]`]}
				<li class="flex min-w-0 flex-col py-2">
					<div class="flex min-w-0 items-center justify-between gap-2">
						<span class="min-w-0 truncate font-mono text-sm text-ink" title={origin}>{origin}</span>
						<button
							type="button"
							class="btn btn-ghost btn-icon btn-sm shrink-0"
							aria-label="Remove {origin}"
							onclick={() => remove(i)}
						>
							<X class="size-4" aria-hidden="true" />
						</button>
					</div>
					{#if err}<p class="field-error mt-0" role="alert">{err}</p>{/if}
				</li>
			{/each}
		</ul>
	{:else}
		<p class="hint mt-0">No origins yet. The embed script works only on sites in this list.</p>
	{/if}

	{#if listError}<p class="field-error mt-0" role="alert">{listError}</p>{/if}
</fieldset>
