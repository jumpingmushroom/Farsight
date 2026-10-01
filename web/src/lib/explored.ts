// The explored mask (spec 2026-10-01 §1): the game's 2048×2048 minimap
// grid, 12 m per cell. Cell (px, py) = (round(x/12) + 1024, round(z/12) +
// 1024) with banker's rounding (Unity's Mathf.RoundToInt). The server sends
// it gzip'd and base64'd; the fog itself is drawn into the tiles
// server-side, so the browser only uses the mask for search, marker
// filtering and the cursor readout.

import type { Explored } from './types';

export const SIZE = 2048;
export const CELL = 12;
const HALF = SIZE / 2;
export const MASK_BYTES = (SIZE * SIZE) / 8;

/** Round half to even, as Unity's Mathf.RoundToInt (and Go's math.RoundToEven). */
export function roundHalfEven(v: number): number {
	const f = Math.floor(v);
	const d = v - f;
	if (d > 0.5) return f + 1;
	if (d < 0.5) return f;
	return f % 2 === 0 ? f : f + 1;
}

/** The cell under world point (x, z); it may lie off the grid. */
export function cellOf(x: number, z: number): [number, number] {
	return [roundHalfEven(x / CELL) + HALF, roundHalfEven(z / CELL) + HALF];
}

/** An empty mask (all unexplored). */
export function emptyMask(): Uint8Array<ArrayBuffer> {
	return new Uint8Array(MASK_BYTES);
}

/** Marks cell (px, py) explored; off-grid cells are ignored. */
export function setCell(mask: Uint8Array, px: number, py: number): void {
	if (px < 0 || px >= SIZE || py < 0 || py >= SIZE) return;
	const i = py * SIZE + px;
	mask[i >> 3] |= 1 << (i & 7);
}

/** Whether the cell under world point (x, z) is explored; off-grid is not. */
export function isExplored(mask: Uint8Array, x: number, z: number): boolean {
	const [px, py] = cellOf(x, z);
	if (px < 0 || px >= SIZE || py < 0 || py >= SIZE) return false;
	const i = py * SIZE + px;
	return (mask[i >> 3] & (1 << (i & 7))) !== 0;
}

/**
 * Decodes the snapshot API's `explored` (base64 of gzip of the bitset).
 * Undefined when it is missing, malformed, or the browser lacks
 * DecompressionStream: the server has already filtered the pins, so the
 * mask only refines search and the cursor readout.
 */
export async function decodeExplored(e: Explored | undefined): Promise<Uint8Array | undefined> {
	if (!e || e.cell !== CELL || e.size !== SIZE || !e.bits || typeof DecompressionStream !== 'function') return undefined;
	try {
		const bin = atob(e.bits);
		const gz = Uint8Array.from(bin, (c) => c.charCodeAt(0));
		const stream = new Blob([gz]).stream().pipeThrough(new DecompressionStream('gzip'));
		const out = new Uint8Array(await new Response(stream).arrayBuffer());
		return out.length === MASK_BYTES ? out : undefined;
	} catch {
		return undefined;
	}
}
