// Browser-side image previews for the cropper. The ORIGINAL file is always what
// gets uploaded; previews exist only so the user can pick a crop rectangle.

import type { Crop } from '../api';
export type { Crop };

export type Preview = {
	url: string;
	/** Preview size in pixels (EXIF orientation applied). */
	width: number;
	height: number;
	/** Multiply preview pixels by these to get ORIGINAL image pixels. */
	scaleX: number;
	scaleY: number;
	revoke: () => void;
};

/** Accept for every image input: browsers don't always map .heic to image/*. */
export const IMAGE_ACCEPT = 'image/*,.heic,.heif,image/heic,image/heif';

const HEIC_TYPE = /^image\/hei[cf](-sequence)?$/i;
const HEIC_NAME = /\.hei[cf]$/i;

export function isHeic(file: File): boolean {
	return HEIC_TYPE.test(file.type) || HEIC_NAME.test(file.name);
}

export function isImageFile(file: File): boolean {
	return file.type.startsWith('image/') || isHeic(file);
}

function loadImage(url: string): Promise<HTMLImageElement> {
	return new Promise((resolve, reject) => {
		const img = new Image();
		img.onload = () => (img.naturalWidth > 0 ? resolve(img) : reject(new Error('Empty image')));
		img.onerror = () => reject(new Error('The browser cannot display this image'));
		img.src = url;
	});
}

/**
 * Reads the primary image size from the HEIF `ispe` boxes (largest one wins:
 * the others belong to thumbnails/tiles). Returns null when none is found.
 */
async function heifSize(file: File): Promise<{ w: number; h: number } | null> {
	const buf = new Uint8Array(await file.slice(0, 256 * 1024).arrayBuffer());
	const dv = new DataView(buf.buffer);
	let best: { w: number; h: number } | null = null;
	for (let i = 4; i + 16 <= buf.length; i++) {
		// 'ispe' = 69 73 70 65, then 4 bytes version/flags, u32 width, u32 height
		if (buf[i] !== 0x69 || buf[i + 1] !== 0x73 || buf[i + 2] !== 0x70 || buf[i + 3] !== 0x65)
			continue;
		const w = dv.getUint32(i + 8);
		const h = dv.getUint32(i + 12);
		if (!best || w * h > best.w * best.h) best = { w, h };
	}
	return best;
}

async function decodeHeic(file: File): Promise<string> {
	const { heicTo } = await import('heic-to');
	const blob = await heicTo({ blob: file, type: 'image/jpeg', quality: 0.85 });
	return URL.createObjectURL(blob);
}

/** Loads a displayable preview; HEIC/HEIF is decoded in the browser when needed. */
export async function loadPreview(file: File): Promise<Preview> {
	let url = URL.createObjectURL(file);
	let img: HTMLImageElement;
	let decoded = false;
	try {
		img = await loadImage(url);
	} catch (err) {
		URL.revokeObjectURL(url);
		if (!isHeic(file)) throw err;
		url = await decodeHeic(file);
		decoded = true;
		img = await loadImage(url).catch((e) => {
			URL.revokeObjectURL(url);
			throw e;
		});
	}
	const width = img.naturalWidth;
	const height = img.naturalHeight;
	let scaleX = 1;
	let scaleY = 1;
	if (decoded) {
		// libheif applies irot/imir, so the decode should match the original's oriented size.
		// If it doesn't (downscaled decode), map back via the stored size, matching orientation.
		const s = await heifSize(file).catch(() => null);
		if (s) {
			const swap = s.w > s.h !== width > height && s.w !== s.h;
			const ow = swap ? s.h : s.w;
			const oh = swap ? s.w : s.h;
			if (Math.abs(ow / width - 1) > 0.01 || Math.abs(oh / height - 1) > 0.01) {
				scaleX = ow / width;
				scaleY = oh / height;
			}
		}
	}
	const final = url;
	return {
		url,
		width,
		height,
		scaleX,
		scaleY,
		revoke: () => URL.revokeObjectURL(final)
	};
}

/** Preview-pixel crop → integer ORIGINAL-pixel crop. */
export function toOriginal(c: Crop, p: Pick<Preview, 'scaleX' | 'scaleY'>): Crop {
	return {
		x: Math.round(c.x * p.scaleX),
		y: Math.round(c.y * p.scaleY),
		w: Math.round(c.w * p.scaleX),
		h: Math.round(c.h * p.scaleY)
	};
}
