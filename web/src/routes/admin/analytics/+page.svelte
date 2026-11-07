<script lang="ts">
	import type { AnalyticsRange, AnalyticsReport, Breakdown } from '#lib/api/types.js';
	import PageHeader from '#lib/admin/components/PageHeader.svelte';
	import Segmented from '#lib/admin/components/Segmented.svelte';
	import KpiRow, { type Kpi } from '#lib/admin/components/KpiRow.svelte';
	import TimeSeriesChart, { type Series } from '#lib/admin/components/TimeSeriesChart.svelte';
	import BarTable, { type BarRow } from '#lib/admin/components/BarTable.svelte';
	import ErrorState from '#lib/admin/components/ErrorState.svelte';
	import { get, qs } from '#lib/admin/api.js';
	import { Resource } from '#lib/admin/resource.svelte.js';
	import { compact, countryName, delta, flag, timeSpan } from '#lib/admin/format.js';

	const tz = Intl.DateTimeFormat().resolvedOptions().timeZone;
	let range = $state<AnalyticsRange>('7d');

	const report = new Resource(() => get<AnalyticsReport>('/analytics' + qs({ range, tz })));

	$effect(() => {
		void report.load();
	});

	// Keep "live now" fresh.
	$effect(() => {
		const t = setInterval(() => {
			if (document.visibilityState === 'visible') void report.load(true);
		}, 60_000);
		return () => clearInterval(t);
	});

	const r = $derived(report.data?.range === range ? report.data : undefined);

	const kpis: Kpi[] = $derived(
		r
			? [
					{
						label: 'Views',
						value: compact(r.totals.views),
						delta: delta(r.totals.views, r.totals.views_prev),
						sub: `vs ${compact(r.totals.views_prev)} before`
					},
					{
						label: 'Visitors',
						value: compact(r.totals.visitors),
						delta: delta(r.totals.visitors, r.totals.visitors_prev),
						sub: `vs ${compact(r.totals.visitors_prev)} before`
					},
					{
						label: 'Avg. time on page',
						value: timeSpan(r.totals.avg_visible_ms),
						sub: 'visible time'
					},
					{
						label: 'Live now',
						value: compact(r.totals.live_now),
						live: r.totals.live_now > 0,
						sub: 'active visitors'
					}
				]
			: ['Views', 'Visitors', 'Avg. time on page', 'Live now'].map((label) => ({
					label,
					value: ''
				}))
	);

	const x = $derived(r ? r.timeseries.map((p) => new Date(p.t)) : []);
	const series: Series[] = $derived(
		r
			? [
					{
						id: 'views',
						label: 'Views',
						color: 'var(--series-a)',
						fill: 'var(--series-a-fill)',
						values: r.timeseries.map((p) => p.views)
					},
					{
						id: 'visitors',
						label: 'Visitors',
						color: 'var(--ink-soft)',
						dash: '5 4',
						values: r.timeseries.map((p) => p.visitors)
					}
				]
			: []
	);

	const hourFmt = new Intl.DateTimeFormat(undefined, { hour: 'numeric', timeZone: tz });
	const dayFmt = new Intl.DateTimeFormat(undefined, {
		month: 'short',
		day: 'numeric',
		timeZone: tz
	});
	const hourTip = new Intl.DateTimeFormat(undefined, {
		weekday: 'short',
		hour: 'numeric',
		minute: '2-digit',
		timeZone: tz
	});
	const dayTip = new Intl.DateTimeFormat(undefined, {
		weekday: 'short',
		month: 'short',
		day: 'numeric',
		timeZone: tz
	});
	const hourly = $derived(r?.bucket === 'hour');

	function rows(list: Breakdown[], map?: (b: Breakdown) => Partial<BarRow>): BarRow[] {
		return list.map((b) => ({
			key: b.key || '(none)',
			label: b.key || '(none)',
			views: b.views,
			visitors: b.visitors,
			...map?.(b)
		}));
	}

	const options: { value: AnalyticsRange; label: string }[] = [
		{ value: '24h', label: '24h' },
		{ value: '7d', label: '7d' },
		{ value: '30d', label: '30d' },
		{ value: '90d', label: '90d' }
	];

	const tables = $derived(
		r
			? [
					{ title: 'Pages', keyLabel: 'Path', rows: rows(r.paths, (b) => ({ href: b.key })) },
					{
						title: 'Referrers',
						keyLabel: 'Source',
						rows: rows(r.referrers, (b) => ({ label: b.key || 'Direct / none' }))
					},
					{
						title: 'Countries',
						keyLabel: 'Country',
						rows: rows(r.countries, (b) => ({
							prefix: flag(b.key),
							label: b.key ? countryName(b.key) : 'Unknown'
						}))
					},
					{ title: 'Devices', keyLabel: 'Device', rows: rows(r.devices) },
					{ title: 'Browsers', keyLabel: 'Browser', rows: rows(r.browsers) }
				]
			: []
	);
</script>

<PageHeader
	title="Analytics"
	eyebrow="Traffic"
	description="Cookieless page views · times in {tz}{r
		? ` · ${compact(r.totals.live_now)} live now`
		: ''}"
>
	{#snippet actions()}
		<Segmented {options} bind:value={range} label="Time range" />
	{/snippet}
</PageHeader>

{#if report.error && !r}
	<ErrorState message={report.error} onretry={() => report.load()} />
{:else}
	<KpiRow items={kpis} loading={!r} label="Traffic totals" />

	<section class="mt-8" aria-labelledby="ts-h">
		<h2 id="ts-h" class="mb-3 font-display text-2xl font-semibold">Views and visitors</h2>
		{#if !r}
			<div class="skeleton h-64" aria-hidden="true"></div>
		{:else if r.timeseries.length === 0}
			<p class="py-10 text-sm text-ink-soft">No traffic in this range yet.</p>
		{:else}
			<TimeSeriesChart
				{x}
				{series}
				height={260}
				label="Views and visitors per {hourly ? 'hour' : 'day'}, last {range}"
				xFormat={(d) => (hourly ? hourFmt : dayFmt).format(d)}
				xTooltipFormat={(d) => (hourly ? hourTip : dayTip).format(d)}
			/>
			<details class="mt-3">
				<summary class="link inline-flex min-h-11 items-center text-sm text-ink-soft"
					>Show as a table</summary
				>
				<div class="max-h-80 overflow-y-auto">
					<table class="data-table">
						<thead>
							<tr>
								<th scope="col">{hourly ? 'Hour' : 'Day'}</th>
								<th scope="col" class="num">Views</th>
								<th scope="col" class="num">Visitors</th>
							</tr>
						</thead>
						<tbody>
							{#each r.timeseries as p (p.t)}
								<tr>
									<td class="font-mono text-sm"
										>{(hourly ? hourTip : dayTip).format(new Date(p.t))}</td
									>
									<td class="num font-mono">{p.views}</td>
									<td class="num font-mono">{p.visitors}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</details>
		{/if}
	</section>

	<div class="mt-10 grid gap-x-10 gap-y-8 md:grid-cols-2">
		{#if !r}
			{#each [1, 2, 3, 4] as i (i)}
				<div class="skeleton h-56" aria-hidden="true"></div>
			{/each}
		{:else}
			{#each tables as t (t.title)}
				<section aria-labelledby="bd-{t.title}">
					<h2 id="bd-{t.title}" class="mb-2 font-display text-xl font-semibold">{t.title}</h2>
					<BarTable rows={t.rows} caption="{t.title}, last {range}" keyLabel={t.keyLabel} />
				</section>
			{/each}
		{/if}
	</div>
{/if}
