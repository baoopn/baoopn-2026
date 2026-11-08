import type { Brand, BrandSlot } from '#lib/api/types.js';

export type { BrandSlot };

/**
 * URL of a brand asset. Falls back to the API redirect, which always points
 * at the current file, when the brand doc could not be loaded.
 */
export function brandSrc(brand: Brand | null | undefined, slot: BrandSlot): string {
	return brand?.assets[slot]?.url ?? '/api/v1/brand/' + slot;
}

/** Width and height of a brand asset, or the given fallback. */
export function brandSize(
	brand: Brand | null | undefined,
	slot: BrandSlot,
	width: number,
	height: number
): { width: number; height: number } {
	const a = brand?.assets[slot];
	return a && a.width > 0 && a.height > 0 ? { width: a.width, height: a.height } : { width, height };
}
