<script lang="ts">
	let { class: className = 'size-8' }: { class?: string } = $props();
</script>

<!-- Brand-kit monogram: primary (navy badge) by day, inverse by night. -->
<img
	src="/logo/baoopn-monogram-primary.svg"
	alt=""
	width="64"
	height="64"
	class={['block dark:hidden', className]}
/>
<img
	src="/logo/baoopn-monogram-inverse.svg"
	alt=""
	width="64"
	height="64"
	class={['hidden dark:block', className]}
/>
