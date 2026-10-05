import './testing/leaflet-node';
import { describe, expect, test } from 'vitest';
import { MarkerGrid, createVisibleCache, viewPoints, type Projector } from './marker-view';
import { buildMarkers, defaultLayers, portalPairs, visibleMarkers, type LayerKey, type MapMarker } from './markers';
import { fixtureMask, fixtureSnapshot, fixtureWorld } from './testing/markers-fixture';

// A deterministic PRNG (mulberry32): the synthetic sets are the same every run.
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

/** Synthetic markers: only the fields the view code reads matter. */
function synth(n: number, seed: number, spread = 10_000, prefix = 'm'): MapMarker[] {
	const r = rng(seed);
	return Array.from({ length: n }, (_, i) => {
		const m = {
			id: `${prefix}${i}`,
			x: (r() * 2 - 1) * spread,
			z: (r() * 2 - 1) * spread,
			layer: 'locations'
		} as MapMarker;
		if (i % 3 === 0) m.minZoom = 3;
		return m;
	});
}

/**
 * The world CRS (geo.ts) as a pure projection: container px from world
 * (x, z) at `zoom` with the view's top-left at world-pixel (ox, oy).
 */
function projector(zoom: number, ox: number, oy: number): Projector {
	const k = (256 / 21000) * 2 ** zoom;
	return {
		project: (x, z) => ({ x: k * x + 128 * 2 ** zoom - ox, y: -k * z + 128 * 2 ** zoom - oy }),
		unproject: (px, py) => ({ x: (px + ox - 128 * 2 ** zoom) / k, z: -(py + oy - 128 * 2 ** zoom) / k })
	};
}

/** The old render's projection + cull: every marker projected, kept within ±cull of the view. */
function fullScan(ms: MapMarker[], p: Projector, size: { x: number; y: number }, cull: number) {
	const out: { id: string; x: number; y: number }[] = [];
	for (const m of ms) {
		const q = p.project(m.x, m.z);
		if (q.x < -cull || q.y < -cull || q.x > size.x + cull || q.y > size.y + cull) continue;
		out.push({ id: m.id, x: q.x, y: q.y });
	}
	return out;
}

describe('MarkerGrid', () => {
	test('a query returns exactly the full scan of the rect, in input order', () => {
		const ms = synth(3000, 1);
		const grid = new MarkerGrid(ms);
		const r = rng(2);
		for (let i = 0; i < 200; i++) {
			const x0 = (r() * 2 - 1) * 12_000;
			const z0 = (r() * 2 - 1) * 12_000;
			const w = r() * 8000;
			const h = r() * 8000;
			const want = ms.filter((m) => m.x >= x0 && m.x <= x0 + w && m.z >= z0 && m.z <= z0 + h);
			expect(grid.query(x0, z0, x0 + w, z0 + h)).toEqual(want);
		}
	});

	test('bounds are inclusive, an empty grid or rect returns nothing', () => {
		const ms = [{ id: 'a', x: 512, z: -512 } as MapMarker, { id: 'b', x: 0, z: 0 } as MapMarker];
		const grid = new MarkerGrid(ms);
		expect(grid.query(512, -512, 512, -512).map((m) => m.id)).toEqual(['a']);
		expect(grid.query(-1, -1, 1, 1).map((m) => m.id)).toEqual(['b']);
		expect(grid.query(1, 1, -1, -1)).toEqual([]);
		expect(new MarkerGrid([]).query(-1e9, -1e9, 1e9, 1e9)).toEqual([]);
	});

	test('a query scans only the cells it overlaps', () => {
		const near = synth(1000, 3, 1000, 'n');
		const far = synth(9000, 4, 10_000, 'f').map((m) => ({ ...m, x: m.x + 30_000 }) as MapMarker);
		const stats = { scanned: 0 };
		new MarkerGrid([...near, ...far]).query(-1000, -1000, 1000, 1000, stats);
		expect(stats.scanned).toBeLessThan(1500);
	});
});

describe('viewPoints', () => {
	const size = { x: 1440, y: 900 };
	const CULL = 40;

	test('equals the full projection scan at many zooms and offsets', () => {
		const ms = synth(4000, 5);
		const grid = new MarkerGrid(ms);
		const r = rng(6);
		for (let i = 0; i < 150; i++) {
			const zoom = 1 + r() * 5;
			const world = 256 * 2 ** zoom;
			const p = projector(zoom, Math.round(r() * world - size.x / 2), Math.round(r() * world - size.y / 2));
			expect(viewPoints(grid, p, size, CULL)).toEqual(fullScan(ms, p, size, CULL));
		}
	});

	test('regression guard: a pan projects only the markers near the view, not all of them', () => {
		// 2,000 markers in the view's neighbourhood, then 20,000 more far away.
		const zoom = 5;
		const p = projector(zoom, 256 * 2 ** zoom * 0.5 - size.x / 2, 256 * 2 ** zoom * 0.5 - size.y / 2);
		const near = synth(2000, 7, 900, 'n');
		const far = synth(20_000, 8, 9000, 'f').filter((m) => Math.abs(m.x) > 3000 || Math.abs(m.z) > 3000);
		const count = (ms: MapMarker[]) => {
			let n = 0;
			const counting: Projector = { project: (x, z) => (n++, p.project(x, z)), unproject: p.unproject };
			const pts = viewPoints(new MarkerGrid(ms), counting, size, CULL);
			return { projected: n, inView: pts.length };
		};
		const a = count(near);
		const b = count([...near, ...far]);
		expect(b.inView).toBe(a.inView);
		// The far markers add no projections at all; the near ones are projected at most once.
		expect(b.projected).toBe(a.projected);
		expect(b.projected).toBeLessThanOrEqual(near.length);
		expect(b.projected).toBeLessThan((near.length + far.length) / 5);
	});
});

describe('createVisibleCache', () => {
	const all = buildMarkers(fixtureSnapshot(), fixtureWorld());
	const ALL_ON = Object.fromEntries(Object.keys(defaultLayers()).map((k) => [k, true])) as Record<LayerKey, boolean>;

	test('matches visibleMarkers and portalPairs for every input', () => {
		const get = createVisibleCache();
		const mask = fixtureMask();
		for (const layers of [defaultLayers(), ALL_ON, { ...ALL_ON, portals: false }])
			for (const fog of [true, false])
				for (const zoom of [1, 2.75, 3, 4.5, 6]) {
					const v = get(all, layers, mask, fog, zoom);
					const want = visibleMarkers(all, layers, mask, fog, zoom);
					expect(v.visible).toEqual(want);
					expect(v.pairs).toEqual(portalPairs(want));
					expect([...v.byId.keys()]).toEqual(want.map((m) => m.id));
					expect(v.grid.query(-1e9, -1e9, 1e9, 1e9)).toEqual(want);
				}
	});

	test('a pan, or a zoom that crosses no minZoom, reuses the cached set', () => {
		const get = createVisibleCache();
		const mask = fixtureMask();
		const layers = defaultLayers();
		const a = get(all, layers, mask, true, 1.75);
		expect(get(all, layers, mask, true, 1.75)).toBe(a);
		expect(get(all, layers, mask, true, 2.75)).toBe(a);
		// A copy of the same layer values is the same filter.
		expect(get(all, { ...layers }, mask, true, 2)).toBe(a);
	});

	test('recomputes when an input changes or the zoom crosses a minZoom', () => {
		const get = createVisibleCache();
		const mask = fixtureMask();
		const layers = defaultLayers();
		const a = get(all, layers, mask, true, 2);
		const b = get(all, layers, mask, true, 3); // crosses the dungeons' minZoom
		expect(b).not.toBe(a);
		expect(get(all, layers, mask, true, 2)).not.toBe(b);
		const c = get(all, layers, mask, true, 2);
		expect(get(all, layers, mask, false, 2)).not.toBe(c);
		const d = get(all, layers, mask, false, 2);
		expect(get(all, layers, fixtureMask(), false, 2)).not.toBe(d);
		const e = get(all, layers, mask, false, 2);
		expect(get(all, { ...layers, beds: !layers.beds }, mask, false, 2)).not.toBe(e);
		const f = get(all, layers, mask, false, 2);
		expect(get([...all], layers, mask, false, 2)).not.toBe(f);
	});

	test('a layer object mutated in place still recomputes', () => {
		const get = createVisibleCache();
		const layers = defaultLayers();
		const a = get(all, layers, undefined, false, 2);
		layers.beds = !layers.beds;
		const b = get(all, layers, undefined, false, 2);
		expect(b).not.toBe(a);
		expect(b.visible).toEqual(visibleMarkers(all, layers, undefined, false, 2));
	});
});
