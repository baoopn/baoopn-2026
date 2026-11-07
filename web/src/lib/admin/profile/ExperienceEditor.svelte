<script lang="ts">
	import { Plus } from '@lucide/svelte';
	import type { ExperienceItem } from '#lib/api/types.js';
	import ItemActions from './ItemActions.svelte';
	import { moveItem, removeItem } from './list';

	let { items = $bindable([]) }: { items: ExperienceItem[] } = $props();

	const id = $props.id();
</script>

<div class="flex flex-col gap-3">
	{#if items.length === 0}
		<p class="text-sm text-ink-soft">No experience yet.</p>
	{/if}
	<ol class="flex flex-col gap-3">
		{#each items as x, i (i)}
			{@const bad = !!x.end && !!x.start && x.end < x.start}
			<li class="card flex flex-col gap-3 p-3">
				<div class="flex items-start justify-between gap-3">
					<p class="eyebrow pt-3">{x.role || 'Role'} · {x.org || 'Organisation'}</p>
					<ItemActions
						name={x.role || `entry ${i + 1}`}
						index={i}
						count={items.length}
						onmove={(d) => (items = moveItem(items, i, d))}
						onremove={() => (items = removeItem(items, i))}
					/>
				</div>
				<div class="grid gap-3 sm:grid-cols-2">
					<div>
						<label class="field-label" for="{id}-role-{i}">Role</label>
						<input id="{id}-role-{i}" class="input" bind:value={x.role} required />
					</div>
					<div>
						<label class="field-label" for="{id}-org-{i}">Organisation</label>
						<input id="{id}-org-{i}" class="input" bind:value={x.org} required />
					</div>
					<div>
						<label class="field-label" for="{id}-url-{i}">URL</label>
						<input
							id="{id}-url-{i}"
							class="input"
							type="text"
							inputmode="url"
							bind:value={x.url}
							placeholder="https://"
						/>
					</div>
					<div>
						<label class="field-label" for="{id}-loc-{i}">Location</label>
						<input id="{id}-loc-{i}" class="input" bind:value={x.location} />
					</div>
					<div>
						<label class="field-label" for="{id}-start-{i}">Start</label>
						<input
							id="{id}-start-{i}"
							class="input"
							type="month"
							bind:value={x.start}
							placeholder="YYYY-MM"
						/>
					</div>
					<div>
						<label class="field-label" for="{id}-end-{i}">End</label>
						<input
							id="{id}-end-{i}"
							class="input"
							type="month"
							bind:value={x.end}
							placeholder="YYYY-MM"
							aria-invalid={bad}
							aria-describedby="{id}-end-hint-{i}"
						/>
						{#if bad}
							<p id="{id}-end-hint-{i}" class="field-error" role="alert">End is before start.</p>
						{:else}
							<p id="{id}-end-hint-{i}" class="hint">Empty means present.</p>
						{/if}
					</div>
				</div>
				<div>
					<label class="field-label" for="{id}-sum-{i}">Summary (Markdown)</label>
					<textarea
						id="{id}-sum-{i}"
						class="input font-mono text-sm"
						rows="3"
						bind:value={x.summary_md}></textarea>
				</div>
			</li>
		{/each}
	</ol>
	<div>
		<button
			type="button"
			class="btn btn-secondary btn-sm"
			onclick={() =>
				(items = [
					...items,
					{ role: '', org: '', url: '', location: '', start: '', end: '', summary_md: '' }
				])}
		>
			<Plus class="size-4" aria-hidden="true" /> Add experience
		</button>
	</div>
</div>
