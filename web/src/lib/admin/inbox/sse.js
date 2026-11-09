// Incremental text/event-stream parser (WHATWG rules, like internal/listening/sse.go).
// Plain JS with JSDoc types and no $app imports, so `node --test` can load it.

/** Cap for one line and for all data: lines of one event (1 MiB, in UTF-16 units). */
export const MAX_SSE_EVENT = 1 << 20;

/**
 * @typedef {object} SSEParser
 * @property {(chunk: string) => void} push Feed decoded text; may hold a partial line.
 * @property {() => void} end The stream ended: drop any partial line or event (no dispatch).
 */

/**
 * Create a parser that calls `onEvent(type, data)` for each dispatched event.
 * `type` is "message" when no event: field was sent. Comments, id: and retry:
 * are consumed and ignored (the client never replays and has its own backoff).
 * @param {(type: string, data: string) => void} onEvent
 * @returns {SSEParser}
 */
export function createSSEParser(onEvent) {
	let buf = '';
	let skipLF = false; // the previous chunk ended in CR: a leading LF belongs to it
	let started = false;
	let lineTooLong = false; // the current (unterminated) line is being discarded

	let type = '';
	let data = '';
	let hasData = false;
	let tooBig = false;

	function dispatch() {
		const t = type || 'message';
		const d = data;
		const ok = hasData && !tooBig;
		type = '';
		data = '';
		hasData = false;
		tooBig = false;
		if (ok) onEvent(t, d);
	}

	/** @param {string} line */
	function line(line) {
		if (!started) {
			started = true;
			if (line.charCodeAt(0) === 0xfeff) line = line.slice(1);
		}
		if (line === '') return dispatch();
		if (line[0] === ':') return; // comment, e.g. ": keep-alive"
		const i = line.indexOf(':');
		const field = i < 0 ? line : line.slice(0, i);
		let value = i < 0 ? '' : line.slice(i + 1);
		if (value[0] === ' ') value = value.slice(1);
		if (field === 'event') {
			type = value;
		} else if (field === 'data') {
			if (tooBig) return;
			if (data.length + value.length + 1 > MAX_SSE_EVENT) {
				tooBig = true; // dropped at dispatch
				data = '';
				return;
			}
			data = hasData ? data + '\n' + value : value;
			hasData = true;
		}
		// id:, retry: and unknown fields are ignored.
	}

	return {
		push(chunk) {
			if (!chunk) return;
			if (skipLF) {
				skipLF = false;
				if (chunk[0] === '\n') chunk = chunk.slice(1);
			}
			buf += chunk;
			let start = 0;
			for (;;) {
				const cr = buf.indexOf('\r', start);
				const lf = buf.indexOf('\n', start);
				const end = cr < 0 ? lf : lf < 0 ? cr : Math.min(cr, lf);
				if (end < 0) break;
				const text = buf.slice(start, end);
				if (lineTooLong) lineTooLong = false;
				else line(text);
				start = end + 1;
				if (buf[end] === '\r') {
					if (start < buf.length) {
						if (buf[start] === '\n') start++;
					} else {
						skipLF = true;
					}
				}
			}
			buf = buf.slice(start);
			if (buf.length > MAX_SSE_EVENT) {
				// One line over the cap: discard it up to its terminator, and drop
				// the event it belongs to rather than dispatch it with partial data.
				buf = '';
				lineTooLong = true;
				tooBig = true;
				data = '';
			}
		},
		end() {
			buf = '';
			skipLF = false;
			lineTooLong = false;
			type = '';
			data = '';
			hasData = false;
			tooBig = false;
		}
	};
}
