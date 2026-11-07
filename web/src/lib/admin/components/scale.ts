/** Round, human-friendly ticks from 0 to >= max. */
export function niceTicks(max: number, count = 4): number[] {
	if (!Number.isFinite(max) || max <= 0) return [0, 1];
	const raw = max / count;
	const mag = 10 ** Math.floor(Math.log10(raw));
	const norm = raw / mag;
	const step = (norm <= 1 ? 1 : norm <= 2 ? 2 : norm <= 2.5 ? 2.5 : norm <= 5 ? 5 : 10) * mag;
	const top = Math.ceil(max / step) * step;
	const ticks: number[] = [];
	for (let v = 0; v <= top + step / 2; v += step) ticks.push(Number(v.toPrecision(12)));
	return ticks;
}

/** Evenly spaced indices (always including the last) for at most `max` labels. */
export function tickIndices(n: number, max: number): number[] {
	if (n <= 0) return [];
	if (n <= max) return Array.from({ length: n }, (_, i) => i);
	const step = Math.ceil((n - 1) / (max - 1));
	const out: number[] = [];
	for (let i = 0; i < n; i += step) out.push(i);
	if (out[out.length - 1] !== n - 1) {
		// keep the last tick, drop the previous one if too close
		if (n - 1 - out[out.length - 1] < step / 2) out.pop();
		out.push(n - 1);
	}
	return out;
}
