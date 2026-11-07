<script lang="ts">
	import { X } from '@lucide/svelte';

	let {
		values = $bindable([]),
		id,
		placeholder = 'Type and press Enter',
		describedby,
		suggestions = []
	}: {
		values?: string[];
		id: string;
		placeholder?: string;
		describedby?: string;
		/** Offered via a datalist. */
		suggestions?: string[];
	} = $props();

	let draft = $state('');
	const listId = $derived(`${id}-suggestions`);
	const offered = $derived(suggestions.filter((s) => !values.includes(s)));

	function commit() {
		const parts = draft
			.split(',')
			.map((s) => s.trim())
			.filter(Boolean);
		if (parts.length === 0) return;
		const next = [...values];
		for (const p of parts) if (!next.includes(p)) next.push(p);
		values = next;
		draft = '';
	}

	function remove(v: string) {
		values = values.filter((x) => x !== v);
		document.getElementById(id)?.focus();
	}

	function onkeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' || e.key === ',') {
			e.preventDefault();
			commit();
		} else if (e.key === 'Backspace' && !draft && values.length) {
			values = values.slice(0, -1);
		}
	}
</script>

<div class="input flex flex-wrap items-center gap-1.5 py-1.5">
	{#each values as v (v)}
		<span class="tag">
			{v}
			<button
				type="button"
				class="-mr-1 grid size-6 place-items-center rounded-full hover:bg-paper-3"
				aria-label="Remove {v}"
				onclick={() => remove(v)}
			>
				<X class="size-3" aria-hidden="true" />
			</button>
		</span>
	{/each}
	<input
		{id}
		class="min-h-8 min-w-[8rem] flex-1 bg-transparent text-sm text-ink outline-none"
		{placeholder}
		bind:value={draft}
		{onkeydown}
		onblur={commit}
		aria-describedby={describedby}
		autocomplete="off"
		list={offered.length ? listId : undefined}
	/>
	{#if offered.length}
		<datalist id={listId}>
			{#each offered as s (s)}
				<option value={s}></option>
			{/each}
		</datalist>
	{/if}
</div>
