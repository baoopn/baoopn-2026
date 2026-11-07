<script lang="ts">
	import { MessagesSquare } from '@lucide/svelte';
	import EmptyState from '#lib/admin/components/EmptyState.svelte';
</script>

<svelte:head>
	<title>Inbox · baoopn admin</title>
</svelte:head>

<div class="grid h-full place-items-center">
	<EmptyState
		icon={MessagesSquare}
		title="Pick a thread"
		message="Choose a conversation on the left to read it and reply."
	/>
</div>
