<script lang="ts">
	import '../app.css';
	import '@fontsource-variable/inter';
	import '@fontsource-variable/outfit';
	import '@fontsource-variable/jetbrains-mono';
	import { brandSrc } from '#lib/brand.js';

	let { data, children } = $props();
</script>

<svelte:head>
	<link rel="icon" href={brandSrc(data.brand, 'favicon_ico')} sizes="48x48" />
	<link rel="icon" href={brandSrc(data.brand, 'favicon_svg')} type="image/svg+xml" />
	<link rel="apple-touch-icon" href={brandSrc(data.brand, 'apple_touch_icon')} />
</svelte:head>

{@render children()}
