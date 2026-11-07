<script lang="ts">
	import type { PostState } from '#lib/api/types.js';

	let { state }: { state: PostState } = $props();

	const spot: Record<PostState, string> = {
		draft: 'var(--spot-butter)',
		scheduled: 'var(--spot-sky)',
		published: 'var(--spot-mint)'
	};
</script>

<!-- Status = flat chip with a dot + mono label, filled with the matching spot ink. -->
<span class="tag gap-1.5 border border-on-spot" style:--tag={spot[state]}>
	<span class="size-1.5 rounded-full bg-on-spot" aria-hidden="true"></span>
	{state}
</span>
