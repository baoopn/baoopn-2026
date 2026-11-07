/** Return a copy of `arr` with item `i` moved by `dir` (clamped). */
export function moveItem<T>(arr: T[], i: number, dir: -1 | 1): T[] {
	const j = i + dir;
	if (j < 0 || j >= arr.length) return arr;
	const next = [...arr];
	[next[i], next[j]] = [next[j], next[i]];
	return next;
}

export function removeItem<T>(arr: T[], i: number): T[] {
	return arr.filter((_, k) => k !== i);
}
