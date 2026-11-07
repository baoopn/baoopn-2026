import { errorMessage } from './format';

/**
 * A tiny async loader: `data`, `error`, `loading`. Calling `load()` inside an
 * `$effect` re-runs it whenever reactive values the fetcher reads (before its
 * first `await`) change. Stale responses are dropped.
 */
export class Resource<T> {
	data = $state.raw<T | undefined>(undefined);
	error = $state<string | null>(null);
	loading = $state(false);

	#fetcher: () => Promise<T>;
	#seq = 0;

	constructor(fetcher: () => Promise<T>) {
		this.#fetcher = fetcher;
	}

	/** `silent` keeps the current data on screen (polling, refresh after an action). */
	async load(silent = false): Promise<void> {
		const seq = ++this.#seq;
		if (!silent) this.loading = true;
		let promise: Promise<T>;
		try {
			promise = this.#fetcher();
		} catch (err) {
			this.error = errorMessage(err);
			this.loading = false;
			return;
		}
		try {
			const d = await promise;
			if (seq !== this.#seq) return;
			this.data = d;
			this.error = null;
		} catch (err) {
			if (seq !== this.#seq) return;
			// A failed background refresh keeps the last good data.
			if (!silent || this.data === undefined) this.error = errorMessage(err);
		} finally {
			if (seq === this.#seq) this.loading = false;
		}
	}

	/** Replace the data locally (optimistic updates). */
	set(d: T): void {
		this.data = d;
	}
}
