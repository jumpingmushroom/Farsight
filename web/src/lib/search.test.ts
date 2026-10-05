import './testing/leaflet-node';
import { describe, expect, test } from 'vitest';
import { buildMarkers } from './markers';
import { hints, search } from './search';
import { fixtureSnapshot, fixtureWorld } from './testing/markers-fixture';
import type { SnapshotView } from './types';

const snap = fixtureSnapshot();
const all = buildMarkers(snap, fixtureWorld());

function mini(markers: SnapshotView['markers'], extra: Partial<SnapshotView> = {}): SnapshotView {
	return {
		savedAt: '',
		fogKey: '0123456789abcdef',
		explored: { source: 'tables', cell: 12, size: 2048, bits: '' },
		markers,
		locations: [],
		bases: [],
		players: [],
		...extra
	};
}

describe('search', () => {
	test('"hom" → the home portals first (prefix), then the substring sign', () => {
		const r = search(all, 'hom');
		expect(r.map((x) => x.id)).toEqual(['portal-1', 'portal-2', 'sign-2']);
		expect(r[0]).toMatchObject({ title: 'home', sub: 'Portal · paired', icon: 'portal' });
		expect(r[0].pin.bg).toBe('#3d7eab');
		expect(r[2]).toMatchObject({ title: 'Welcome home', sub: 'Sign', icon: 'signpost' });
	});

	test('query is trimmed and case-insensitive; empty gives nothing', () => {
		expect(search(all, '  HOME ').map((x) => x.id)).toEqual(['portal-1', 'portal-2', 'sign-2']);
		expect(search(all, '   ')).toEqual([]);
	});

	test('capped at 8 by default, or at the given limit', () => {
		const many = buildMarkers(
			mini(
				Array.from({ length: 12 }, (_, i) => ({
					id: `sign-${i}`,
					kind: 'sign',
					x: i * 100,
					y: 0,
					z: 0,
					label: `home ${i}`
				}))
			)
		);
		expect(search(many, 'hom').length).toBe(8);
		expect(search(many, 'hom', 3).length).toBe(3);
	});

	test('rank: exact, then prefix, then substring, regardless of kind order', () => {
		const ms = buildMarkers(
			mini([
				{ id: 'sign-1', kind: 'sign', x: 0, y: 0, z: 0, label: 'Smoked lox' },
				{ id: 'portal-1', kind: 'portal', x: 100, y: 0, z: 0, label: 'loxpen' },
				{ id: 'tame-1', kind: 'tame', x: 200, y: 0, z: 0, label: 'Lox', species: 'Lox' }
			])
		);
		expect(search(ms, 'lox').map((r) => r.id)).toEqual(['tame-1', 'portal-1', 'sign-1']);
	});

	test('within a rank, kinds come portal, base, tame, sign, altar, trader', () => {
		const ms = buildMarkers(
			mini(
				[
					{ id: 'sign-1', kind: 'sign', x: 0, y: 0, z: 0, label: 'ulf' },
					{ id: 'tame-1', kind: 'tame', x: 100, y: 0, z: 0, label: 'Ulf', species: 'Wolf' },
					{ id: 'portal-1', kind: 'portal', x: 200, y: 0, z: 0, label: 'ulf' }
				],
				{
					bases: [{ id: 'base-1', name: 'Ulf', x: 300, z: 0, radius: 10, pieces: 3, builders: [] }],
					locations: [{ id: 'loc-1', kind: 'trader', x: 400, y: 0, z: 0, label: 'Ulf' }]
				}
			)
		);
		expect(search(ms, 'ulf').map((r) => r.id)).toEqual([
			'portal-1',
			'base-1',
			'tame-1',
			'sign-1',
			'loc-1'
		]);
	});

	test('an exact tame name ranks above prefix and substring matches', () => {
		const r = search(all, 'big mama');
		expect(r.map((x) => x.id)).toEqual(['tame-1']);
		expect(r[0].sub).toBe("Lox · near Halvor's base");
		expect(search(all, 'hen')[0]).toMatchObject({ id: 'tame-2', title: 'Hen', sub: 'Hen' });
	});

	test('a builder’s full name and the base title match; words match too', () => {
		const r = search(all, "halvor's");
		expect(r.map((x) => x.id)).toEqual(['base-1']);
		expect(r[0]).toMatchObject({ title: "Halvor's base", sub: 'Base by Halvor · 2,184 pieces', icon: 'home' });
		expect(search(all, 'frøya').map((x) => x.id)).toEqual(['base-1']);
		// A typographic apostrophe matches too.
		expect(search(all, 'halvor’s').map((x) => x.id)).toEqual(['base-1']);
	});

	test('altar and portal sub-lines', () => {
		expect(search(all, 'eikthyr')[0]).toMatchObject({ sub: 'Boss altar · Defeated', icon: 'flame' });
		expect(search(all, 'yagluth')[0]).toMatchObject({ sub: 'Boss altar · Not yet defeated' });
		expect(search(all, 'copper')[0]).toMatchObject({ sub: 'Portal · unpaired' });
	});

	test('beds, tombstones and dungeons are never returned', () => {
		for (const q of ['bjorn', 'bed', 'unknown', 'tombstone', 'crypt', 'troll', 'dungeon']) {
			const ids = search(all, q).map((r) => r.id);
			expect(ids.filter((id) => /^(bed|tombstone)/.test(id) || id === 'loc-4' || id === 'loc-5'), q).toEqual([]);
		}
	});

	test('every searchable marker is a candidate (the server already dropped unexplored ones)', () => {
		expect(search(all, 'haldor')[0]).toMatchObject({ id: 'loc-3', sub: 'Trader', icon: 'coins' });
		expect(search(all, 'trader').map((r) => r.id)).toEqual(['loc-3']);
	});
});

describe('hints', () => {
	test('up to 5: first portal tag, named tame, base builder, boss altar, sign text', () => {
		expect(hints(all)).toEqual(['home', 'Big Mama', 'Halvor', 'Eikthyr', 'Welcome home']);
	});

	test('skip empty values, dedupe, and truncate sign text to 20 chars', () => {
		const ms = buildMarkers(
			mini([
				{ id: 'portal-1', kind: 'portal', x: 0, y: 0, z: 0, label: '' },
				{ id: 'portal-2', kind: 'portal', x: 0, y: 0, z: 0, label: 'Ulf' },
				{ id: 'tame-1', kind: 'tame', x: 0, y: 0, z: 0, species: 'Hen' },
				{ id: 'tame-2', kind: 'tame', x: 0, y: 0, z: 0, label: 'Ulf', species: 'Wolf' },
				{ id: 'sign-1', kind: 'sign', x: 0, y: 0, z: 0, label: 'Beware of the trolls beyond the ridge' }
			])
		);
		expect(hints(ms)).toEqual(['Ulf', 'Beware of the trolls']);
	});

	test('no data, no hints', () => {
		expect(hints([])).toEqual([]);
	});
});
