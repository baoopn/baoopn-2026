export function debounce<A extends unknown[]>(fn: (...args: A) => void, wait = 300) {
	let t: ReturnType<typeof setTimeout> | undefined;
	const debounced = (...args: A) => {
		if (t) clearTimeout(t);
		t = setTimeout(() => fn(...args), wait);
	};
	debounced.cancel = () => {
		if (t) clearTimeout(t);
	};
	return debounced;
}
