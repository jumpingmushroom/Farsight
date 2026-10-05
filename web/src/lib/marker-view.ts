// The map's per-render marker work, kept proportional to what is on screen
// (perf: zoom/pan stutter on large servers). MarkerLayer used to re-filter,
// re-pair and project every marker on every moveend; now:
//
//  - createVisibleCache() filters (layers, fog mask, minZoom) and pairs the
//    portals only when an input changes: the marker list, the mask, fog,
//    a layer switch, or the zoom crossing some marker's minZoom. A plain
//    pan, or a zoom inside the same band, reuses the cached set.
//  - The cached set carries a MarkerGrid, a coarse spatial grid in world
//    metres, so viewPoints() projects only the markers in the view (plus the
//    cull margin), not all of them. Its output is exactly the old full scan's:
//    the same markers, in the same (input) order, at the same points — the
//    clusterer is order-sensitive, so the order matters.

import { CRS, TRANSFORM } from './geo';
import { portalPairs, visibleMarkers, type LayerKey, type MapMarker } from './markers';

const CELL_M = 512;

export interface ScanStats {
	/** Markers whose coordinates the query compared against the rect. */
	scanned: number;
}

/** A static grid over `items` in world (x, z); queries keep the input order. */
export class MarkerGrid {
	private readonly items: MapMarker[];
	private readonly cells = new Map<number, number[]>();
	private readonly c0x: number;
	private readonly c0z: number;
	private readonly c1x: number;
	private readonly c1z: number;

	constructor(items: MapMarker[], private readonly cell = CELL_M) {
		this.items = items;
		let c0x = Infinity, c0z = Infinity, c1x = -Infinity, c1z = -Infinity;
		for (const [i, m] of items.entries()) {
			const cx = Math.floor(m.x / cell);
			const cz = Math.floor(m.z / cell);
			if (cx < c0x) c0x = cx;
			if (cz < c0z) c0z = cz;
			if (cx > c1x) c1x = cx;
			if (cz > c1z) c1z = cz;
			const k = this.key(cx, cz);
			const list = this.cells.get(k);
			if (list) list.push(i);
			else this.cells.set(k, [i]);
		}
		this.c0x = c0x;
		this.c0z = c0z;
		this.c1x = c1x;
		this.c1z = c1z;
	}

	private key(cx: number, cz: number): number {
		// Cells are 512 m; any world fits well inside ±2^20 cells per axis.
		return (cx + 0x100000) * 0x200000 + (cz + 0x100000);
	}

	/** The items with minX ≤ x ≤ maxX and minZ ≤ z ≤ maxZ, in input order. */
	query(minX: number, minZ: number, maxX: number, maxZ: number, stats?: ScanStats): MapMarker[] {
		if (!(minX <= maxX && minZ <= maxZ) || this.items.length === 0) return [];
		const ax = Math.max(this.c0x, Math.floor(minX / this.cell));
		const az = Math.max(this.c0z, Math.floor(minZ / this.cell));
		const bx = Math.min(this.c1x, Math.floor(maxX / this.cell));
		const bz = Math.min(this.c1z, Math.floor(maxZ / this.cell));
		const hits: number[] = [];
		let scanned = 0;
		for (let cx = ax; cx <= bx; cx++) {
			for (let cz = az; cz <= bz; cz++) {
				const list = this.cells.get(this.key(cx, cz));
				if (!list) continue;
				for (const i of list) {
					scanned++;
					const m = this.items[i];
					if (m.x >= minX && m.x <= maxX && m.z >= minZ && m.z <= maxZ) hits.push(i);
				}
			}
		}
		if (stats) stats.scanned += scanned;
		// Cells are visited column by column: restore the input order (a
		// typed array sorts numerically, natively).
		const order = Int32Array.from(hits).sort();
		const out: MapMarker[] = new Array(order.length);
		for (let i = 0; i < order.length; i++) out[i] = this.items[order[i]];
		return out;
	}
}

/** World (x, z) ↔ map container pixels at the current view. */
export interface Projector {
	project(x: number, z: number): { x: number; y: number };
	unproject(px: number, py: number): { x: number; z: number };
}

/**
 * Map#latLngToContainerPoint for world (x, z), without its four allocations
 * per call: the same float steps in the same order (Leaflet 1.9.4: project
 * with the CRS transformation, round, subtract the pixel origin, add the map
 * pane position), so the result is bit-for-bit Leaflet's. Valid while the
 * view (zoom, origin, pane position) doesn't change.
 */
export function containerProjector(
	zoom: number,
	origin: { x: number; y: number },
	pane: { x: number; y: number }
): (x: number, z: number) => { x: number; y: number } {
	const scale = CRS.scale(zoom);
	const { a, b, c, d } = TRANSFORM;
	return (x, z) => ({
		x: Math.round(scale * (a * x + b)) - origin.x + pane.x,
		y: Math.round(scale * (c * z + d)) - origin.y + pane.y
	});
}

export interface ViewPoint {
	id: string;
	x: number;
	y: number;
}

// World metres of slack on the query rect, so float rounding in unproject
// can never drop a marker the exact pixel test below would keep.
const SLACK_M = 1;
// Pixels of padding on the query rect: Leaflet rounds projected points, so a
// marker up to 0.5 px past the cull line rounds back inside it (half a pixel
// is ~20 m at zoom 1, more than SLACK_M covers).
const PAD_PX = 1;

/**
 * The markers within `cull` px of the `size` view, projected to container
 * points, in the grid's input order: exactly what projecting and culling
 * every marker would give, but only the grid cells around the view are
 * looked at, and only the markers in the view rect are projected.
 */
export function viewPoints(
	grid: MarkerGrid,
	p: Projector,
	size: { x: number; y: number },
	cull: number,
	stats?: ScanStats
): ViewPoint[] {
	const a = p.unproject(-cull - PAD_PX, -cull - PAD_PX);
	const b = p.unproject(size.x + cull + PAD_PX, size.y + cull + PAD_PX);
	const near = grid.query(
		Math.min(a.x, b.x) - SLACK_M,
		Math.min(a.z, b.z) - SLACK_M,
		Math.max(a.x, b.x) + SLACK_M,
		Math.max(a.z, b.z) + SLACK_M,
		stats
	);
	const out: ViewPoint[] = [];
	for (const m of near) {
		const q = p.project(m.x, m.z);
		if (q.x < -cull || q.y < -cull || q.x > size.x + cull || q.y > size.y + cull) continue;
		out.push({ id: m.id, x: q.x, y: q.y });
	}
	return out;
}

export interface VisibleSet {
	/** visibleMarkers(...) for the inputs. */
	visible: MapMarker[];
	byId: Map<string, MapMarker>;
	/** Each marker's index in `visible`: its stable stacking rank. */
	rank: Map<string, number>;
	/** portalPairs(visible). */
	pairs: [MapMarker, MapMarker][];
	grid: MarkerGrid;
}

export interface VisibleCache {
	(
		all: MapMarker[],
		layers: Record<LayerKey, boolean>,
		mask: Uint8Array | undefined,
		fog: boolean,
		zoom: number
	): VisibleSet;
	/**
	 * Computes one not-yet-cached zoom band's set for these inputs (for idle
	 * time, so a zoom crossing a minZoom later finds it ready); true while
	 * more bands remain.
	 */
	warm(all: MapMarker[], layers: Record<LayerKey, boolean>, mask: Uint8Array | undefined, fog: boolean): boolean;
}

/**
 * A memoised visibleMarkers (plus its pairs and grid). The zoom only matters
 * through the markers' minZoom thresholds, so sets are cached per band (how
 * many of them the zoom has reached) for the current marker list, mask, fog
 * and layers; layers are compared by value (the object may be mutated in
 * place or replaced by an equal copy). Any other change drops every band.
 */
export function createVisibleCache(): VisibleCache {
	let lastAll: MapMarker[] | undefined;
	let thresholds: number[] = [];
	let key: { mask: Uint8Array | undefined; fog: boolean; layers: string } | undefined;
	const bands = new Map<number, VisibleSet>();

	function sync(all: MapMarker[], layers: Record<LayerKey, boolean>, mask: Uint8Array | undefined, fog: boolean): void {
		if (all !== lastAll) {
			lastAll = all;
			key = undefined;
			const t = new Set<number>();
			for (const m of all) if (m.minZoom !== undefined) t.add(m.minZoom);
			thresholds = [...t].sort((a, b) => a - b);
		}
		const layerKey = (Object.keys(layers) as LayerKey[])
			.sort()
			.map((k) => `${k}:${layers[k] ? 1 : 0}`)
			.join(',');
		if (key && key.mask === mask && key.fog === fog && key.layers === layerKey) return;
		key = { mask, fog, layers: layerKey };
		bands.clear();
	}

	function compute(all: MapMarker[], layers: Record<LayerKey, boolean>, mask: Uint8Array | undefined, fog: boolean, zoom: number): VisibleSet {
		const visible = visibleMarkers(all, layers, mask, fog, zoom);
		return {
			visible,
			byId: new Map(visible.map((m) => [m.id, m])),
			rank: new Map(visible.map((m, i) => [m.id, i])),
			pairs: portalPairs(visible),
			grid: new MarkerGrid(visible)
		};
	}

	const get = ((all, layers, mask, fog, zoom) => {
		sync(all, layers, mask, fog);
		let band = 0;
		while (band < thresholds.length && zoom >= thresholds[band]) band++;
		let value = bands.get(band);
		if (!value) {
			value = compute(all, layers, mask, fog, zoom);
			bands.set(band, value);
		}
		return value;
	}) as VisibleCache;

	get.warm = (all, layers, mask, fog) => {
		sync(all, layers, mask, fog);
		for (let band = 0; band <= thresholds.length; band++) {
			if (bands.has(band)) continue;
			// Band b covers zooms from thresholds[b - 1] up to thresholds[b].
			const zoom = band === 0 ? -Infinity : thresholds[band - 1];
			bands.set(band, compute(all, layers, mask, fog, zoom));
			return bands.size <= thresholds.length;
		}
		return false;
	};

	return get;
}
