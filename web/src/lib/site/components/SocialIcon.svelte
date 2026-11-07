<script lang="ts">
	// Line icons for social links (Lucide has no brand marks). 24px grid, 1.75 stroke.
	let { icon, class: klass = 'size-5' }: { icon: string; class?: string } = $props();

	type Shape =
		| { t: 'path'; d: string }
		| { t: 'rect'; x: number; y: number; w: number; h: number; rx?: number }
		| { t: 'circle'; cx: number; cy: number; r: number };

	const SHAPES: Record<string, Shape[]> = {
		github: [
			{
				t: 'path',
				d: 'M15 22v-4a4.8 4.8 0 0 0-1-3.5c3 0 6-2 6-5.5.08-1.25-.27-2.48-1-3.5.28-1.15.28-2.35 0-3.5 0 0-1 0-3 1.5-2.64-.5-5.36-.5-8 0C6 2 5 2 5 2c-.3 1.15-.3 2.35 0 3.5A5.4 5.4 0 0 0 4 9c0 3.5 3 5.5 6 5.5-.39.49-.68 1.05-.85 1.65-.17.6-.22 1.23-.15 1.85v4'
			},
			{ t: 'path', d: 'M9 18c-4.51 2-5-2-7-2' }
		],
		linkedin: [
			{ t: 'path', d: 'M16 8a6 6 0 0 1 6 6v7h-4v-7a2 2 0 0 0-4 0v7h-4v-7a6 6 0 0 1 6-6z' },
			{ t: 'rect', x: 2, y: 9, w: 4, h: 12 },
			{ t: 'circle', cx: 4, cy: 4, r: 2 }
		],
		instagram: [
			{ t: 'rect', x: 2, y: 2, w: 20, h: 20, rx: 5 },
			{ t: 'path', d: 'M16 11.37A4 4 0 1 1 12.63 8 4 4 0 0 1 16 11.37z' },
			{ t: 'path', d: 'M17.5 6.5h.01' }
		],
		x: [
			{ t: 'path', d: 'M4 4h4.5L20 20h-4.5z' },
			{ t: 'path', d: 'M4 20l6.6-6.6M13.4 10.6 20 4' }
		],
		youtube: [
			{
				t: 'path',
				d: 'M2.5 17a24 24 0 0 1 0-10 2 2 0 0 1 1.4-1.4 49.6 49.6 0 0 1 16.2 0A2 2 0 0 1 21.5 7a24 24 0 0 1 0 10 2 2 0 0 1-1.4 1.4 49.6 49.6 0 0 1-16.2 0A2 2 0 0 1 2.5 17'
			},
			{ t: 'path', d: 'm10 15 5-3-5-3z' }
		],
		spotify: [
			{ t: 'circle', cx: 12, cy: 12, r: 10 },
			{ t: 'path', d: 'M6.5 9.2c3.6-1.2 8-.9 11.2 1M7.2 12.4c3-.9 6.6-.6 9.3.9M8 15.5c2.4-.6 4.9-.4 7 .7' }
		],
		email: [
			{ t: 'rect', x: 2, y: 4, w: 20, h: 16, rx: 2 },
			{ t: 'path', d: 'm22 7-8.97 5.7a1.94 1.94 0 0 1-2.06 0L2 7' }
		],
		rss: [
			{ t: 'path', d: 'M4 11a9 9 0 0 1 9 9M4 4a16 16 0 0 1 16 16' },
			{ t: 'circle', cx: 5, cy: 19, r: 1 }
		],
		website: [
			{ t: 'circle', cx: 12, cy: 12, r: 10 },
			{ t: 'path', d: 'M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20M2 12h20' }
		]
	};

	const shapes = $derived(SHAPES[icon] ?? SHAPES.website);
</script>

<svg
	viewBox="0 0 24 24"
	class={klass}
	fill="none"
	stroke="currentColor"
	stroke-width="1.75"
	stroke-linecap="round"
	stroke-linejoin="round"
	aria-hidden="true"
>
	{#each shapes as s, i (i)}
		{#if s.t === 'path'}
			<path d={s.d} />
		{:else if s.t === 'rect'}
			<rect x={s.x} y={s.y} width={s.w} height={s.h} rx={s.rx ?? 0} />
		{:else}
			<circle cx={s.cx} cy={s.cy} r={s.r} />
		{/if}
	{/each}
</svg>
