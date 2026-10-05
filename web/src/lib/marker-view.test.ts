import './testing/leaflet-node';
import { describe, expect, test } from 'vitest';
import L from 'leaflet';
import { CRS } from './geo';
import { MarkerGrid, containerProjector, createVisibleCache, viewPoints, type Projector } from './marker-view';
import { buildMarkers, defaultLayers, portalPairs, visibleMarkers, type LayerKey, type MapMarker } from './markers';
import { fixtureSnapshot, fixtureWorld } from './testing/markers-fixture';

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
			layer: 'landmarks'
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

	test("equals the full scan with Leaflet's own rounded projection, at fractional zooms 1-6", () => {
		// The runtime pairing: containerProjector rounds to whole pixels, so a
		// marker up to 0.5 px past the cull line rounds back inside it.
		const ms = synth(10_000, 12, 10_500);
		const grid = new MarkerGrid(ms);
		const r = rng(13);
		const flat = (vs: { id: string; x: number; y: number }[]) => vs.map((v) => `${v.id}@${v.x},${v.y}`).join(' ');
		for (let i = 0; i < 300; i++) {
			const zoom = 1 + Math.round(r() * 20) * 0.25 + (i % 3 === 0 ? r() * 0.25 : 0);
			const world = 256 * 2 ** zoom;
			const origin = L.point(Math.round(r() * world - size.x / 2), Math.round(r() * world - size.y / 2));
			const pane = L.point(Math.round((r() - 0.5) * 200), Math.round((r() - 0.5) * 200));
			const p: Projector = {
				project: containerProjector(zoom, origin, pane),
				unproject: (px, py) => {
					const ll = CRS.pointToLatLng(L.point(px, py).subtract(pane).add(origin), zoom);
					return { x: ll.lng, z: ll.lat };
				}
			};
			// Compared as strings: toEqual over thousands of objects is slow.
			expect(flat(viewPoints(grid, p, size, CULL))).toBe(flat(fullScan(ms, p, size, CULL)));
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
		for (const layers of [defaultLayers(), ALL_ON, { ...ALL_ON, portals: false }])
			for (const zoom of [1, 2.75, 3, 4.5, 6]) {
				const v = get(all, layers, zoom);
				const want = visibleMarkers(all, layers, zoom);
				expect(v.visible).toEqual(want);
				expect(v.pairs).toEqual(portalPairs(want));
				expect([...v.byId.keys()]).toEqual(want.map((m) => m.id));
				expect([...v.rank]).toEqual(want.map((m, i) => [m.id, i]));
				expect(v.grid.query(-1e9, -1e9, 1e9, 1e9)).toEqual(want);
			}
	});

	test('a pan, or a zoom that crosses no minZoom, reuses the cached set', () => {
		const get = createVisibleCache();
		const layers = defaultLayers();
		const a = get(all, layers, 1.75);
		expect(get(all, layers, 1.75)).toBe(a);
		expect(get(all, layers, 2.75)).toBe(a);
		// A copy of the same layer values is the same filter.
		expect(get(all, { ...layers }, 2)).toBe(a);
	});

	test('recomputes when an input changes or the zoom crosses a minZoom', () => {
		const get = createVisibleCache();
		const layers = defaultLayers();
		const a = get(all, layers, 2);
		const b = get(all, layers, 3); // crosses the dungeons' minZoom
		expect(b).not.toBe(a);
		expect(get(all, layers, 2)).not.toBe(b);
		const e = get(all, layers, 2);
		expect(get(all, { ...layers, beds: !layers.beds }, 2)).not.toBe(e);
		const f = get(all, layers, 2);
		expect(get([...all], layers, 2)).not.toBe(f);
	});

	test('a band computed once is kept while the other inputs stay the same', () => {
		const get = createVisibleCache();
		const layers = defaultLayers();
		const low = get(all, layers, 2);
		const high = get(all, layers, 4);
		expect(get(all, layers, 1)).toBe(low);
		expect(get(all, layers, 5)).toBe(high);
		// Another input drops every band.
		get(all, { ...layers, beds: !layers.beds }, 2);
		expect(get(all, layers, 4)).not.toBe(high);
	});

	test('warm computes the other bands one per call, then a zoom into them is a hit', () => {
		const get = createVisibleCache();
		const layers = defaultLayers();
		const low = get(all, layers, 2);
		// The fixture's only minZoom is the dungeons' 3: two bands.
		expect(get.warm(all, layers)).toBe(false);
		const high = get(all, layers, 3.5);
		expect(high.visible).toEqual(visibleMarkers(all, layers, 3.5));
		expect(get(all, layers, 6)).toBe(high);
		expect(get(all, layers, 1)).toBe(low);
		expect(get.warm(all, layers)).toBe(false);
		// Cold: band 0 first (more to do), then band 1.
		const cold = createVisibleCache();
		expect(cold.warm(all, layers)).toBe(true);
		expect(cold.warm(all, layers)).toBe(false);
		expect(cold(all, layers, 1).visible).toEqual(visibleMarkers(all, layers, 1));
		expect(cold(all, layers, 3).visible).toEqual(visibleMarkers(all, layers, 3));
	});

	test('a layer object mutated in place still recomputes', () => {
		const get = createVisibleCache();
		const layers = defaultLayers();
		const a = get(all, layers, 2);
		layers.beds = !layers.beds;
		const b = get(all, layers, 2);
		expect(b).not.toBe(a);
		expect(b.visible).toEqual(visibleMarkers(all, layers, 2));
	});
});

describe('containerProjector', () => {
	test("is bit-for-bit Leaflet's latLngToContainerPoint (project, round, − origin, + pane)", () => {
		const r = rng(9);
		for (let i = 0; i < 2000; i++) {
			const zoom = 1 + Math.round(r() * 20) * 0.25;
			const origin = L.point(Math.round((r() - 0.5) * 40000), Math.round((r() - 0.5) * 40000));
			const pane = r() < 0.5 ? L.point(Math.round((r() - 0.5) * 3000), Math.round((r() - 0.5) * 3000)) : L.point((r() - 0.5) * 3000, (r() - 0.5) * 3000);
			const x = (r() * 2 - 1) * 12000;
			const z = (r() * 2 - 1) * 12000;
			// Map#latLngToContainerPoint, step by step (Leaflet 1.9.4).
			const want = CRS.latLngToPoint(L.latLng(z, x), zoom).round().subtract(origin).add(pane);
			const got = containerProjector(zoom, origin, pane)(x, z);
			expect(got.x).toBe(want.x);
			expect(got.y).toBe(want.y);
		}
	});
});
