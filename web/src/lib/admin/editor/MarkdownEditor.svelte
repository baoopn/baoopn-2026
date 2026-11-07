<script lang="ts">
	import {
		Bold,
		Code,
		Columns2,
		Eye,
		Heading,
		Image,
		Italic,
		Link,
		List,
		ListOrdered,
		LoaderCircle,
		PenLine,
		Quote,
		SquareCode,
		Table
	} from '@lucide/svelte';
	import { minimalSetup } from 'codemirror';
	import { EditorView, keymap, placeholder as cmPlaceholder } from '@codemirror/view';
	import { untrack } from 'svelte';
	import { MediaQuery } from 'svelte/reactivity';
	import type { PreviewResult } from '#lib/api/types.js';
	import Segmented from '../components/Segmented.svelte';
	import { post, uploadMedia } from '../api';
	import { errorMessage } from '../format';
	import { toasts } from '../toast.svelte';
	import { markdownSupport } from './setup';
	import * as cmd from './commands';

	type Mode = 'write' | 'split' | 'preview';

	let {
		value = $bindable(''),
		label,
		id,
		placeholder = 'Write in Markdown…',
		preview = true,
		height = '28rem',
		onpreview
	}: {
		value?: string;
		/** Accessible name of the editor. */
		label: string;
		id?: string;
		placeholder?: string;
		/** Offer Write / Split / Preview modes, rendered by the API. */
		preview?: boolean;
		/** CSS height of the editing area. */
		height?: string;
		onpreview?: (r: PreviewResult) => void;
	} = $props();

	const uid = $props.id();
	const wide = new MediaQuery('min-width: 1024px');
	// svelte-ignore state_referenced_locally
	let mode = $state<Mode>(preview && wide.current ? 'split' : 'write');
	let view = $state.raw<EditorView | null>(null);
	const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
	const mod = isMac ? '⌘' : 'Ctrl';

	// ---- uploads -------------------------------------------------------------
	let uploadSeq = 0;
	let uploading = $state(0);

	function altFrom(name: string): string {
		return name
			.replace(/\.[^.]+$/, '')
			.replace(/[-_]+/g, ' ')
			.replace(/[[\]]/g, '');
	}

	async function uploadImages(files: File[], at?: number) {
		const v = view;
		if (!v) return;
		for (const file of files) {
			if (!file.type.startsWith('image/')) {
				toasts.warning('Only images can be inserted', file.name);
				continue;
			}
			const token = `![Uploading ${altFrom(file.name)}…](#upload-${++uploadSeq})`;
			const pos = at ?? v.state.selection.main.head;
			v.dispatch({ changes: { from: pos, insert: token + '\n' }, userEvent: 'input' });
			at = undefined;
			uploading++;
			try {
				const m = await uploadMedia(file);
				replaceToken(token, `![${altFrom(file.name)}](${m.url})`);
			} catch (err) {
				replaceToken(token + '\n', '');
				toasts.error('Image upload failed', errorMessage(err));
			} finally {
				uploading--;
			}
		}
	}

	function replaceToken(token: string, insert: string) {
		if (!view) return;
		const at = view.state.doc.toString().indexOf(token);
		if (at < 0) return;
		view.dispatch({ changes: { from: at, to: at + token.length, insert } });
	}

	function imageFiles(list: FileList | null | undefined): File[] {
		return list ? [...list].filter((f) => f.type.startsWith('image/')) : [];
	}

	// ---- editor --------------------------------------------------------------
	function editor(node: HTMLDivElement) {
		const v = new EditorView({
			parent: node,
			doc: untrack(() => value),
			extensions: [
				minimalSetup,
				keymap.of(cmd.markdownShortcuts),
				markdownSupport,
				cmPlaceholder(untrack(() => placeholder)),
				EditorView.contentAttributes.of({
					'aria-label': untrack(() => label),
					'aria-multiline': 'true',
					...(untrack(() => id) ? { id: untrack(() => id)! } : {})
				}),
				EditorView.updateListener.of((u) => {
					if (u.docChanged) value = u.state.doc.toString();
				}),
				EditorView.domEventHandlers({
					paste(e) {
						const files = imageFiles(e.clipboardData?.files);
						if (files.length === 0) return false;
						e.preventDefault();
						void uploadImages(files);
						return true;
					},
					drop(e, ev) {
						const files = imageFiles(e.dataTransfer?.files);
						if (files.length === 0) return false;
						e.preventDefault();
						const pos = ev.posAtCoords({ x: e.clientX, y: e.clientY }) ?? undefined;
						void uploadImages(files, pos);
						return true;
					}
				})
			]
		});
		view = v;
		return () => {
			v.destroy();
			view = null;
		};
	}

	// External value changes (load, restore draft) flow into the editor.
	$effect(() => {
		const v = view;
		const next = value;
		if (v && next !== v.state.doc.toString()) {
			v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: next } });
		}
	});

	function run(fn: (v: EditorView) => boolean) {
		if (view) fn(view);
	}

	// ---- preview -------------------------------------------------------------
	let html = $state('');
	let previewBusy = $state(false);
	let previewError = $state<string | null>(null);
	let previewSeq = 0;

	async function render(md: string) {
		const seq = ++previewSeq;
		previewBusy = true;
		try {
			const r = await post<PreviewResult>('/posts/preview', { markdown: md });
			if (seq !== previewSeq) return;
			html = r.html;
			previewError = null;
			onpreview?.(r);
		} catch (err) {
			if (seq === previewSeq) previewError = errorMessage(err);
		} finally {
			if (seq === previewSeq) previewBusy = false;
		}
	}

	$effect(() => {
		if (!preview || mode === 'write') return;
		const md = value;
		const t = setTimeout(() => void render(md), 400);
		return () => clearTimeout(t);
	});

	const tools = [
		{ label: 'Heading', keys: `${mod}+Alt+H`, icon: Heading, fn: cmd.heading },
		{ label: 'Bold', keys: `${mod}+B`, icon: Bold, fn: cmd.bold },
		{ label: 'Italic', keys: `${mod}+I`, icon: Italic, fn: cmd.italic },
		{ label: 'Link', keys: `${mod}+K`, icon: Link, fn: cmd.link },
		{ label: 'Inline code', keys: `${mod}+E`, icon: Code, fn: cmd.code },
		{ label: 'Code block', keys: `${mod}+Alt+C`, icon: SquareCode, fn: cmd.codeBlock },
		{ label: 'Bulleted list', keys: `${mod}+Shift+8`, icon: List, fn: cmd.bullet },
		{ label: 'Numbered list', keys: `${mod}+Shift+7`, icon: ListOrdered, fn: cmd.ordered },
		{ label: 'Quote', keys: `${mod}+Shift+9`, icon: Quote, fn: cmd.quote },
		{ label: 'Table', keys: '', icon: Table, fn: cmd.table }
	];

	const modes: { value: Mode; label: string; icon: typeof Eye }[] = [
		{ value: 'write', label: 'Write', icon: PenLine },
		{ value: 'split', label: 'Split', icon: Columns2 },
		{ value: 'preview', label: 'Preview', icon: Eye }
	];
</script>

<div class="md-editor overflow-hidden rounded-[10px] border border-rule-strong bg-paper">
	<div
		class="flex flex-wrap items-center gap-1 border-b border-rule bg-paper-2 px-1.5 py-1"
		role="toolbar"
		aria-label="{label} formatting"
	>
		{#if mode !== 'preview'}
			{#each tools as tool (tool.label)}
				<button
					type="button"
					class="btn btn-ghost btn-icon btn-sm"
					title={tool.keys ? `${tool.label} (${tool.keys})` : tool.label}
					aria-label={tool.label}
					onclick={() => run(tool.fn)}
				>
					<tool.icon class="size-4" aria-hidden="true" />
				</button>
			{/each}
			<label
				class="btn btn-ghost btn-icon btn-sm focus-within:outline-2 focus-within:outline-ink"
				title="Insert image (or paste / drop one)"
			>
				<input
					type="file"
					accept="image/*"
					multiple
					class="sr-only"
					aria-label="Insert image"
					onchange={(e) => {
						const input = e.currentTarget;
						const files = imageFiles(input.files);
						input.value = '';
						void uploadImages(files);
					}}
				/>
				<Image class="size-4" aria-hidden="true" />
			</label>
		{/if}
		{#if uploading > 0}
			<span class="eyebrow ml-1 inline-flex items-center gap-1.5" role="status">
				<LoaderCircle class="size-3.5 animate-spin" aria-hidden="true" /> Uploading {uploading}
			</span>
		{/if}
		{#if preview}
			<div class="ml-auto">
				<Segmented options={modes} bind:value={mode} label="{label} view" size="sm" />
			</div>
		{/if}
	</div>

	<div
		class={['grid', mode === 'split' && 'lg:grid-cols-2']}
		style:height={mode === 'split' && !wide.current ? undefined : height}
	>
		<div
			class={[
				'editor-host min-h-0 overflow-hidden',
				mode === 'preview' && 'hidden',
				mode === 'split' && 'border-rule lg:border-r'
			]}
			style:height={mode === 'split' && !wide.current ? height : undefined}
			{@attach editor}
		></div>
		{#if preview && mode !== 'write'}
			<div
				class={[
					'relative min-h-0 overflow-y-auto bg-paper-2 px-5 py-4',
					mode === 'split' && 'max-lg:border-t max-lg:border-rule'
				]}
				style:height={mode === 'split' && !wide.current ? height : undefined}
				aria-label="{label} preview"
				aria-busy={previewBusy}
				role="region"
				id="{uid}-preview"
			>
				{#if previewError}
					<p class="text-danger text-sm" role="alert">Preview failed: {previewError}</p>
				{:else if !html && previewBusy}
					<div class="flex flex-col gap-3" aria-hidden="true">
						<span class="skeleton h-6 w-1/2"></span>
						<span class="skeleton h-4 w-full"></span>
						<span class="skeleton h-4 w-4/5"></span>
					</div>
				{:else if !value.trim()}
					<p class="text-sm text-ink-soft">Nothing to preview yet.</p>
				{:else}
					<div class="prose">
						<!-- eslint-disable-next-line svelte/no-at-html-tags -- HTML rendered and sanitised by our API -->
						{@html html}
					</div>
				{/if}
			</div>
		{/if}
	</div>
</div>

<style>
	.md-editor:focus-within {
		outline: 2px solid var(--ink);
		outline-offset: 2px;
	}
	.editor-host :global(.cm-editor) {
		height: 100%;
	}
</style>
