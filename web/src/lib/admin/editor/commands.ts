// Markdown editing commands for the CodeMirror editor toolbar + shortcuts.

import { EditorSelection, type ChangeSpec } from '@codemirror/state';
import type { EditorView, KeyBinding } from '@codemirror/view';

/** Wrap each selection in `before`/`after`, or unwrap it when already wrapped. */
export function wrap(view: EditorView, before: string, after = before, filler = 'text'): boolean {
	const { state } = view;
	const tr = state.changeByRange((range) => {
		const text = state.sliceDoc(range.from, range.to);
		const outer = state.sliceDoc(range.from - before.length, range.to + after.length);
		if (text && outer === before + text + after) {
			return {
				changes: [
					{ from: range.from - before.length, to: range.from },
					{ from: range.to, to: range.to + after.length }
				],
				range: EditorSelection.range(range.from - before.length, range.to - before.length)
			};
		}
		const inner = text || filler;
		return {
			changes: { from: range.from, to: range.to, insert: before + inner + after },
			range: EditorSelection.range(
				range.from + before.length,
				range.from + before.length + inner.length
			)
		};
	});
	view.dispatch(state.update(tr, { scrollIntoView: true, userEvent: 'input' }));
	view.focus();
	return true;
}

/** Toggle a prefix (e.g. "> ", "- ") on every line touched by the selection. */
export function linePrefix(view: EditorView, prefix: string | ((i: number) => string)): boolean {
	const { state } = view;
	const changes: ChangeSpec[] = [];
	const seen = new Set<number>();
	for (const range of state.selection.ranges) {
		const first = state.doc.lineAt(range.from).number;
		const last = state.doc.lineAt(range.to).number;
		const lines = [];
		for (let n = first; n <= last; n++) if (!seen.has(n)) lines.push(state.doc.line(n));
		lines.forEach((l) => seen.add(l.number));
		const p = (i: number) => (typeof prefix === 'string' ? prefix : prefix(i));
		const all = lines.every((l, i) => l.text.startsWith(p(i)));
		lines.forEach((l, i) => {
			if (all) changes.push({ from: l.from, to: l.from + p(i).length });
			else changes.push({ from: l.from, insert: p(i) });
		});
	}
	view.dispatch({ changes, scrollIntoView: true, userEvent: 'input' });
	view.focus();
	return true;
}

/** Cycle the current line(s) through no heading -> ## -> ### -> none. */
export function heading(view: EditorView): boolean {
	const { state } = view;
	const changes: ChangeSpec[] = [];
	for (const range of state.selection.ranges) {
		const line = state.doc.lineAt(range.from);
		const m = /^(#{1,6})\s/.exec(line.text);
		const level = m ? m[1].length : 0;
		const next = level === 0 ? '## ' : level === 2 ? '### ' : '';
		changes.push({ from: line.from, to: line.from + (m ? m[0].length : 0), insert: next });
	}
	view.dispatch({ changes, scrollIntoView: true, userEvent: 'input' });
	view.focus();
	return true;
}

/** Insert a block on its own lines at the cursor, selecting `select` within it. */
export function insertBlock(view: EditorView, text: string, select?: string): boolean {
	const { state } = view;
	const range = state.selection.main;
	const before = state.sliceDoc(Math.max(0, range.from - 2), range.from);
	const lead = range.from === 0 || before === '\n\n' ? '' : before.endsWith('\n') ? '\n' : '\n\n';
	const insert = lead + text + '\n';
	const start = range.from + lead.length;
	const idx = select ? text.indexOf(select) : -1;
	view.dispatch({
		changes: { from: range.from, to: range.to, insert },
		selection:
			idx >= 0
				? EditorSelection.range(start + idx, start + idx + select!.length)
				: EditorSelection.cursor(start + text.length),
		scrollIntoView: true,
		userEvent: 'input'
	});
	view.focus();
	return true;
}

export function link(view: EditorView): boolean {
	const { state } = view;
	const range = state.selection.main;
	const text = state.sliceDoc(range.from, range.to) || 'link text';
	const insert = `[${text}](https://)`;
	const urlStart = range.from + text.length + 3;
	view.dispatch({
		changes: { from: range.from, to: range.to, insert },
		selection: EditorSelection.range(urlStart, urlStart + 'https://'.length),
		scrollIntoView: true,
		userEvent: 'input'
	});
	view.focus();
	return true;
}

export function codeBlock(view: EditorView): boolean {
	const range = view.state.selection.main;
	const sel = view.state.sliceDoc(range.from, range.to);
	return insertBlock(view, '```ts\n' + (sel || 'code') + '\n```', sel ? undefined : 'code');
}

export const TABLE = '| Column | Column |\n| ------ | ------ |\n| Cell   | Cell   |';

export function table(view: EditorView): boolean {
	return insertBlock(view, TABLE, 'Column');
}

export const bold = (v: EditorView) => wrap(v, '**', '**', 'bold');
export const italic = (v: EditorView) => wrap(v, '_', '_', 'italic');
export const code = (v: EditorView) => wrap(v, '`', '`', 'code');
export const bullet = (v: EditorView) => linePrefix(v, '- ');
export const ordered = (v: EditorView) => linePrefix(v, (i) => `${i + 1}. `);
export const quote = (v: EditorView) => linePrefix(v, '> ');

export const markdownShortcuts: KeyBinding[] = [
	{ key: 'Mod-b', run: bold },
	{ key: 'Mod-i', run: italic },
	{ key: 'Mod-k', run: link },
	{ key: 'Mod-e', run: code },
	{ key: 'Mod-Alt-h', run: heading },
	{ key: 'Mod-Shift-8', run: bullet },
	{ key: 'Mod-Shift-7', run: ordered },
	{ key: 'Mod-Shift-9', run: quote },
	{ key: 'Mod-Alt-c', run: codeBlock }
];
