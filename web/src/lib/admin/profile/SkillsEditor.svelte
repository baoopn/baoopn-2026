<script lang="ts">
	import { Plus } from '@lucide/svelte';
	import type { SkillGroup } from '#lib/api/types.js';
	import ChipInput from '../components/ChipInput.svelte';
	import ItemActions from './ItemActions.svelte';
	import { moveItem, removeItem } from './list';

	let { items = $bindable([]) }: { items: SkillGroup[] } = $props();

	const id = $props.id();
</script>

<div class="flex flex-col gap-3">
	{#if items.length === 0}
		<p class="text-sm text-ink-soft">No skill groups yet.</p>
	{/if}
	<ol class="flex flex-col gap-3">
		{#each items as g, i (i)}
			<li class="card flex flex-col gap-3 p-3">
				<div class="flex items-end gap-3">
					<div class="flex-1">
						<label class="field-label" for="{id}-group-{i}">Group</label>
						<input id="{id}-group-{i}" class="input" bind:value={g.group} placeholder="Frontend" />
					</div>
					<ItemActions
						name={g.group || `group ${i + 1}`}
						index={i}
						count={items.length}
						onmove={(d) => (items = moveItem(items, i, d))}
						onremove={() => (items = removeItem(items, i))}
					/>
				</div>
				<div>
					<label class="field-label" for="{id}-items-{i}">Skills</label>
					<ChipInput id="{id}-items-{i}" bind:values={g.items} placeholder="Svelte, Go, …" />
				</div>
			</li>
		{/each}
	</ol>
	<div>
		<button
			type="button"
			class="btn btn-secondary btn-sm"
			onclick={() => (items = [...items, { group: '', items: [] }])}
		>
			<Plus class="size-4" aria-hidden="true" /> Add group
		</button>
	</div>
</div>
