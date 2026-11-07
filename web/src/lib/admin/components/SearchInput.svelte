<script lang="ts">
	import { Search, X } from '@lucide/svelte';
	import { debounce } from '../debounce';

	let {
		value = '',
		placeholder = 'Search…',
		label = 'Search',
		delay = 300,
		class: className = 'w-full sm:w-72',
		onsearch
	}: {
		value?: string;
		placeholder?: string;
		label?: string;
		delay?: number;
		class?: string;
		onsearch: (q: string) => void;
	} = $props();

	// Local draft seeded from the prop; the parent owns the committed value.
	// svelte-ignore state_referenced_locally
	let draft = $state(value);

	// svelte-ignore state_referenced_locally
	const emit = debounce((q: string) => onsearch(q.trim()), delay);

	function clear() {
		emit.cancel();
		draft = '';
		onsearch('');
	}
</script>

<div class={['relative', className]}>
	<Search
		class="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-ink-soft"
		aria-hidden="true"
	/>
	<input
		type="search"
		class="input pr-11 pl-9"
		{placeholder}
		aria-label={label}
		bind:value={draft}
		oninput={() => emit(draft)}
		onkeydown={(e) => {
			if (e.key === 'Enter') {
				emit.cancel();
				onsearch(draft.trim());
			}
		}}
	/>
	{#if draft}
		<button
			type="button"
			class="absolute top-1/2 right-1 grid size-9 -translate-y-1/2 place-items-center rounded-md text-ink-soft hover:bg-paper-3 hover:text-ink"
			aria-label="Clear search"
			onclick={clear}
		>
			<X class="size-4" aria-hidden="true" />
		</button>
	{/if}
</div>

<style>
	input[type='search']::-webkit-search-cancel-button {
		display: none;
	}
</style>
