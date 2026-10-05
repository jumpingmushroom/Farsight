import './testing/leaflet-node';
import { describe, expect, test } from 'vitest';
import { emptyMask, setCell } from './explored';
import { BIOME_NAMES, GRID_BYTES, GRID_SIZE, biomeAt, placeLabel } from './biomes';

/** A grid with `byte` at the cell under world point (x, z) and 0 elsewhere. */
function gridWith(x: number, z: number, byte: number): Uint8Array {
	const grid = new Uint8Array(GRID_BYTES);
	const gx = Math.floor(x / 20) + 512;
	const gz = Math.floor(z / 20) + 512;
	grid[gz * GRID_SIZE + gx] = byte;
	return grid;
}

describe('biomeAt', () => {
	test('reads the byte at the cell under (x, z)', () => {
		const grid = gridWith(5000, 5000, 3);
		expect(biomeAt(grid, 5000, 5000)).toBe('Swamp');
	});

	test('off-grid is unnamed', () => {
		const grid = new Uint8Array(GRID_BYTES);
		expect(biomeAt(grid, 20000, 0)).toBe('');
	});

	test('byte 0 (no biome) is unnamed', () => {
		const grid = new Uint8Array(GRID_BYTES);
		expect(biomeAt(grid, 0, 0)).toBe('');
	});

	test('every index 1-9 names a biome (internal/biomegrid.indexOf)', () => {
		expect(BIOME_NAMES.slice(1)).toEqual([
			'Meadows',
			'Black Forest',
			'Swamp',
			'Mountains',
			'Plains',
			'Mistlands',
			'Ashlands',
			'Deep North',
			'Ocean'
		]);
	});
});

describe('placeLabel', () => {
	test('no cursor -> "Hover the map"', () => {
		expect(placeLabel(undefined, {})).toBe('Hover the map');
	});

	test('outside the world disc -> "World edge"', () => {
		expect(placeLabel({ x: 20000, z: 0 }, {})).toBe('World edge');
	});

	test('mask present and the 12 m cell unexplored -> "Unexplored"', () => {
		const mask = emptyMask();
		expect(placeLabel({ x: 0, z: 0 }, { mask })).toBe('Unexplored');
	});

	test('explored, with the grid loaded -> the biome name', () => {
		const mask = emptyMask();
		setCell(mask, 1024, 1024); // the cell under (0, 0), per explored.test.ts
		const grid = gridWith(0, 0, 1);
		expect(placeLabel({ x: 0, z: 0 }, { mask, grid })).toBe('Meadows');
	});

	test('explored, with no grid loaded yet -> \'\'', () => {
		const mask = emptyMask();
		setCell(mask, 1024, 1024);
		expect(placeLabel({ x: 0, z: 0 }, { mask })).toBe('');
	});
});
