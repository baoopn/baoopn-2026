<script lang="ts">
	// Three little level bars; still when paused or under reduced motion.
	let { playing = false, class: klass = 'h-3.5 w-3.5' }: { playing?: boolean; class?: string } =
		$props();
</script>

<span class={['bars', klass, playing && 'on']} aria-hidden="true">
	<span></span><span></span><span></span>
</span>

<style>
	.bars {
		display: inline-flex;
		align-items: flex-end;
		gap: 2px;
	}
	.bars span {
		flex: 1;
		height: 35%;
		background: currentColor;
		border-radius: 1px;
	}
	.bars span:nth-child(2) {
		height: 70%;
	}
	.bars span:nth-child(3) {
		height: 50%;
	}
	.on span {
		animation: level 900ms ease-in-out infinite alternate;
	}
	.on span:nth-child(2) {
		animation-delay: -300ms;
	}
	.on span:nth-child(3) {
		animation-delay: -600ms;
	}
	@keyframes level {
		from {
			height: 25%;
		}
		to {
			height: 100%;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.on span {
			animation: none;
		}
	}
</style>
