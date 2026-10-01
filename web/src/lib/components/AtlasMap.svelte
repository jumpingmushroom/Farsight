<!--
  The world map (DESIGN-NOTES §1.1 item 1, §5.5–§5.8, §7.3): Leaflet with the
  world CRS, the server's tile pyramid over a styled world disc, and the fog
  of war in its own pane. The map is created once; the tile layer is replaced
  when the server or tile key changes, and the fog is redrawn per snapshot.

  Panes: disc 150 (below tiles 200), fog 350, portalLines 380, markers use
  Leaflet's markerPane (600). The popover is DOM, not a pane.
-->
<script lang="ts">
	import L from 'leaflet';
	import { onMount, untrack } from 'svelte';
	import { tileUrl } from '$lib/api';
	import { createFogLayer, maskForZones, type FogLayer } from '$lib/fog';
	import { CRS, MAX_BOUNDS, WORLD_BOUNDS, WORLD_RADIUS, toLatLng } from '$lib/geo';
	import type { Card, SnapshotView } from '$lib/types';

	let {
		card,
		snapshot,
		padLeft = 0,
		defaultZoom = 1.75,
		dim = 1,
		filter = '',
		fog = true,
		onready,
		onclick,
		onmove
	}: {
		/** Undefined while a server switch loads: the map stays mounted, without tiles. */
		card: Card | undefined;
		/** Tiles are drawn only while this is a loaded snapshot (the fog needs it). */
		snapshot: SnapshotView | null | undefined;
		padLeft: number;
		/** The default view's zoom (§7.3 ruling: 1.75 desktop, 1.5 mobile). */
		defaultZoom?: number;
		dim: number;
		filter: string;
		fog: boolean;
		onready?: (map: L.Map) => void;
		onclick?: () => void;
		onmove?: () => void;
	} = $props();

	let el: HTMLDivElement;
	let map = $state.raw<L.Map | undefined>(undefined);

	// Terrain is never shown without fog: no tiles until a snapshot (the fog
	// mask) is loaded — not while it loads (undefined), not before the first
	// save (null), and not when its fetch fails. The world disc stays.
	const tileSrc = $derived(
		snapshot && card && card.tiles.state === 'complete' && card.tiles.key
			? tileUrl(card.id, card.tiles.key)
			: undefined
	);
	const zones = $derived(snapshot?.exploredZones);
	const mask = $derived(zones ? maskForZones(zones) : undefined);
	const showFog = $derived(fog && !!snapshot);

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
			zoomControl: false,
			// No zoom animation: mid-animation Leaflet shows new-level tiles
			// at the target zoom while the fog canvas is still CSS-scaling
			// toward it (and a stalled compositor can hold that transition at
			// its start), so the fog would trail the terrain and bare it.
			// Without it, tiles and fog change zoom in the same frame.
			zoomAnimation: false
		});
		padBoundsLimit(m);
		m.createPane('disc').style.zIndex = '150';
		const fogPane = m.createPane('fog');
		fogPane.style.zIndex = '350';
		fogPane.style.pointerEvents = 'none';
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

	// Tile layer: replaced when the server or tile key changes.
	$effect(() => {
		const m = map;
		const src = tileSrc;
		if (!m || !src) return;
		const layer = L.tileLayer(src, {
			tileSize: 256,
			minZoom: 0,
			maxNativeZoom: 5,
			maxZoom: 6,
			noWrap: true,
			bounds: WORLD_BOUNDS,
			keepBuffer: 2
		}).addTo(m);
		return () => {
			layer.remove();
		};
	});

	// Fog: present while there's a snapshot and the fog is on; redrawn
	// whenever the mask changes (a new snapshot).
	let fogLayer: FogLayer | undefined;
	$effect(() => {
		const m = map;
		if (!m || !showFog) return;
		const layer = createFogLayer(() => mask).addTo(m);
		fogLayer = layer;
		return () => {
			fogLayer = undefined;
			layer.remove();
		};
	});
	$effect(() => {
		void mask;
		untrack(() => fogLayer?.redraw());
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
	.atlas-map :global(.world-disc) {
		fill: color-mix(in srgb, var(--color-text) 6%, transparent);
		fill-opacity: 1;
		stroke: color-mix(in srgb, var(--color-text) 12%, transparent);
		stroke-opacity: 1;
		stroke-width: 5px;
		filter: drop-shadow(0 30px 80px rgba(0, 0, 0, 0.35));
	}
</style>
