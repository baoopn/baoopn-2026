<script lang="ts">
	import type { PostInput } from '#lib/api/types.js';
	import ChipInput from '../components/ChipInput.svelte';
	import MediaField from '../components/MediaField.svelte';
	import { fromLocalInput, slugify, toLocalInput } from '../format';

	let {
		form = $bindable(),
		slugTouched = $bindable(false),
		slugError = $bindable<string | null>(null),
		tagSuggestions = []
	}: {
		form: PostInput;
		slugTouched?: boolean;
		slugError?: string | null;
		tagSuggestions?: string[];
	} = $props();

	const id = $props.id();

	// The picker shows local time; the API stores an instant.
	const publishLocal = $derived(toLocalInput(form.published_at));
	const scheduled = $derived(
		form.status === 'published' &&
			!!form.published_at &&
			new Date(form.published_at).getTime() > Date.now()
	);
	const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
</script>

<div class="flex flex-col gap-5">
	<fieldset class="flex flex-col gap-2">
		<legend class="field-label">Status</legend>
		<div class="flex flex-wrap gap-x-5">
			<label class="check">
				<input type="radio" name="{id}-status" value="draft" bind:group={form.status} /> Draft
			</label>
			<label class="check">
				<input type="radio" name="{id}-status" value="published" bind:group={form.status} />
				Published
			</label>
		</div>
		<div>
			<label class="field-label" for="{id}-date">Publish date</label>
			<input
				id="{id}-date"
				class="input"
				type="datetime-local"
				value={publishLocal}
				oninput={(e) => (form.published_at = fromLocalInput(e.currentTarget.value))}
				aria-describedby="{id}-date-hint"
			/>
			<p id="{id}-date-hint" class="hint">
				{#if scheduled}
					<strong class="text-info">Scheduled:</strong> goes live at this time ({zone}).
				{:else if form.status === 'published'}
					Empty means now. A future date schedules the post ({zone}).
				{:else}
					Used when you publish. A future date schedules it ({zone}).
				{/if}
			</p>
		</div>
	</fieldset>

	<div>
		<label class="field-label" for="{id}-slug">Slug</label>
		<div class="flex items-center gap-1">
			<span class="font-mono text-sm text-ink-soft">/blog/</span>
			<input
				id="{id}-slug"
				class="input font-mono text-sm"
				bind:value={form.slug}
				oninput={() => {
					slugTouched = true;
					slugError = null;
				}}
				onblur={() => (form.slug = slugify(form.slug ?? ''))}
				aria-invalid={!!slugError}
				aria-describedby="{id}-slug-msg"
				spellcheck="false"
				autocomplete="off"
			/>
		</div>
		{#if slugError}
			<p id="{id}-slug-msg" class="field-error" role="alert">{slugError}</p>
		{:else}
			<p id="{id}-slug-msg" class="hint">
				{slugTouched ? 'Edited by hand.' : 'Follows the title until you edit it.'}
			</p>
		{/if}
	</div>

	<div>
		<label class="field-label" for="{id}-excerpt">Excerpt</label>
		<textarea
			id="{id}-excerpt"
			class="input"
			rows="3"
			maxlength="300"
			bind:value={form.excerpt}
			aria-describedby="{id}-excerpt-hint"></textarea>
		<p id="{id}-excerpt-hint" class="hint">
			Shown in lists and link previews · {form.excerpt.length}/300
		</p>
	</div>

	<div>
		<label class="field-label" for="{id}-tags">Tags</label>
		<ChipInput
			id="{id}-tags"
			bind:values={form.tags}
			suggestions={tagSuggestions}
			describedby="{id}-tags-hint"
		/>
		<p id="{id}-tags-hint" class="hint">Enter or comma to add, Backspace to remove.</p>
	</div>

	<MediaField id="{id}-cover" label="Cover image" bind:value={form.cover_url} />

	<fieldset class="flex flex-col gap-3 border-t border-rule pt-4">
		<legend class="eyebrow">Series</legend>
		<div>
			<label class="field-label" for="{id}-series">Series key</label>
			<input
				id="{id}-series"
				class="input font-mono text-sm"
				bind:value={form.series}
				placeholder="e.g. homelab"
				autocomplete="off"
			/>
		</div>
		<div class="grid grid-cols-[1fr_6rem] gap-3">
			<div>
				<label class="field-label" for="{id}-series-title">Series title</label>
				<input id="{id}-series-title" class="input" bind:value={form.series_title} />
			</div>
			<div>
				<label class="field-label" for="{id}-series-order">Order</label>
				<input
					id="{id}-series-order"
					class="input"
					type="number"
					min="0"
					step="1"
					bind:value={form.series_order}
				/>
			</div>
		</div>
	</fieldset>
</div>
