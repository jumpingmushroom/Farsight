<!--
  The world map (DESIGN-NOTES §1.1 item 1, §5.5–§5.8, §7.3; fog per spec
  2026-10-01): Leaflet with the world CRS and the server's fog tiles (terrain
  with the fog of war drawn in) over a parchment-filled world disc, so a tile
  still loading looks fogged, never bare. The map is created once; the tile
  layer is replaced when the server, tile key or fog key changes. Leaflet's
  own zoom animation is on: the fog is in the tiles, so it can't lag them.

  Panes: disc 150 (below tiles 200), portalLines 380, markers use Leaflet's
  markerPane (600). The popover is DOM, not a pane.
-->
<script lang="ts">
	import L from 'leaflet';
	import { onMount, untrack } from 'svelte';
	import { tileUrl } from '$lib/api';
	import { CRS, MAX_BOUNDS, WORLD_BOUNDS, WORLD_RADIUS, toLatLng } from '$lib/geo';
	import type { Card, SnapshotView } from '$lib/types';

	let {
		card,
		snapshot,
		padLeft = 0,
		defaultZoom = 1.75,
		dim = 1,
		filter = '',
		onready,
		onclick,
		onmove
	}: {
		/** Undefined while a server switch loads: the map stays mounted, without tiles. */
		card: Card | undefined;
		/** Tiles are drawn only while this is a loaded snapshot (it names the fog tiles). */
		snapshot: SnapshotView | null | undefined;
		padLeft: number;
		/** The default view's zoom (§7.3 ruling: 1.75 desktop, 1.5 mobile). */
		defaultZoom?: number;
		dim: number;
		filter: string;
		onready?: (map: L.Map) => void;
		onclick?: () => void;
		onmove?: () => void;
	} = $props();

	let el: HTMLDivElement;
	let map = $state.raw<L.Map | undefined>(undefined);

	// The tile URL carries the snapshot's fog key: no tiles until a snapshot
	// is loaded — not while it loads (undefined), not before the first save
	// (null), and not when its fetch fails. The world disc stays.
	const tileSrc = $derived(
		snapshot?.fogKey && card && card.tiles.state === 'complete' && card.tiles.key
			? tileUrl(card.id, card.tiles.key, snapshot.fogKey)
			: undefined
	);

	/** Container point at the centre of the map area right of the panel. */
	function visibleCentre(m: L.Map): L.Point {
		const size = m.getSize();
		return L.point(size.x / 2 + padLeft / 2, size.y / 2);
	}

	/**
	 * Puts world (x, z) at the visible centre at `zoom`, or `dy` px below it
	 * (negative = above; the mobile shell puts a selected marker at y ≈ 300).
	 */
	export function centerOn(x: number, z: number, zoom: number, dy = 0): void {
		if (!map) return;
		const p = map.project(toLatLng(x, z), zoom).subtract([padLeft / 2, dy]);
		map.setView(map.unproject(p, zoom), zoom);
	}

	/** The default view: the world centred in the visible area at `defaultZoom`. */
	export function resetView(): void {
		centerOn(0, 0, defaultZoom);
	}

	/** Zooms by `delta` about the visible centre. */
	export function zoomBy(delta: number): void {
		if (!map) return;
		map.setZoomAround(visibleCentre(map), map.getZoom() + delta);
	}

	/**
	 * The side panel floats over the left `padLeft` px, so maxBounds limits
	 * the *visible* area right of it: Leaflet's bounds offset (used by
	 * setView, zoom and panInsideBounds) sees the view without that strip.
	 * When the world is smaller than the visible area this centres it at the
	 * visible centre (§1.1: x = 900 with the panel open) instead of under the
	 * panel. `_getBoundsOffset` is internal to Leaflet 1.9.4 (pinned).
	 *
	 * `padB` does the same for a strip at the bottom (the mobile docked
	 * marker card, see `setPadBottom`); it is 0 on desktop, so the desktop
	 * limit is unchanged.
	 */
	let pad = untrack(() => padLeft);
	let padB = 0;
	function padBoundsLimit(m: L.Map): void {
		type Limiter = { _getBoundsOffset(px: L.Bounds, b: L.LatLngBounds, zoom?: number): L.Point };
		const lm = m as unknown as Limiter;
		const orig = lm._getBoundsOffset;
		lm._getBoundsOffset = function (this: L.Map, px, b, zoom) {
			return orig.call(this, L.bounds(px.min!.add([pad, 0]), px.max!.subtract([0, padB])), b, zoom);
		};
	}
	/**
	 * Treats the bottom `px` of the map as covered (mobile: the docked card
	 * area), so maxBounds lets the view pan far enough to put a marker at the
	 * centre of what's left (y ≈ 300). Synchronous, so a following `centerOn`
	 * is limited by it. Back to 0 re-applies the normal limit.
	 */
	export function setPadBottom(px: number): void {
		const next = Math.max(0, Math.round(px));
		if (next === padB) return;
		padB = next;
		if (next === 0) map?.panInsideBounds(MAX_BOUNDS);
	}

	$effect(() => {
		const p = padLeft;
		if (p === pad) return;
		pad = p;
		untrack(() => map?.panInsideBounds(MAX_BOUNDS));
	});

	onMount(() => {
		const m = L.map(el, {
			crs: CRS,
			zoomSnap: 0.25,
			zoomDelta: 0.75,
			wheelPxPerZoomLevel: 90,
			minZoom: 1,
			maxZoom: 6,
			maxBounds: MAX_BOUNDS,
			maxBoundsViscosity: 0.8,
			attributionControl: false,
			zoomControl: false
		});
		padBoundsLimit(m);
		m.createPane('disc').style.zIndex = '150';
		m.createPane('portalLines').style.zIndex = '380';

		L.circle([0, 0], {
			radius: WORLD_RADIUS,
			pane: 'disc',
			className: 'world-disc',
			interactive: false
		}).addTo(m);

		// The default view: the world centre at the visible centre.
		const z0 = untrack(() => defaultZoom);
		const start = m.project(toLatLng(0, 0), z0).subtract([pad / 2, 0]);
		m.setView(m.unproject(start, z0), z0, { animate: false });

		m.on('click', () => onclick?.());
		m.on('move zoom', () => onmove?.());
		map = m;
		onready?.(m);
		return () => {
			map = undefined;
			m.remove();
		};
	});

	// Tile layer: replaced when the server, tile key or fog key changes.
	// Fog tiles exist natively up to zoom 6.
	$effect(() => {
		const m = map;
		const src = tileSrc;
		if (!m || !src) return;
		const layer = L.tileLayer(src, {
			tileSize: 256,
			minZoom: 0,
			maxNativeZoom: 6,
			maxZoom: 6,
			noWrap: true,
			bounds: WORLD_BOUNDS,
			keepBuffer: 2
		}).addTo(m);
		return () => {
			layer.remove();
		};
	});

	// Tile-pane filter (layers off, offline, stale; §3.22).
	$effect(() => {
		const pane = map?.getPane('tilePane');
		if (pane) pane.style.filter = filter;
	});
</script>

<div
	class="atlas-map"
	bind:this={el}
	style:filter={dim < 1 ? `brightness(${dim})` : undefined}
	data-testid="atlas-map"
></div>

<style>
	.atlas-map {
		position: absolute;
		inset: 0;
		isolation: isolate;
		touch-action: none;
		user-select: none;
		transition: filter 0.4s;
	}
	.atlas-map :global(.leaflet-tile-pane) {
		transition: filter 0.4s;
	}
	/* Parchment (#cfbe9c, the fog colour): tiles that are still loading look
	   fogged rather than empty. */
	.atlas-map :global(.world-disc) {
		fill: #cfbe9c;
		fill-opacity: 1;
		stroke: color-mix(in srgb, var(--color-text) 12%, transparent);
		stroke-opacity: 1;
		stroke-width: 5px;
		filter: drop-shadow(0 30px 80px rgba(0, 0, 0, 0.35));
	}
</style>
