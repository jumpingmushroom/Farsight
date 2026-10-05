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
		// Cells are visited column by column: restore the input order.
		hits.sort((a, b) => a - b);
		return hits.map((i) => this.items[i]);
	}
}

/** World (x, z) ↔ map container pixels at the current view. */
export interface Projector {
	project(x: number, z: number): { x: number; y: number };
	unproject(px: number, py: number): { x: number; z: number };
}

export interface ViewPoint {
	id: string;
	x: number;
	y: number;
}

// World metres of slack on the query rect, so float rounding in unproject
// can never drop a marker the exact pixel test below would keep.
const SLACK_M = 1;

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
	const a = p.unproject(-cull, -cull);
	const b = p.unproject(size.x + cull, size.y + cull);
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
	/** portalPairs(visible). */
	pairs: [MapMarker, MapMarker][];
	grid: MarkerGrid;
}

export type VisibleCache = (
	all: MapMarker[],
	layers: Record<LayerKey, boolean>,
	mask: Uint8Array | undefined,
	fog: boolean,
	zoom: number
) => VisibleSet;

/**
 * A memoised visibleMarkers (plus its pairs and grid). The zoom only matters
 * through the markers' minZoom thresholds, so the cache keys on how many of
 * them the zoom has reached; layers are compared by value (the object may be
 * mutated in place or replaced by an equal copy).
 */
export function createVisibleCache(): VisibleCache {
	let lastAll: MapMarker[] | undefined;
	let thresholds: number[] = [];
	let key: { mask: Uint8Array | undefined; fog: boolean; layers: string; band: number } | undefined;
	let value: VisibleSet | undefined;

	return (all, layers, mask, fog, zoom) => {
		if (all !== lastAll) {
			lastAll = all;
			key = undefined;
			const t = new Set<number>();
			for (const m of all) if (m.minZoom !== undefined) t.add(m.minZoom);
			thresholds = [...t].sort((a, b) => a - b);
		}
		let band = 0;
		while (band < thresholds.length && zoom >= thresholds[band]) band++;
		const layerKey = (Object.keys(layers) as LayerKey[])
			.sort()
			.map((k) => `${k}:${layers[k] ? 1 : 0}`)
			.join(',');
		if (value && key && key.mask === mask && key.fog === fog && key.layers === layerKey && key.band === band) {
			return value;
		}
		const visible = visibleMarkers(all, layers, mask, fog, zoom);
		key = { mask, fog, layers: layerKey, band };
		value = {
			visible,
			byId: new Map(visible.map((m) => [m.id, m])),
			pairs: portalPairs(visible),
			grid: new MarkerGrid(visible)
		};
		return value;
	};
}
