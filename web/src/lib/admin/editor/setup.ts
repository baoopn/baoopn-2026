// CodeMirror 6 setup for markdown: theme and highlighting from the CSS tokens,
// so palette and light/dark changes apply without reconfiguring.

import { markdown, markdownLanguage } from '@codemirror/lang-markdown';
import { languages } from '@codemirror/language-data';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { EditorView } from '@codemirror/view';
import { tags as t } from '@lezer/highlight';

export const editorTheme = EditorView.theme({
	'&': {
		backgroundColor: 'var(--paper)',
		color: 'var(--ink)',
		fontSize: '0.9375rem',
		height: '100%'
	},
	'&.cm-focused': { outline: 'none' },
	'.cm-scroller': { overflow: 'auto', fontFamily: 'var(--font-mono)', lineHeight: '1.65' },
	'.cm-content': { padding: '1rem 0', caretColor: 'var(--ink)' },
	'.cm-line': { padding: '0 1rem' },
	'.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--ink)', borderLeftWidth: '2px' },
	'&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground':
		{ backgroundColor: 'color-mix(in oklab, var(--spot-select) 55%, transparent)' },
	'.cm-placeholder': { color: 'var(--ink-soft)' },
	'.cm-activeLine': { backgroundColor: 'transparent' }
});

const highlight = HighlightStyle.define([
	{ tag: t.heading1, fontWeight: '700', fontSize: '1.3em' },
	{ tag: t.heading2, fontWeight: '700', fontSize: '1.18em' },
	{ tag: [t.heading3, t.heading4, t.heading5, t.heading6], fontWeight: '700' },
	{ tag: t.strong, fontWeight: '700' },
	{ tag: t.emphasis, fontStyle: 'italic' },
	{ tag: t.strikethrough, textDecoration: 'line-through' },
	{ tag: [t.link, t.url], color: 'var(--info)' },
	{ tag: t.quote, color: 'var(--ink-soft)', fontStyle: 'italic' },
	{ tag: [t.processingInstruction, t.contentSeparator, t.meta], color: 'var(--ink-soft)' },
	{ tag: t.monospace, color: 'var(--ok)' },
	// fenced code (language-data)
	{
		tag: [t.keyword, t.controlKeyword, t.moduleKeyword, t.operatorKeyword],
		color: 'var(--danger)'
	},
	{ tag: [t.string, t.special(t.string), t.regexp], color: 'var(--ok)' },
	{ tag: [t.number, t.bool, t.null, t.atom], color: 'var(--warn)' },
	{
		tag: [t.comment, t.lineComment, t.blockComment],
		color: 'var(--ink-soft)',
		fontStyle: 'italic'
	},
	{ tag: [t.function(t.variableName), t.function(t.propertyName)], color: 'var(--info)' },
	{ tag: [t.typeName, t.className, t.tagName], color: 'var(--warn)' },
	{ tag: [t.propertyName, t.attributeName], color: 'var(--info)' },
	{ tag: t.invalid, color: 'var(--danger)', textDecoration: 'underline wavy' }
]);

export const markdownSupport = [
	markdown({ base: markdownLanguage, codeLanguages: languages }),
	syntaxHighlighting(highlight),
	EditorView.lineWrapping,
	editorTheme
];
