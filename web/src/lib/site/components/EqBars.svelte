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
	/* Full-height bars scaled from the bottom: the level moves on the compositor. */
	.bars span {
		flex: 1;
		height: 100%;
		background: currentColor;
		border-radius: 1px;
		transform-origin: bottom;
		transform: scaleY(0.35);
	}
	.bars span:nth-child(2) {
		transform: scaleY(0.7);
	}
	.bars span:nth-child(3) {
		transform: scaleY(0.5);
	}
	.on span {
		animation: level 900ms var(--ease-soft-inout) infinite alternate;
	}
	.on span:nth-child(2) {
		animation-delay: -300ms;
	}
	.on span:nth-child(3) {
		animation-delay: -600ms;
	}
	@keyframes level {
		from {
			transform: scaleY(0.25);
		}
		to {
			transform: scaleY(1);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.on span {
			animation: none;
		}
	}
</style>
