<script lang="ts">
	import { ArrowDown, ArrowUp, Trash2 } from '@lucide/svelte';

	let {
		name,
		index,
		count,
		onmove,
		onremove
	}: {
		/** What the row is, for the button labels ("Move GitHub up"). */
		name: string;
		index: number;
		count: number;
		onmove: (dir: -1 | 1) => void;
		onremove: () => void;
	} = $props();
</script>

<div class="flex shrink-0 items-center gap-0.5">
	<button
		type="button"
		class="btn btn-ghost btn-icon btn-sm"
		aria-label="Move {name} up"
		disabled={index === 0}
		onclick={() => onmove(-1)}
	>
		<ArrowUp class="size-4" aria-hidden="true" />
	</button>
	<button
		type="button"
		class="btn btn-ghost btn-icon btn-sm"
		aria-label="Move {name} down"
		disabled={index === count - 1}
		onclick={() => onmove(1)}
	>
		<ArrowDown class="size-4" aria-hidden="true" />
	</button>
	<button
		type="button"
		class="btn btn-ghost btn-icon btn-sm"
		aria-label="Remove {name}"
		onclick={onremove}
	>
		<Trash2 class="size-4" aria-hidden="true" />
	</button>
</div>
