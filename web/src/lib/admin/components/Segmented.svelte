<script lang="ts" generics="T extends string">
	import type { Component } from 'svelte';

	type Option = { value: T; label: string; icon?: Component };

	let {
		options,
		value = $bindable(),
		label,
		size = 'md',
		onchange
	}: {
		options: Option[];
		value: T;
		label: string;
		size?: 'sm' | 'md';
		onchange?: (value: T) => void;
	} = $props();

	function select(v: T) {
		if (v === value) return;
		value = v;
		onchange?.(v);
	}

	function onkeydown(e: KeyboardEvent, index: number) {
		let next = -1;
		if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (index + 1) % options.length;
		else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp')
			next = (index - 1 + options.length) % options.length;
		else if (e.key === 'Home') next = 0;
		else if (e.key === 'End') next = options.length - 1;
		if (next < 0) return;
		e.preventDefault();
		select(options[next].value);
		const group = (e.currentTarget as HTMLElement).parentElement;
		group?.querySelectorAll<HTMLButtonElement>('[role="radio"]')[next]?.focus();
	}
</script>

<div
	role="radiogroup"
	aria-label={label}
	class="inline-flex items-center gap-0.5 rounded-lg border border-rule-strong bg-paper-2 p-0.5"
>
	{#each options as opt, i (opt.value)}
		{@const active = opt.value === value}
		<button
			type="button"
			role="radio"
			aria-checked={active}
			tabindex={active ? 0 : -1}
			class={[
				'inline-flex min-h-11 items-center gap-1.5 rounded-md font-mono font-medium tracking-[0.04em] uppercase transition-colors duration-150',
				size === 'sm' ? 'px-2.5 py-1 text-[0.6875rem]' : 'px-3 py-1.5 text-xs',
				active ? 'bg-spot-select text-on-spot' : 'text-ink-soft hover:bg-paper-3 hover:text-ink'
			]}
			onclick={() => select(opt.value)}
			onkeydown={(e) => onkeydown(e, i)}
		>
			{#if opt.icon}<opt.icon class="size-4" aria-hidden="true" />{/if}
			{opt.label}
		</button>
	{/each}
</div>
