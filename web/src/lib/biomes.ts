// The base-biome grid for the cursor readout (Task 4, spec 2026-10-05): a
// 1024×1024 byte grid served by GET /tiles/{id}/{key}/biomes
// (internal/biomegrid), one byte per 20 m cell sampled at its centre. Cell
// (gx, gz) = (floor(x/20)+512, floor(z/20)+512), index gz*1024+gx (row 0 is
// the south edge); byte 0 means "no biome" (so does off-grid).

import { isExplored } from './explored';
import { insideWorld } from './geo';

export const GRID_SIZE = 1024;
const CELL = 20;
const HALF = GRID_SIZE / 2;
export const GRID_BYTES = GRID_SIZE * GRID_SIZE;

/** Index -> display name (internal/biomegrid's indexOf); '' at 0 (no biome). */
export const BIOME_NAMES: string[] = [
	'',
	'Meadows',
	'Black Forest',
	'Swamp',
	'Mountains',
	'Plains',
	'Mistlands',
	'Ashlands',
	'Deep North',
	'Ocean'
];

/** The biome name at world point (x, z); '' off-grid or where the grid has no biome (byte 0). */
export function biomeAt(grid: Uint8Array, x: number, z: number): string {
	const gx = Math.floor(x / CELL) + HALF;
	const gz = Math.floor(z / CELL) + HALF;
	if (gx < 0 || gx >= GRID_SIZE || gz < 0 || gz >= GRID_SIZE) return '';
	return BIOME_NAMES[grid[gz * GRID_SIZE + gx]] ?? '';
}

/**
 * The cursor readout's status slot (ScaleReadout, DESIGN-NOTES §5.8), in
 * order: "Hover the map" with no cursor, "World edge" outside the world
 * disc, "Unexplored" when a mask is loaded and the 12 m cell under the
 * cursor isn't explored, else the biome name from `grid` once it has
 * loaded, else '' while it hasn't.
 */
export function placeLabel(
	cursor: { x: number; z: number } | undefined,
	{ mask, grid }: { mask?: Uint8Array; grid?: Uint8Array }
): string {
	if (!cursor) return 'Hover the map';
	if (!insideWorld(cursor.x, cursor.z)) return 'World edge';
	if (mask && !isExplored(mask, cursor.x, cursor.z)) return 'Unexplored';
	return grid ? biomeAt(grid, cursor.x, cursor.z) : '';
}
