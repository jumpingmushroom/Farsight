import { describe, expect, test } from 'vitest';
import { cluster, type ClusterGroup, type ClusterPoint } from './cluster';

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

/** The original linear-scan clusterer, kept as the reference the indexed one must match exactly. */
function reference(points: ClusterPoint[], radius: number, lockedId?: string): ClusterGroup[] {
	const groups: (ClusterGroup & { locked: boolean })[] = [];
	const r2 = radius * radius;
	for (const p of points) {
		if (p.id === lockedId) {
			groups.push({ ids: [p.id], x: p.x, y: p.y, locked: true });
			continue;
		}
		const g = groups.find((g) => !g.locked && (g.x - p.x) ** 2 + (g.y - p.y) ** 2 < r2);
		if (!g) {
			groups.push({ ids: [p.id], x: p.x, y: p.y, locked: false });
			continue;
		}
		const n = g.ids.length;
		g.x = (g.x * n + p.x) / (n + 1);
		g.y = (g.y * n + p.y) / (n + 1);
		g.ids.push(p.id);
	}
	return groups.map(({ ids, x, y }) => ({ ids, x, y }));
}

function rng(seed: number): () => number {
	let a = seed >>> 0;
	return () => {
		a = (a + 0x6d2b79f5) >>> 0;
		let t = a;
		t = Math.imul(t ^ (t >>> 15), t | 1);
		t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
		return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
	};
}

describe('cluster equals the linear-scan reference', () => {
	test('on random sets: sparse, dense, integer and fractional points, with and without a locked id', () => {
		const r = rng(11);
		for (let round = 0; round < 120; round++) {
			const n = 1 + Math.floor(r() * 1500);
			const w = [100, 600, 1500, 4000][round % 4];
			const int = round % 3 === 0;
			const pts: ClusterPoint[] = Array.from({ length: n }, (_, i) => {
				let x = r() * w - 40;
				let y = r() * w * 0.6 - 40;
				if (int) [x, y] = [Math.round(x), Math.round(y)];
				return { id: `p${i}`, x, y };
			});
			const locked = round % 2 ? `p${Math.floor(r() * n)}` : undefined;
			expect(cluster(pts, 26, locked)).toEqual(reference(pts, 26, locked));
		}
	});

	test('points exactly on cell edges and exactly one radius apart', () => {
		const pts: ClusterPoint[] = [];
		for (let i = 0; i < 40; i++) for (let j = 0; j < 12; j++) pts.push({ id: `${i}:${j}`, x: i * 13, y: j * 26 });
		expect(cluster(pts, 26)).toEqual(reference(pts, 26));
		const neg = pts.map((p) => ({ ...p, x: -p.x, y: -p.y }));
		expect(cluster(neg, 26, '3:3')).toEqual(reference(neg, 26, '3:3'));
	});
});
