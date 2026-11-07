<script lang="ts" module>
	export type Aspect = 'free' | 'original' | '16:9' | '4:3' | '1:1' | '3:2';
</script>

<script lang="ts">
	import { LoaderCircle, ZoomIn } from '@lucide/svelte';
	import { errorMessage } from '../format';
	import { loadPreview, toOriginal, type Crop, type Preview } from '../media/preview';
	import type { UploadJob } from '../media/upload.svelte';
	import Dialog from './Dialog.svelte';
	import Segmented from './Segmented.svelte';
	import UploadProgress from './UploadProgress.svelte';

	let {
		open = $bindable(false),
		file,
		aspect: initialAspect = 'free',
		title = 'Crop image',
		onconfirm,
		oncancel
	}: {
		open?: boolean;
		file: File | null;
		/** Preset selected when the dialog opens. */
		aspect?: Aspect;
		title?: string;
		/**
		 * Crop in ORIGINAL image pixels, or null to upload the full image. Return the
		 * started upload to show its progress here; the dialog closes once it is ready.
		 */
		onconfirm: (crop: Crop | null) => UploadJob | void;
		oncancel?: () => void;
	} = $props();

	const uid = $props.id();
	const MAX_ZOOM = 8;
	const presets: { value: Aspect; label: string }[] = [
		{ value: 'free', label: 'Free' },
		{ value: 'original', label: 'Original' },
		{ value: '16:9', label: '16:9' },
		{ value: '4:3', label: '4:3' },
		{ value: '1:1', label: '1:1' },
		{ value: '3:2', label: '3:2' }
	];

	let preview = $state.raw<Preview | null>(null);
	let loadError = $state<string | null>(null);
	let aspect = $state<Aspect>('free');
	let box = $state<Crop>({ x: 0, y: 0, w: 0, h: 0 });
	let stageW = $state(0);
	let stageH = $state(0);
	let job = $state.raw<UploadJob | null>(null);

	/**
	 * Loads (and decodes, for HEIC) the preview. Attached to the dialog body, which
	 * mounts each time the dialog opens, so every open starts fresh.
	 */
	function previewOf(f: File | null) {
		return () => {
			preview = null;
			loadError = null;
			job = null;
			if (!f) return;
			let alive = true;
			let loaded: Preview | null = null;
			loadPreview(f)
				.then((p) => {
					if (!alive) return p.revoke();
					loaded = p;
					preview = p;
					setAspect(initialAspect);
				})
				.catch((err) => {
					if (!alive) return;
					// Don't block: upload the full image straight away, with a notice.
					loadError = errorMessage(err);
					finish(null);
				});
			return () => {
				alive = false;
				loaded?.revoke();
			};
		};
	}

	const W = $derived(preview?.width ?? 1);
	const H = $derived(preview?.height ?? 1);
	const scale = $derived(stageW && stageH ? Math.min(stageW / W, stageH / H) : 0);

	function ratioOf(a: Aspect): number | null {
		if (a === 'free') return null;
		if (a === 'original') return W / H;
		const [x, y] = a.split(':').map(Number);
		return x / y;
	}

	/** Largest box of ratio `r` that fits the image. */
	function fit(r: number) {
		const w = Math.min(W, H * r);
		return { w, h: w / r };
	}

	function clamp(b: Crop): Crop {
		const w = Math.min(Math.max(b.w, 8), W);
		const h = Math.min(Math.max(b.h, 8), H);
		return {
			w,
			h,
			x: Math.min(Math.max(b.x, 0), W - w),
			y: Math.min(Math.max(b.y, 0), H - h)
		};
	}

	function setAspect(a: Aspect) {
		aspect = a;
		const r = ratioOf(a);
		const { w, h } = r ? fit(r) : { w: W, h: H };
		box = clamp({ x: (W - w) / 2, y: (H - h) / 2, w, h });
	}

	const zoom = $derived(box.w ? fit(box.w / box.h).w / box.w : 1);

	function setZoom(z: number) {
		const r = box.w / box.h;
		const f = fit(r);
		const zz = Math.min(Math.max(z, 1), MAX_ZOOM);
		const w = f.w / zz;
		const h = f.h / zz;
		const cx = box.x + box.w / 2;
		const cy = box.y + box.h / 2;
		box = clamp({ x: cx - w / 2, y: cy - h / 2, w, h });
	}

	/** Resize from the bottom-right corner, keeping the ratio unless Free. */
	function resize(dw: number, dh: number) {
		const r = ratioOf(aspect);
		let w = Math.min(box.w + dw, W - box.x);
		let h = r ? w / r : Math.min(box.h + dh, H - box.y);
		if (r && box.y + h > H) {
			h = H - box.y;
			w = h * r;
		}
		box = clamp({ ...box, w, h });
	}

	// ---- pointer: drag the box, resize from the handle, pinch to zoom ---------
	// Active pointers (not reactive: only read by the handlers below).
	let pointers: { id: number; x: number; y: number }[] = [];
	let drag: { mode: 'move' | 'resize'; x: number; y: number; box: Crop } | null = null;
	let pinch: { dist: number; zoom: number } | null = null;

	function spread() {
		const [a, b] = pointers;
		return Math.hypot(a.x - b.x, a.y - b.y);
	}

	function down(e: PointerEvent, mode: 'move' | 'resize') {
		e.stopPropagation();
		(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
		track(e);
		if (pointers.length === 2) {
			drag = null;
			pinch = { dist: spread(), zoom };
		} else {
			drag = { mode, x: e.clientX, y: e.clientY, box: { ...box } };
		}
	}

	function move(e: PointerEvent) {
		if (!scale || !pointers.some((p) => p.id === e.pointerId)) return;
		track(e);
		if (pinch && pointers.length === 2) return setZoom((pinch.zoom * spread()) / pinch.dist);
		if (!drag) return;
		const dx = (e.clientX - drag.x) / scale;
		const dy = (e.clientY - drag.y) / scale;
		if (drag.mode === 'move') box = clamp({ ...drag.box, x: drag.box.x + dx, y: drag.box.y + dy });
		else {
			const start = drag.box;
			box = start;
			resize(dx, dy);
		}
	}

	function track(e: PointerEvent) {
		const p = { id: e.pointerId, x: e.clientX, y: e.clientY };
		const i = pointers.findIndex((q) => q.id === p.id);
		if (i >= 0) pointers[i] = p;
		else pointers = [...pointers, p].slice(-2);
	}

	function up(e: PointerEvent) {
		pointers = pointers.filter((p) => p.id !== e.pointerId);
		if (pointers.length < 2) pinch = null;
		if (pointers.length === 0) drag = null;
	}

	function wheel(node: HTMLElement) {
		const on = (e: WheelEvent) => {
			e.preventDefault();
			setZoom(zoom * Math.exp(-e.deltaY * 0.002));
		};
		node.addEventListener('wheel', on, { passive: false });
		return () => node.removeEventListener('wheel', on);
	}

	function onkeydown(e: KeyboardEvent) {
		const step = (e.altKey ? 1 : 10) / Math.max(scale, 0.01);
		const d: Record<string, [number, number]> = {
			ArrowLeft: [-step, 0],
			ArrowRight: [step, 0],
			ArrowUp: [0, -step],
			ArrowDown: [0, step]
		};
		if (d[e.key]) {
			e.preventDefault();
			const [dx, dy] = d[e.key];
			if (e.shiftKey) resize(dx, dy);
			else box = clamp({ ...box, x: box.x + dx, y: box.y + dy });
		} else if (e.key === '+' || e.key === '=') {
			e.preventDefault();
			setZoom(zoom * 1.1);
		} else if (e.key === '-') {
			e.preventDefault();
			setZoom(zoom / 1.1);
		}
	}

	function finish(crop: Crop | null) {
		const started = onconfirm(crop);
		if (started) job = started;
		else open = false;
	}

	function cropAndUpload() {
		if (!preview) return;
		const full = box.x < 0.5 && box.y < 0.5 && box.w > W - 0.5 && box.h > H - 0.5;
		let crop: Crop | null = null;
		try {
			crop = full ? null : toOriginal(box, preview);
		} catch (err) {
			loadError = errorMessage(err); // fall back to the full image, with a notice
		}
		finish(crop);
	}

	function cancel() {
		open = false;
		dismissed();
	}

	/** Closing before an upload starts cancels; after, the upload carries on outside. */
	function dismissed() {
		if (!job) oncancel?.();
	}
</script>

<Dialog bind:open {title} size="lg" onclose={dismissed}>
	<div {@attach previewOf(file)}>
		{#if job}
			<div class="grid gap-3 py-4">
				{#if loadError}
					<p class="rounded-lg bg-spot-butter px-3 py-2 text-sm text-on-spot" role="status">
						This image couldn't be previewed for cropping ({loadError}), so the full image is
						uploading instead. The server still converts it to WebP.
					</p>
				{/if}
				<UploadProgress {job} onfinished={() => (open = false)} />
			</div>
		{:else if loadError}
			<div class="grid gap-2 py-6 text-center" role="alert">
				<p class="font-medium text-ink">This image can't be previewed for cropping.</p>
				<p class="text-sm text-ink-soft">{loadError}</p>
				<p class="text-sm text-ink-soft">
					You can still upload it; the server converts it to WebP.
				</p>
			</div>
		{:else}
			<div class="grid gap-3">
				<div class="overflow-x-auto">
					<Segmented
						options={presets}
						value={aspect}
						label="Aspect ratio"
						size="sm"
						onchange={setAspect}
					/>
				</div>
				<div
					class="stage grid h-[min(60dvh,30rem)] place-items-center rounded-lg bg-paper-3"
					bind:clientWidth={stageW}
					bind:clientHeight={stageH}
				>
					{#if preview && scale}
						<div
							class="relative overflow-hidden"
							style:width="{W * scale}px"
							style:height="{H * scale}px"
							{@attach wheel}
						>
							<img
								src={preview.url}
								alt=""
								class="pointer-events-none size-full select-none"
								draggable="false"
							/>
							<!-- A focusable, keyboard-operable crop widget: no native element or interactive ARIA role fits. -->
							<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
							<div
								class="crop-box absolute cursor-move"
								style:left="{box.x * scale}px"
								style:top="{box.y * scale}px"
								style:width="{box.w * scale}px"
								style:height="{box.h * scale}px"
								role="application"
								tabindex="0"
								aria-roledescription="crop area"
								aria-label="Crop area"
								aria-describedby="{uid}-help"
								onpointerdown={(e) => down(e, 'move')}
								onpointermove={move}
								onpointerup={up}
								onpointercancel={up}
								{onkeydown}
							>
								<span
									class="handle absolute -right-[22px] -bottom-[22px] grid size-11 cursor-nwse-resize place-items-center"
									aria-hidden="true"
									onpointerdown={(e) => down(e, 'resize')}
									onpointermove={move}
									onpointerup={up}
									onpointercancel={up}
									><span class="size-3.5 rounded-sm border-2 border-paper bg-ink"></span></span
								>
							</div>
						</div>
					{:else}
						<LoaderCircle class="size-6 animate-spin text-ink-soft" aria-label="Loading preview" />
					{/if}
				</div>
				<label class="flex min-h-11 items-center gap-3 text-sm" for="{uid}-zoom">
					<ZoomIn class="size-4 shrink-0 text-ink-soft" aria-hidden="true" />
					<span class="sr-only">Zoom</span>
					<input
						id="{uid}-zoom"
						type="range"
						class="h-11 flex-1"
						min="1"
						max={MAX_ZOOM}
						step="0.01"
						value={zoom}
						disabled={!preview}
						oninput={(e) => setZoom(+e.currentTarget.value)}
					/>
					<span class="w-12 text-right font-mono text-xs text-ink-soft">{zoom.toFixed(1)}×</span>
				</label>
				<p id="{uid}-help" class="hint">
					Drag to move, drag the corner to resize, scroll or pinch to zoom. Keyboard: arrows move,
					Shift+arrows resize, Alt for 1px steps, + and − zoom.
					{#if preview}<span class="font-mono">
							{Math.round(box.w * preview.scaleX)} × {Math.round(box.h * preview.scaleY)} px</span
						>{/if}
				</p>
			</div>
		{/if}
	</div>

	{#snippet footer()}
		{#if job}
			<button type="button" class="btn btn-ghost" onclick={() => (open = false)}>
				{job.stage === 'ready' ? 'Close' : 'Continue in background'}
			</button>
		{:else}
			{@render actions()}
		{/if}
	{/snippet}
</Dialog>

{#snippet actions()}
	<button type="button" class="btn btn-ghost" onclick={cancel}>Cancel</button>
	<button type="button" class="btn btn-secondary" onclick={() => finish(null)}>
		{loadError ? 'Upload without cropping' : 'Use full image'}
	</button>
	{#if !loadError}
		<button type="button" class="btn btn-primary" disabled={!preview} onclick={cropAndUpload}>
			Crop and upload
		</button>
	{/if}
{/snippet}

<style>
	.stage > div {
		touch-action: none;
	}
	.crop-box {
		box-shadow: 0 0 0 9999px color-mix(in oklab, black 55%, transparent);
		outline: 2px solid white;
		touch-action: none;
	}
	.crop-box:focus-visible {
		outline: 3px solid var(--spot-select, white);
	}
	.handle {
		touch-action: none;
	}
</style>
