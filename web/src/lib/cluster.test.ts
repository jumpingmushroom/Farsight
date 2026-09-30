import { describe, expect, test } from 'vitest';
import { cluster } from './cluster';

describe('cluster (DESIGN-NOTES §5.1 greedy running-centroid)', () => {
	test('two points 10 px apart form one group at their mean', () => {
		const g = cluster(
			[
				{ id: 'a', x: 100, y: 100 },
				{ id: 'b', x: 110, y: 100 }
			],
			26
		);
		expect(g).toEqual([{ ids: ['a', 'b'], x: 105, y: 100 }]);
	});

	test('points 30 px apart stay separate', () => {
		const g = cluster(
			[
				{ id: 'a', x: 0, y: 0 },
				{ id: 'b', x: 30, y: 0 }
			],
			26
		);
		expect(g).toEqual([
			{ ids: ['a'], x: 0, y: 0 },
			{ ids: ['b'], x: 30, y: 0 }
		]);
	});

	test('the centroid runs: 0, 20, 40 → {0,20} at 10, then 40 is 30 away', () => {
		const g = cluster(
			[
				{ id: 'a', x: 0, y: 0 },
				{ id: 'b', x: 20, y: 0 },
				{ id: 'c', x: 40, y: 0 }
			],
			26
		);
		expect(g).toEqual([
			{ ids: ['a', 'b'], x: 10, y: 0 },
			{ ids: ['c'], x: 40, y: 0 }
		]);
	});

	test('distance must be strictly below the radius', () => {
		const g = cluster(
			[
				{ id: 'a', x: 0, y: 0 },
				{ id: 'b', x: 26, y: 0 }
			],
			26
		);
		expect(g.length).toBe(2);
	});

	test('the locked id never merges, even when coincident', () => {
		const pts = [
			{ id: 'a', x: 50, y: 50 },
			{ id: 'sel', x: 50, y: 50 },
			{ id: 'b', x: 52, y: 50 }
		];
		expect(cluster(pts, 26, 'sel')).toEqual([
			{ ids: ['a', 'b'], x: 51, y: 50 },
			{ ids: ['sel'], x: 50, y: 50 }
		]);
		// Locked first: later points don't join it either.
		expect(cluster([pts[1], pts[0], pts[2]], 26, 'sel')).toEqual([
			{ ids: ['sel'], x: 50, y: 50 },
			{ ids: ['a', 'b'], x: 51, y: 50 }
		]);
	});

	test('input order is preserved for group creation and membership', () => {
		const g = cluster(
			[
				{ id: 'z', x: 500, y: 0 },
				{ id: 'y', x: 0, y: 0 },
				{ id: 'x', x: 505, y: 0 },
				{ id: 'w', x: 5, y: 0 }
			],
			26
		);
		expect(g.map((x) => x.ids)).toEqual([
			['z', 'x'],
			['y', 'w']
		]);
	});

	test('joins the first matching group, not the nearest', () => {
		const g = cluster(
			[
				{ id: 'a', x: 0, y: 0 },
				{ id: 'b', x: 30, y: 0 },
				{ id: 'c', x: 20, y: 0 }
			],
			26
		);
		expect(g.map((x) => x.ids)).toEqual([['a', 'c'], ['b']]);
	});

	test('empty input gives no groups', () => {
		expect(cluster([], 26)).toEqual([]);
	});
});
