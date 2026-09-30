import './testing/leaflet-node';
import { afterEach, describe, expect, test, vi } from 'vitest';
import L from 'leaflet';
import {
	FOG_PADDING,
	PATTERN_SIZE,
	ZN,
	ZOFF,
	createFogLayer,
	fogBlur,
	isExplored,
	maskCells,
	maskDrawRect,
	maskForZones,
	maskSums,
	maskWorldRect,
	mulberry32,
	patternOffset,
	punchPlan,
	blurScale,
	boxBlur,
	boxRadiiForGauss,
	gaussBlur,
	releaseScratch,
	scratchCanvas,
	worldDisc,
	zoneMask
} from './fog';
import { CRS, toLatLng } from './geo';

describe('zoneMask', () => {
	test('grid constants', () => {
		expect(ZN).toBe(330);
		expect(ZOFF).toBe(165);
	});
	test('sets exactly the explored cells (row = ZOFF − zz, col = zx + ZOFF)', () => {
		const m = zoneMask([
			[0, 0],
			[-164, 164],
			[164, -164]
		]);
		expect(m.length).toBe(ZN * ZN);
		const set: number[] = [];
		m.forEach((v, i) => {
			if (v) set.push(i);
		});
		expect(set).toEqual([1 * ZN + 1, 165 * ZN + 165, 329 * ZN + 329]);
		expect(m[165 * ZN + 165]).toBe(1);
	});
	test('ignores out-of-range zones without throwing', () => {
		const m = zoneMask([
			[-166, 0],
			[165, 0],
			[0, 166],
			[0, -165],
			[1000, -1000]
		]);
		expect(m.some((v) => v !== 0)).toBe(false);
	});
	test('edges of the grid are in range', () => {
		const m = zoneMask([
			[-165, 165],
			[164, -164]
		]);
		expect(m[0]).toBe(1);
		expect(m[329 * ZN + 329]).toBe(1);
	});
});

describe('maskForZones', () => {
	test('caches one mask per zones array (i.e. per snapshot)', () => {
		const zones: [number, number][] = [[1, 2]];
		const a = maskForZones(zones);
		expect(maskForZones(zones)).toBe(a);
		expect(a[(ZOFF - 2) * ZN + ZOFF + 1]).toBe(1);
		expect(maskForZones([[1, 2]])).not.toBe(a);
	});
});

describe('isExplored', () => {
	test('looks a world point up in the mask by its zone', () => {
		const m = zoneMask([[1, -1]]);
		expect(isExplored(m, 32, -32.1)).toBe(true);
		expect(isExplored(m, 31.9, -32.1)).toBe(false);
		expect(isExplored(m, 99999, 0)).toBe(false);
	});
});

describe('maskWorldRect', () => {
	test('covers the zone grid in world metres', () => {
		expect(maskWorldRect()).toEqual({ x0: -10592, z0: 10592, size: 21120 });
	});
});

describe('noise PRNG', () => {
	test('mulberry32 is deterministic and in [0,1)', () => {
		const a = mulberry32(42);
		const b = mulberry32(42);
		const xs = Array.from({ length: 100 }, () => a());
		expect(xs).toEqual(Array.from({ length: 100 }, () => b()));
		expect(xs.every((v) => v >= 0 && v < 1)).toBe(true);
		expect(new Set(xs).size).toBeGreaterThan(95);
	});
});

describe('maskDrawRect', () => {
	const near = (a: number, b: number) => expect(a).toBeCloseTo(b, 6);
	test('zoom 0: the mask rect in world pixels (origin 0,0)', () => {
		const r = maskDrawRect({ x: 0, y: 0 }, 0);
		// x0 = −10592 m → 256/21000·(−10592) + 128; z0 = 10592 m (north) → the same on y.
		near(r.x, (256 / 21000) * -10592 + 128);
		near(r.y, (-256 / 21000) * 10592 + 128);
		near(r.x, -1.1215238);
		near(r.size, (21120 * 256) / 21000);
		near(r.cell, r.size / ZN);
	});
	test('zoom 5, relative to a pixel origin, matches the CRS', () => {
		const origin = { x: 1234, y: 567 };
		const r = maskDrawRect(origin, 5);
		const nw = CRS.latLngToPoint(toLatLng(-10592, 10592), 5);
		const se = CRS.latLngToPoint(toLatLng(-10592 + 21120, 10592 - 21120), 5);
		near(r.x, nw.x - origin.x);
		near(r.y, nw.y - origin.y);
		near(r.size, se.x - nw.x);
		near(r.size, se.y - nw.y);
		near(r.cell, 64 * (256 / 21000) * 32);
	});
	test('fractional zoom scales by 2^zoom', () => {
		const a = maskDrawRect({ x: 0, y: 0 }, 3);
		const b = maskDrawRect({ x: 0, y: 0 }, 3.25);
		near(b.size / a.size, 2 ** 0.25);
	});
});

describe('maskCells', () => {
	const rect = { x: -100, y: -50, size: 330 * 10, cell: 10 };
	test('only the cells over the view plus the margin (and one spare cell)', () => {
		// view 200×100 px, margin 15 px → mask x ∈ [85, 315], y ∈ [35, 165] → cells 8.5–31.5, 3.5–16.5
		expect(maskCells(rect, 200, 100, 15)).toEqual({ c0: 7, r0: 2, c1: 33, r1: 18 });
	});
	test('clamped to the grid', () => {
		expect(maskCells({ x: 50, y: 60, size: 3300, cell: 10 }, 10000, 10000, 0)).toEqual({ c0: 0, r0: 0, c1: ZN, r1: ZN });
	});
	test('null when the view misses the mask', () => {
		expect(maskCells({ x: 5000, y: 0, size: 3300, cell: 10 }, 200, 100, 15)).toBeNull();
		expect(maskCells({ x: 0, y: -9000, size: 3300, cell: 10 }, 200, 100, 15)).toBeNull();
	});
	test('bounded at high zoom: a 330×330 mask at zoom 6 draws a few dozen cells', () => {
		const r = maskDrawRect({ x: 8000, y: 8000 }, 6);
		const c = maskCells(r, 2160, 1350, 72)!;
		expect(c.c1 - c.c0).toBeLessThan(50);
		expect(c.r1 - c.r0).toBeLessThan(35);
	});
});

describe('patternOffset', () => {
	test('in [0, PATTERN_SIZE) and a multiple of the 9-px hatch', () => {
		expect(PATTERN_SIZE).toBe(126);
		expect(PATTERN_SIZE % 9).toBe(0);
		for (const o of [0, 1, 125, 126, 127, -1, -127, 99999, -99999]) {
			const p = patternOffset({ x: o, y: -o });
			expect(p.x).toBeGreaterThanOrEqual(0);
			expect(p.x).toBeLessThan(PATTERN_SIZE);
			expect(p.y).toBeGreaterThanOrEqual(0);
			expect(p.y).toBeLessThan(PATTERN_SIZE);
		}
	});
	test('anchored to world pixel (0,0): an origin at 0 needs no shift', () => {
		expect(patternOffset({ x: 0, y: 0 })).toEqual({ x: 0, y: 0 });
		expect(patternOffset({ x: 126 * 7, y: -126 * 3 })).toEqual({ x: 0, y: 0 });
	});
	test('periodic (mod 126) and continuous across a pan of N px', () => {
		const o = { x: 1000, y: 777 };
		const p0 = patternOffset(o);
		for (let n = -300; n <= 300; n += 7) {
			const p = patternOffset({ x: o.x + n, y: o.y - n });
			// Panning the canvas origin by n moves the pattern by −n within the canvas,
			// i.e. world pixel (0,0) of the pattern stays put.
			expect((((p.x - p0.x + n) % 126) + 126) % 126).toBe(0);
			expect((((p.y - p0.y - n) % 126) + 126) % 126).toBe(0);
		}
		expect(patternOffset({ x: o.x + 126, y: o.y })).toEqual(p0);
	});
	test('takes the period in device px (a 252-px tile at DPR 2)', () => {
		expect(patternOffset({ x: 10, y: -10 }, 252)).toEqual({ x: 242, y: 10 });
		expect(patternOffset({ x: 252 * 5 + 3, y: 0 }, 252)).toEqual({ x: 249, y: 0 });
	});
});

describe('maskSums (summed-area table)', () => {
	test('counts explored cells in any cell range, clamped to the grid', () => {
		const m = zoneMask([
			[0, 0],
			[1, 0],
			[0, -1]
		]);
		const s = maskSums(m);
		const c = ZOFF, r = ZOFF;
		expect(s.count(c, r, c + 1, r + 1)).toBe(1);
		expect(s.count(c, r, c + 2, r + 2)).toBe(3);
		expect(s.count(0, 0, ZN, ZN)).toBe(3);
		expect(s.count(-50, -50, ZN + 50, ZN + 50)).toBe(3);
		expect(s.count(c + 2, r, c + 10, r + 10)).toBe(0);
		expect(s.count(c, r, c, r + 5)).toBe(0);
	});
	test('cached per mask', () => {
		const m = zoneMask([[0, 0]]);
		expect(maskSums(m)).toBe(maskSums(m));
	});
});

describe('punchPlan', () => {
	// 20×20 explored zones around the centre; 10-px cells with the grid origin at (−1650, −1650) → cell (165,165) at (0,0).
	const zones: [number, number][] = [];
	for (let zx = 0; zx < 20; zx++) for (let zz = 0; zz < 20; zz++) zones.push([zx, -zz]);
	const sums = maskSums(zoneMask(zones));
	const rect = { x: -1650, y: -1650, cell: 10 };
	const plan = punchPlan(sums, rect, 400, 300, 32, 12);
	const inAny = (rs: number[][], x: number, y: number) => rs.some(([rx, ry, w, h]) => x >= rx && x < rx + w && y >= ry && y < ry + h);
	test('explored interior is cleared, the edge band is blended, far fog untouched', () => {
		expect(inAny(plan.clear, 100, 100)).toBe(true); // deep inside the 200×200 explored square
		expect(inAny(plan.band, 100, 100)).toBe(false);
		expect(inAny(plan.band, 199, 100)).toBe(true); // on the east edge
		expect(inAny(plan.clear, 199, 100)).toBe(false);
		expect(inAny(plan.band, 350, 250)).toBe(false); // far outside
		expect(inAny(plan.clear, 350, 250)).toBe(false);
	});
	test('every pixel within the margin of an explored cell is covered; rects stay on the canvas and on whole pixels', () => {
		for (let y = 0; y < 300; y += 3)
			for (let x = 0; x < 400; x += 3) {
				const near = x < 200 + 12 && y < 200 + 12;
				if (near) expect(inAny(plan.clear, x, y) || inAny(plan.band, x, y), `${x},${y}`).toBe(true);
			}
		for (const [x, y, w, h] of [...plan.clear, ...plan.band]) {
			for (const v of [x, y, w, h]) expect(Number.isInteger(v)).toBe(true);
			expect(x >= 0 && y >= 0 && x + w <= 400 && y + h <= 300).toBe(true);
		}
	});
	test('band bounds are the union of the band rects', () => {
		const b = plan.bandBounds!;
		expect(b.x0).toBe(Math.min(...plan.band.map((r) => r[0])));
		expect(b.x1).toBe(Math.max(...plan.band.map((r) => r[0] + r[2])));
		expect(punchPlan(maskSums(zoneMask([])), rect, 400, 300, 32, 12)).toEqual({ clear: [], band: [], bandBounds: null });
	});
	test('runs along a row are merged', () => {
		const wide = punchPlan(sums, rect, 400, 300, 8, 4);
		const rows = new Set(wide.clear.map((r) => r[1]));
		expect(wide.clear.length).toBe(rows.size);
	});
});

describe('blurScale', () => {
	test('downscale so the blur keeps ≥ 2 px on the scratch, between 1 and 8', () => {
		expect(blurScale(2)).toBe(1);
		expect(blurScale(4)).toBe(2);
		expect(blurScale(17)).toBe(8);
		expect(blurScale(48)).toBe(8);
		expect(blurScale(0.5)).toBe(1);
	});
});

describe('worldDisc and fogBlur', () => {
	test('the world disc (radius 10 500 m) at a zoom, relative to an origin', () => {
		expect(worldDisc({ x: 0, y: 0 }, 0)).toEqual({ x: 128, y: 128, r: 128 });
		expect(worldDisc({ x: 100, y: 10 }, 2)).toEqual({ x: 412, y: 502, r: 512 });
	});
	test('blur ≈ 0.9 zones, capped at 24 px', () => {
		expect(fogBlur(1.5)).toBeCloseTo((0.9 * 64 * 256 * 2 ** 1.5) / 21000, 6);
		expect(fogBlur(6)).toBe(24);
	});
});

describe('JS blur fallback (no canvas filter)', () => {
	test('box radii for 3 passes approximate the gaussian variance', () => {
		// Whole-pixel boxes: coarse for a tiny sigma, close from the ≥ 2 px
		// the scratch downscale keeps (blurScale).
		for (const [sigma, tol] of [
			[1, 0.4],
			[2, 0.2],
			[2.5, 0.2],
			[4, 0.1],
			[9, 0.05]
		]) {
			const radii = boxRadiiForGauss(sigma);
			expect(radii).toHaveLength(3);
			// A box of radius r has variance ((2r+1)² − 1) / 12; passes add up.
			const v = radii.reduce((a, r) => a + ((2 * r + 1) ** 2 - 1) / 12, 0);
			expect(Math.abs(v - sigma * sigma) / (sigma * sigma), `sigma ${sigma}`).toBeLessThan(tol);
		}
		expect(boxRadiiForGauss(0)).toEqual([0, 0, 0]);
	});

	test('boxBlur: radius 0 is the identity; the mean over the box elsewhere (zero outside)', () => {
		const a = new Float32Array([0, 0, 9, 0, 0, 0, 0, 0, 0]);
		boxBlur(a, 3, 3, 0);
		expect([...a]).toEqual([0, 0, 9, 0, 0, 0, 0, 0, 0]);
		const b = new Float32Array(5 * 5);
		b[2 * 5 + 2] = 9;
		boxBlur(b, 5, 5, 1);
		// A 3×3 box: the impulse spreads evenly over its 3×3 neighbourhood.
		for (let y = 0; y < 5; y++)
			for (let x = 0; x < 5; x++) {
				const inside = Math.abs(x - 2) <= 1 && Math.abs(y - 2) <= 1;
				expect(b[y * 5 + x]).toBeCloseTo(inside ? 1 : 0, 5);
			}
	});

	/** Direct (non-separable) box blur, zero outside the image: the same maths boxBlur computes via two passes. */
	function bruteBoxBlur(a: Float32Array, w: number, h: number, r: number): Float32Array {
		const out = new Float32Array(w * h);
		const n = (2 * r + 1) * (2 * r + 1);
		for (let y = 0; y < h; y++) {
			for (let x = 0; x < w; x++) {
				let sum = 0;
				for (let dy = -r; dy <= r; dy++) {
					for (let dx = -r; dx <= r; dx++) {
						const xx = x + dx;
						const yy = y + dy;
						if (xx >= 0 && xx < w && yy >= 0 && yy < h) sum += a[yy * w + xx];
					}
				}
				out[y * w + x] = sum / n;
			}
		}
		return out;
	}

	test('boxBlur: matches a brute-force reference, including content on the image border', () => {
		const w = 7;
		const h = 6;
		const r = 2;
		const a = new Float32Array(w * h);
		// Content right on the edges and in a corner, where the zero-outside
		// boundary (not a clamped/wrapped one) matters most.
		a[0] = 100; // top-left corner
		a[w - 1] = 80; // top-right corner
		a[(h - 1) * w] = 60; // bottom-left corner
		a[(h - 1) * w + (w - 1)] = 40; // bottom-right corner
		a[3] = 255; // top edge, middle column
		a[(h - 1) * w + 3] = 255; // bottom edge, middle column
		a[2 * w + 0] = 90; // left edge
		a[2 * w + (w - 1)] = 70; // right edge

		const expected = bruteBoxBlur(a, w, h, r);
		const actual = new Float32Array(a);
		boxBlur(actual, w, h, r);
		for (let i = 0; i < actual.length; i++) expect(actual[i], `cell ${i}`).toBeCloseTo(expected[i], 5);
	});

	test('gaussBlur: an impulse keeps its mass, stays symmetric, spreads with variance ≈ σ²', () => {
		const W = 61;
		const sigma = 4;
		const a = new Float32Array(W * W);
		a[30 * W + 30] = 1000;
		gaussBlur(a, W, W, sigma);
		let sum = 0;
		let vx = 0;
		for (let y = 0; y < W; y++)
			for (let x = 0; x < W; x++) {
				const v = a[y * W + x];
				sum += v;
				vx += v * (x - 30) ** 2;
			}
		expect(sum).toBeCloseTo(1000, 1);
		expect(Math.abs(vx / sum - sigma * sigma) / (sigma * sigma)).toBeLessThan(0.2);
		expect(a[30 * W + 26]).toBeCloseTo(a[30 * W + 34], 4);
		expect(a[26 * W + 30]).toBeCloseTo(a[34 * W + 30], 4);
		expect(a[30 * W + 30]).toBeLessThan(1000 / (2 * Math.PI * sigma * sigma) * 1.3);
		// Monotone fall-off from the centre.
		for (let x = 30; x < 45; x++) expect(a[30 * W + x + 1]).toBeLessThanOrEqual(a[30 * W + x] + 1e-6);
	});

	test('gaussBlur: a solid block stays ~solid inside and softens across its edge', () => {
		const W = 60;
		const a = new Float32Array(W * W);
		for (let y = 0; y < W; y++) for (let x = 10; x < 40; x++) a[y * W + x] = 255;
		gaussBlur(a, W, W, 3);
		const row = 30 * W;
		expect(a[row + 25]).toBeCloseTo(255, 0);
		expect(a[row + 55]).toBeCloseTo(0, 0);
		// Half way at the edge (between the last solid and the first empty column).
		expect(Math.abs((a[row + 39] + a[row + 40]) / 2 - 127.5)).toBeLessThan(2);
		expect(a[row + 36]).toBeGreaterThan(a[row + 39]);
		expect(a[row + 43]).toBeLessThan(a[row + 40]);
	});
});

describe('createFogLayer', () => {
	type FakeEl = { className?: string; style: Record<string, string>; parentNode?: FakePane; width?: number; height?: number };
	type FakePane = { children: FakeEl[]; appendChild(c: FakeEl): void; removeChild(c: FakeEl): void };
	function fakeMap() {
		const pane: FakePane = {
			children: [],
			appendChild(c) {
				c.parentNode = pane;
				pane.children.push(c);
			},
			removeChild(c) {
				pane.children = pane.children.filter((x) => x !== c);
				c.parentNode = undefined;
			}
		};
		const map = {
			pane,
			getPane: (name: string) => (name === 'fog' ? pane : undefined),
			getSize: () => L.point(400, 200),
			containerPointToLayerPoint: (p: L.Point) => p.add([10, 20]),
			getPixelOrigin: () => L.point(1000, 2000),
			getCenter: () => L.latLng(0, 0),
			getZoom: () => 3
		};
		return map;
	}

	test('a padded canvas layer in the fog pane, with redraw()', () => {
		const layer = createFogLayer(() => undefined);
		expect(layer.options.pane).toBe('fog');
		expect((layer.options as { padding?: number }).padding).toBe(FOG_PADDING);
		expect(FOG_PADDING).toBe(0.25);
		expect(typeof layer.redraw).toBe('function');
		expect(layer).toBeInstanceOf(L.Layer);
	});

	test('adds exactly one canvas, sized to the view plus padding, and removes it', () => {
		const map = fakeMap();
		const layer = createFogLayer(() => undefined) as unknown as {
			_map: unknown;
			onAdd(m: unknown): void;
			onRemove(m: unknown): void;
			redraw(): void;
		};
		layer._map = map;
		layer.onAdd(map);
		expect(map.pane.children.length).toBe(1);
		const c = map.pane.children[0];
		expect(c.className).toContain('fs-fog');
		// 400×200 view, padding 0.25 → 600×300 CSS px at DPR 1.
		expect(c.style.width).toBe('600px');
		expect(c.style.height).toBe('300px');
		expect(c.width).toBe(600);
		expect(c.height).toBe(300);
		layer.redraw();
		expect(map.pane.children.length).toBe(1);
		// A view reset repositions on whole pixels and does not re-apply
		// L.Renderer's sub-pixel _updateTransform.
		const r = layer as unknown as { _reset(): void; _updateTransform(): void };
		let transforms = 0;
		r._updateTransform = () => void transforms++;
		r._reset();
		expect(transforms).toBe(0);
		expect((c as unknown as { _leaflet_pos: L.Point })._leaflet_pos).toEqual(L.point(-90, -30));
		layer.onRemove(map);
		expect(map.pane.children.length).toBe(0);
		// Re-adding builds a fresh canvas.
		layer.onAdd(map);
		expect(map.pane.children.length).toBe(1);
		layer.onRemove(map);
	});

	/** A fake requestAnimationFrame: callbacks run only on flush(). */
	function fakeRaf() {
		let queue: FrameRequestCallback[] = [];
		const raf = vi.fn((cb: FrameRequestCallback) => queue.push(cb));
		const caf = vi.fn((id: number) => {
			queue[id - 1] = () => {};
		});
		vi.stubGlobal('requestAnimationFrame', raf);
		vi.stubGlobal('cancelAnimationFrame', caf);
		return {
			raf,
			flush() {
				const q = queue;
				queue = [];
				for (const cb of q) cb(0);
			}
		};
	}

	type Internals = {
		_map: unknown;
		onAdd(m: unknown): void;
		onRemove(m: unknown): void;
		redraw(): void;
		getEvents(): Record<string, (this: unknown) => void>;
		_draw(): void;
	};
	/** Adds a fog layer to a fake map and counts its paints (after the first, on add). */
	function addCounted() {
		const map = fakeMap();
		const layer = createFogLayer(() => undefined) as unknown as Internals;
		layer._map = map;
		layer.onAdd(map);
		let draws = 0;
		const draw = layer._draw;
		layer._draw = function () {
			draws++;
			draw.call(this);
		};
		const ev = layer.getEvents();
		const fire = (...names: string[]) => names.forEach((n) => ev[n].call(layer));
		return { map, layer, fire, draws: () => draws };
	}

	afterEach(() => {
		vi.unstubAllGlobals();
	});

	test('repaints are coalesced into one animation frame', () => {
		const raf = fakeRaf();
		const { layer, fire, draws } = addCounted();
		// setView fires viewreset and moveend: one paint, on the next frame.
		fire('viewreset', 'moveend');
		expect(draws()).toBe(0);
		raf.flush();
		expect(draws()).toBe(1);
		// A window resize: several resize events, then moveend.
		fire('resize', 'resize', 'resize', 'moveend');
		layer.redraw();
		raf.flush();
		expect(draws()).toBe(2);
		expect(raf.raf).toHaveBeenCalledTimes(2);
		// Nothing pending: no further paint.
		raf.flush();
		expect(draws()).toBe(2);
		layer.onRemove(undefined);
	});

	test('a pending frame is dropped when the layer is removed', () => {
		const raf = fakeRaf();
		const { layer, fire, draws } = addCounted();
		fire('moveend');
		layer.onRemove(undefined);
		raf.flush();
		expect(draws()).toBe(0);
	});

	test('repaints when the device pixel ratio changes, and stops listening on remove', () => {
		const raf = fakeRaf();
		type Mql = { query: string; listeners: (() => void)[]; addEventListener(t: string, l: () => void): void; removeEventListener(t: string, l: () => void): void };
		const mqls: Mql[] = [];
		vi.stubGlobal(
			'matchMedia',
			vi.fn((query: string) => {
				const m: Mql = {
					query,
					listeners: [],
					addEventListener: (_t, l) => m.listeners.push(l),
					removeEventListener: (_t, l) => (m.listeners = m.listeners.filter((x) => x !== l))
				};
				mqls.push(m);
				return m;
			})
		);
		const { layer, draws } = addCounted();
		expect(mqls).toHaveLength(1);
		expect(mqls[0].query).toBe('(resolution: 1dppx)');
		expect(mqls[0].listeners).toHaveLength(1);
		// The page moves to a 2× screen.
		vi.stubGlobal('devicePixelRatio', 2);
		mqls[0].listeners[0]();
		raf.flush();
		expect(draws()).toBe(1);
		// Re-armed for the new ratio; the old query is released.
		expect(mqls[0].listeners).toHaveLength(0);
		expect(mqls).toHaveLength(2);
		expect(mqls[1].query).toBe('(resolution: 2dppx)');
		expect(mqls[1].listeners).toHaveLength(1);
		layer.onRemove(undefined);
		expect(mqls[1].listeners).toHaveLength(0);
	});

	test('listens to the view events it redraws on', () => {
		const layer = createFogLayer(() => undefined) as unknown as { getEvents(): Record<string, unknown>; _zoomAnimated: boolean };
		layer._zoomAnimated = true;
		const ev = layer.getEvents();
		for (const k of ['moveend', 'zoomend', 'viewreset', 'resize', 'zoom', 'zoomanim', 'move']) expect(ev[k], k).toBeTypeOf('function');
	});

	// The blur scratch canvas (scratchCanvas/releaseScratch) is a module-level
	// singleton reused across draws and freed when the fog layer is torn down.
	// _draw itself needs a working canvas 2D context to reach it (which the
	// Vitest/Node environment doesn't provide — _draw bails at `!ctx`), so
	// scratchCanvas is exercised directly and releaseScratch is exercised both
	// directly and through the real _destroyContainer wiring.
	describe('the blur scratch canvas', () => {
		function fakeCanvasEl(): { width: number; height: number; getContext(): null; style: Record<string, string> } {
			// Real HTMLCanvasElements default to 300×150; the Node test stub
			// (testing/leaflet-node.ts) returns a bare object with neither, so
			// scratchCanvas's `scratch.width < w` growth check would never fire.
			return { width: 300, height: 150, getContext: () => null, style: {} };
		}

		afterEach(() => {
			releaseScratch(); // don't leak the shared scratch into other tests
		});

		test('releaseScratch zeroes the previous canvas in place, and a later draw recreates it', () => {
			const spy = vi.spyOn(document, 'createElement').mockImplementation(() => fakeCanvasEl() as unknown as HTMLCanvasElement);
			const first = scratchCanvas(64, 32);
			expect(first.width).toBeGreaterThan(0);
			expect(first.height).toBeGreaterThan(0);

			releaseScratch();
			expect(first.width).toBe(0);
			expect(first.height).toBe(0);

			// "a later draw": the next call to scratchCanvas, as `_draw` makes.
			const second = scratchCanvas(64, 32);
			expect(second).not.toBe(first);
			expect(second.width).toBeGreaterThan(0);
			expect(second.height).toBeGreaterThan(0);
			spy.mockRestore();
		});

		test('releaseScratch is a no-op when nothing was ever drawn', () => {
			expect(() => releaseScratch()).not.toThrow();
		});

		test('_destroyContainer (via onRemove) releases the scratch canvas', () => {
			const spy = vi.spyOn(document, 'createElement').mockImplementation(() => fakeCanvasEl() as unknown as HTMLCanvasElement);
			const map = fakeMap();
			const layer = createFogLayer(() => undefined) as unknown as { _map: unknown; onAdd(m: unknown): void; onRemove(m: unknown): void };
			layer._map = map;
			layer.onAdd(map);

			// Simulate a draw having created the scratch canvas.
			const created = scratchCanvas(16, 16);
			expect(created.width).toBeGreaterThan(0);

			layer.onRemove(map);
			expect(created.width).toBe(0);
			expect(created.height).toBe(0);

			const recreated = scratchCanvas(16, 16);
			expect(recreated).not.toBe(created);
			spy.mockRestore();
		});
	});
});
