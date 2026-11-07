<script lang="ts">
	import { Plus } from '@lucide/svelte';
	import type { SocialLink } from '#lib/api/types.js';
	import ItemActions from './ItemActions.svelte';
	import { moveItem, removeItem } from './list';

	let { items = $bindable([]) }: { items: SocialLink[] } = $props();

	const id = $props.id();
	const ICONS = [
		'github',
		'linkedin',
		'instagram',
		'email',
		'x',
		'youtube',
		'website',
		'rss',
		'spotify'
	];
</script>

<div class="flex flex-col gap-3">
	{#if items.length === 0}
		<p class="text-sm text-ink-soft">No links yet.</p>
	{/if}
	<ol class="flex flex-col gap-3">
		{#each items as s, i (i)}
			<li class="card grid gap-3 p-3 sm:grid-cols-[8rem_1fr_1fr_1fr_auto] sm:items-end">
				<div>
					<label class="field-label" for="{id}-icon-{i}">Icon</label>
					<select id="{id}-icon-{i}" class="input" bind:value={s.icon}>
						{#each ICONS as ic (ic)}<option value={ic}>{ic}</option>{/each}
					</select>
				</div>
				<div>
					<label class="field-label" for="{id}-label-{i}">Label</label>
					<input id="{id}-label-{i}" class="input" bind:value={s.label} placeholder="GitHub" />
				</div>
				<div>
					<label class="field-label" for="{id}-url-{i}">URL</label>
					<input
						id="{id}-url-{i}"
						class="input"
						type="text"
						inputmode="url"
						bind:value={s.url}
						placeholder="https://"
					/>
				</div>
				<div>
					<label class="field-label" for="{id}-handle-{i}">Handle</label>
					<input id="{id}-handle-{i}" class="input" bind:value={s.handle} placeholder="@baoopn" />
				</div>
				<ItemActions
					name={s.label || `link ${i + 1}`}
					index={i}
					count={items.length}
					onmove={(d) => (items = moveItem(items, i, d))}
					onremove={() => (items = removeItem(items, i))}
				/>
			</li>
		{/each}
	</ol>
	<div>
		<button
			type="button"
			class="btn btn-secondary btn-sm"
			onclick={() => (items = [...items, { label: '', url: '', icon: 'website', handle: '' }])}
		>
			<Plus class="size-4" aria-hidden="true" /> Add link
		</button>
	</div>
</div>
