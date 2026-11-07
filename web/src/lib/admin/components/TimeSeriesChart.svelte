<script lang="ts" module>
	export type Series = {
		id: string;
		label: string;
		/** Stroke colour (ink), e.g. `var(--series-a)`. */
		color: string;
		/** Flat spot-ink fill: the area under a line, or the column body. */
		fill?: string;
		/** `line` (default) or `column` (spot fill with an ink cap at the data end). */
		kind?: 'line' | 'column';
		/** SVG dash pattern, so identity never rests on colour alone. */
		dash?: string;
		values: number[];
	};
</script>

<script lang="ts">
	import { compact } from '../format';
	import { niceTicks, tickIndices } from './scale';

	let {
		x,
		series,
		label,
		height = 220,
		format = compact,
		axisFormat = compact,
		xFormat,
		xTooltipFormat
	}: {
		x: Date[];
		series: Series[];
		/** Accessible description of the chart. */
		label: string;
		height?: number;
		format?: (n: number) => string;
		axisFormat?: (n: number) => string;
		xFormat: (d: Date) => string;
		xTooltipFormat: (d: Date) => string;
	} = $props();

	const M = { top: 12, right: 60, bottom: 28, left: 48 };

	let width = $state(0);
	let hover = $state<number | null>(null);

	const n = $derived(x.length);
	const innerW = $derived(Math.max(0, width - M.left - M.right));
	const innerH = $derived(Math.max(0, height - M.top - M.bottom));
	const maxY = $derived(Math.max(0, ...series.flatMap((s) => s.values)));
	const yTicks = $derived(niceTicks(maxY));
	const yTop = $derived(yTicks[yTicks.length - 1] || 1);
	const xTicks = $derived(tickIndices(n, Math.max(2, Math.floor(innerW / 96))));

	const xPos = (i: number) => (n <= 1 ? innerW / 2 : (i / (n - 1)) * innerW);
	const yPos = (v: number) => innerH - (v / yTop) * innerH;

	function linePath(values: number[]): string {
		return values
			.map((v, i) => `${i === 0 ? 'M' : 'L'}${xPos(i).toFixed(1)},${yPos(v).toFixed(1)}`)
			.join('');
	}

	function areaPath(values: number[]): string {
		if (values.length === 0) return '';
		return `${linePath(values)}L${xPos(values.length - 1).toFixed(1)},${innerH}L${xPos(0).toFixed(1)},${innerH}Z`;
	}

	const lines = $derived(series.filter((s) => s.kind !== 'column'));
	const columns = $derived(series.filter((s) => s.kind === 'column'));
	const colW = $derived(Math.max(2, Math.min(24, n > 0 ? (innerW / n) * 0.6 : 0)));

	// Direct end labels, only when they don't collide (otherwise legend + tooltip carry identity).
	const endLabels = $derived.by(() => {
		if (n === 0 || series.length > 4) return [];
		const items = series
			.map((s) => ({ s, y: yPos(s.values[n - 1] ?? 0) }))
			.sort((a, b) => a.y - b.y);
		for (let i = 1; i < items.length; i++) {
			if (items[i].y - items[i - 1].y < 14) return [];
		}
		return items;
	});

	function indexFromPointer(e: PointerEvent): number | null {
		const svg = (e.currentTarget as SVGElement).ownerSVGElement;
		if (!svg || n === 0) return null;
		const rect = svg.getBoundingClientRect();
		const px = e.clientX - rect.left - M.left;
		if (n === 1) return 0;
		const i = Math.round((px / innerW) * (n - 1));
		return Math.min(n - 1, Math.max(0, i));
	}

	function onkeydown(e: KeyboardEvent) {
		if (n === 0) return;
		const cur = hover ?? n - 1;
		let next: number | null;
		if (e.key === 'ArrowLeft') next = Math.max(0, cur - 1);
		else if (e.key === 'ArrowRight') next = Math.min(n - 1, cur + 1);
		else if (e.key === 'Home') next = 0;
		else if (e.key === 'End') next = n - 1;
		else if (e.key === 'Escape') next = null;
		else return;
		e.preventDefault();
		hover = next;
	}

	const tipLeft = $derived(hover === null ? 0 : M.left + xPos(hover));
	const tipFlip = $derived(hover !== null && xPos(hover) > innerW / 2);
</script>

<div class="relative w-full select-none" bind:clientWidth={width}>
	{#if series.length > 1}
		<ul
			class="mb-3 flex flex-wrap gap-x-4 gap-y-1 font-mono text-xs text-ink-soft"
			aria-label="Legend"
		>
			{#each series as s (s.id)}
				<li class="flex items-center gap-1.5">
					{#if s.kind === 'column'}
						<span
							class="h-2.5 w-3 rounded-t-[2px] border-t-2"
							style:background={s.fill}
							style:border-color={s.color}
							aria-hidden="true"
						></span>
					{:else}
						<svg width="18" height="4" aria-hidden="true"
							><line
								x1="0"
								x2="18"
								y1="2"
								y2="2"
								stroke={s.color}
								stroke-width="2"
								stroke-dasharray={s.dash}
							/></svg
						>
					{/if}
					{s.label}
				</li>
			{/each}
		</ul>
	{/if}

	{#if width > 0}
		<svg
			{width}
			{height}
			role="img"
			aria-label={label}
			class="block overflow-visible"
		>
			<g transform="translate({M.left},{M.top})">
				<!-- gridlines + y axis labels -->
				{#each yTicks as t (t)}
					<line
						x1="0"
						x2={innerW}
						y1={yPos(t)}
						y2={yPos(t)}
						stroke={t === 0 ? 'var(--ink)' : 'var(--rule)'}
						stroke-width="1"
						shape-rendering="crispEdges"
					/>
					<text
						x="-8"
						y={yPos(t)}
						dy="0.32em"
						text-anchor="end"
						class="fill-ink-soft font-mono text-[11px] tabular-nums">{axisFormat(t)}</text
					>
				{/each}

				<!-- x axis labels -->
				{#each xTicks as i (i)}
					<text
						x={xPos(i)}
						y={innerH + 18}
						text-anchor={i === 0 && n > 1 ? 'start' : i === n - 1 && n > 1 ? 'end' : 'middle'}
						class="fill-ink-soft font-mono text-[11px] tabular-nums">{xFormat(x[i])}</text
					>
				{/each}

				<!-- spot fills under lines (flat, no gradient) -->
				{#each lines as s (s.id)}
					{#if s.fill}<path d={areaPath(s.values)} fill={s.fill} />{/if}
				{/each}

				<!-- columns: spot body, 2px ink cap on the data end, square at the baseline -->
				{#each columns as s (s.id)}
					{#each s.values as v, i (i)}
						{#if v > 0}
							<rect
								x={xPos(i) - colW / 2}
								y={yPos(v)}
								width={colW}
								height={Math.max(0, innerH - yPos(v))}
								fill={s.fill ?? s.color}
							/>
							<rect x={xPos(i) - colW / 2} y={yPos(v)} width={colW} height="2" fill={s.color} />
						{/if}
					{/each}
				{/each}

				{#each lines as s (s.id)}
					<path
						d={linePath(s.values)}
						fill="none"
						stroke={s.color}
						stroke-width="2"
						stroke-dasharray={s.dash}
						stroke-linejoin="round"
						stroke-linecap="round"
					/>
				{/each}

				{#each endLabels as l (l.s.id)}
					<text
						x={innerW + 8}
						y={l.y}
						dy="0.32em"
						class="fill-ink font-mono text-[11px] font-medium"
						>{format(l.s.values[n - 1] ?? 0)}</text
					>
				{/each}

				{#if hover !== null}
					<line
						x1={xPos(hover)}
						x2={xPos(hover)}
						y1="0"
						y2={innerH}
						stroke="var(--ink)"
						stroke-width="1"
						shape-rendering="crispEdges"
					/>
					{#each lines as s (s.id)}
						<circle
							cx={xPos(hover)}
							cy={yPos(s.values[hover] ?? 0)}
							r="4"
							fill={s.color}
							stroke="var(--paper-2)"
							stroke-width="2"
						/>
					{/each}
				{/if}

				<!-- hit layer: the crosshair finds the nearest X -->
				<rect
					x={-4}
					y="0"
					width={innerW + 8}
					height={innerH}
					fill="transparent"
					tabindex="0"
					role="slider"
					aria-label="{label}: use arrow keys to inspect values"
					aria-valuemin={0}
					aria-valuemax={Math.max(0, n - 1)}
					aria-valuenow={hover ?? n - 1}
					aria-valuetext={hover !== null && x[hover]
						? `${xTooltipFormat(x[hover])}: ${series.map((s) => `${s.label} ${format(s.values[hover ?? 0] ?? 0)}`).join(', ')}`
						: undefined}
					class="cursor-crosshair outline-none focus-visible:stroke-[var(--ring)] focus-visible:stroke-2"
					onpointermove={(e) => (hover = indexFromPointer(e))}
					onpointerdown={(e) => (hover = indexFromPointer(e))}
					onpointerleave={() => (hover = null)}
					onblur={() => (hover = null)}
					{onkeydown}
				/>
			</g>
		</svg>
	{:else}
		<div style:height="{height}px"></div>
	{/if}

	{#if hover !== null && x[hover]}
		<div
			class="card-print pointer-events-none absolute top-8 z-10 min-w-36 px-3 py-2 text-xs"
			style:left="{tipLeft}px"
			style:transform={tipFlip ? 'translateX(calc(-100% - 12px))' : 'translateX(12px)'}
		>
			<p class="eyebrow mb-1.5">{xTooltipFormat(x[hover])}</p>
			<ul class="flex flex-col gap-1">
				{#each series as s (s.id)}
					<li class="flex items-center gap-2">
						{#if s.kind === 'column'}
							<span
								class="h-2 w-2.5 border-t-2"
								style:background={s.fill}
								style:border-color={s.color}
								aria-hidden="true"
							></span>
						{:else}
							<span class="h-0.5 w-3 rounded-full" style:background={s.color} aria-hidden="true"
							></span>
						{/if}
						<span class="font-mono font-semibold text-ink tabular-nums"
							>{format(s.values[hover] ?? 0)}</span
						>
						<span class="text-ink-soft">{s.label}</span>
					</li>
				{/each}
			</ul>
		</div>
	{/if}
</div>
