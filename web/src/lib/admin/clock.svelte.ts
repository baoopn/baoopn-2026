import { createSubscriber } from 'svelte/reactivity';

/** A reactive "now" that only ticks while something reads it. */
class Clock {
	#subscribe: () => void;

	constructor(intervalMs: number) {
		this.#subscribe = createSubscriber((update) => {
			const id = setInterval(update, intervalMs);
			return () => clearInterval(id);
		});
	}

	get now(): number {
		this.#subscribe();
		return Date.now();
	}
}

/** 1 s ticks, for progress bars. */
export const secondClock = new Clock(1000);
/** 20 s ticks, for relative timestamps. */
export const relativeClock = new Clock(20_000);
