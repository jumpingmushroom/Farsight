// A hand-written snapshot with one of each marker kind (invented names), for
// markers/search tests. Every marker's 12 m cell is explored except the
// tombstone's and Haldor's, so fog filtering can be tested.

import { cellOf, emptyMask, setCell } from '../explored';
import type { Marker, SnapshotView, WorldCard } from '../types';

export const MARKERS: Marker[] = [
	{ id: 'sign-1', kind: 'sign', x: 30, y: 30, z: -20, label: '' },
	{ id: 'portal-1', kind: 'portal', x: 100, y: 30, z: 100, label: 'home', pair: 'portal-2' },
	{ id: 'bed-1', kind: 'bed', x: 10, y: 30, z: 10, owner: '' },
	{ id: 'portal-2', kind: 'portal', x: 3100, y: 30, z: 100, label: 'home', pair: 'portal-1' },
	{ id: 'portal-3', kind: 'portal', x: -1234.4, y: 30, z: 3050, label: 'copper' },
	{ id: 'portal-4', kind: 'portal', x: 500, y: 30, z: -500, label: 'hub' },
	{ id: 'tombstone-1', kind: 'tombstone', x: -2000, y: 30, z: 1000, owner: 'Bjorn' },
	{ id: 'portal-5', kind: 'portal', x: 520, y: 30, z: -500, label: 'hub' },
	{ id: 'portal-6', kind: 'portal', x: 540, y: 30, z: -500, label: 'hub' },
	{ id: 'portal-7', kind: 'portal', x: -800, y: 30, z: -800, label: '' },
	{ id: 'tame-1', kind: 'tame', x: 200, y: 30, z: 0, species: 'Lox', label: 'Big Mama' },
	{ id: 'tame-2', kind: 'tame', x: -3000, y: 30, z: -3000, species: 'Hen' },
	{ id: 'sign-2', kind: 'sign', x: 40, y: 30, z: -20, label: 'Welcome home' }
];

export const LOCATIONS: Marker[] = [
	{ id: 'loc-5', kind: 'dungeon', x: 700, y: 40, z: 1200, label: 'Troll cave', type: 'TrollCave02' },
	{ id: 'loc-1', kind: 'boss_altar', x: -600, y: 40, z: 900, label: 'Eikthyr', type: 'Eikthyrnir' },
	{ id: 'loc-3', kind: 'trader', x: 1300, y: 40, z: 600, label: 'Haldor', type: 'Vendor_BlackForest' },
	{ id: 'loc-2', kind: 'boss_altar', x: 4200, y: 40, z: -120, label: 'Yagluth', type: 'GoblinKing' },
	{ id: 'loc-4', kind: 'dungeon', x: -1000, y: 40, z: -900, label: 'Sunken crypt', type: 'SunkenCrypt4' }
];

const FOGGED = new Set(['tombstone-1', 'loc-3']);

/**
 * The fixture's explored mask: the cell under every marker and location
 * except the fogged ones and those in `hide`, plus the cell at (0, 0)
 * (base-1).
 */
export function fixtureMask(hide: string[] = []): Uint8Array {
	const m = emptyMask();
	for (const p of [...MARKERS, ...LOCATIONS]) {
		if (!FOGGED.has(p.id) && !hide.includes(p.id)) setCell(m, ...cellOf(p.x, p.z));
	}
	setCell(m, ...cellOf(0, 0));
	return m;
}

export function fixtureSnapshot(): SnapshotView {
	return {
		savedAt: '2026-09-30T10:00:00Z',
		fogKey: '0123456789abcdef',
		explored: { source: 'tables', cell: 12, size: 2048, bits: '' },
		mask: fixtureMask(),
		markers: MARKERS.map((m) => ({ ...m })),
		locations: LOCATIONS.map((m) => ({ ...m })),
		bases: [
			{
				id: 'base-1',
				name: '',
				x: 0,
				z: 0,
				radius: 60,
				pieces: 2184,
				builders: [
					{ id: 1, name: 'Halvor', pieces: 1500 },
					{ id: 2, name: 'Frøya', pieces: 600 },
					{ id: 3, pieces: 84 }
				]
			}
		],
		players: []
	};
}

export function fixtureWorld(): WorldCard {
	return {
		name: 'Testheim',
		seedName: 'abc',
		day: 12,
		bosses: [
			{ key: 'defeated_eikthyr', name: 'Eikthyr', defeated: true },
			{ key: 'defeated_goblinking', name: 'Yagluth', defeated: false }
		],
		modifiers: {},
		flags: [],
		exploredPct: 1.2,
		savedAt: '2026-09-30T10:00:00Z',
		readAt: '2026-09-30T10:00:05Z'
	};
}
