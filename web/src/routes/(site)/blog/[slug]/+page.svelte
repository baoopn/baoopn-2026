<script lang="ts">
	import PostView from '#lib/site/components/PostView.svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();
</script>

<PostView post={data.post} author={data.profile.name} />
