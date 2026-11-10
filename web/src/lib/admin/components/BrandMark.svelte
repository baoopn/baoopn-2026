<script lang="ts">
	import { page } from '$app/state';
	import { brandSize, brandSrc } from '#lib/brand.js';

	let { class: className = 'size-8' }: { class?: string } = $props();

	const brand = $derived(page.data.brand);
	const primary = $derived(brandSize(brand, 'monogram_primary', 64, 64));
	const inverse = $derived(brandSize(brand, 'monogram_inverse', 64, 64));
</script>

<!-- Brand-kit glasses mark: primary (navy badge) by day, inverse by night. -->
<img
	src={brandSrc(brand, 'monogram_primary')}
	alt=""
	width={primary.width}
	height={primary.height}
	class={['block dark:hidden', className]}
/>
<img
	src={brandSrc(brand, 'monogram_inverse')}
	alt=""
	width={inverse.width}
	height={inverse.height}
	class={['hidden dark:block', className]}
/>
