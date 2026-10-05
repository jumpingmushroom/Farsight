<!--
  Map markers (DESIGN-NOTES §4.1, §5.1, §5.4). Renders nothing itself: it
  keeps Leaflet layers on `map` in sync with the visible markers.

  - On zoomend / moveend (two rAFs later, coalesced) and on prop changes it
    projects the visible markers to container points, culls to the viewport
    ±40 px, and clusters them (radius 26, the selected marker locked; not
    at max zoom, where a cluster could no longer be split; the clusterer is
    grid-indexed, cluster.ts).
  - One L.marker per group with an L.divIcon, diffed by group key (the ids
    in their stable order), so unchanged pins keep their DOM: a pin is only
    moved, and its icon only replaced, when its HTML changes. Pins stack by
    their first member's rank in the visible set, so a pan never restacks
    the pins that stay.
  - The DOM work (create, remove, move, re-icon, restack) is time-sliced
    (slicer.ts): ~8 ms per animation frame, the selected pin first, then
    nearest the view centre; a newer render drops the old queue. On a zoom
    that changes hundreds of pins this replaced one 400–600 ms task (4×
    CPU throttle) with frame-sized ones.
  - The visible set (layers, fog, minZoom) and its portal pairs are cached
    and only recomputed when an input changes or the zoom crosses a minZoom
    (createVisibleCache); a spatial grid over it means a render projects only
    the markers near the view (viewPoints), not every marker — on a large
    server a pan used to project and cull thousands (marker-view.ts).
  - Portal lines are at most one multi-line L.polyline per style class
    (.pl, .pl-sel, .pl-dim in app.css) in the `portalLines` pane, updated in
    place with setLatLngs — not one polyline per pair, all re-added on any
    change and all reprojected by the SVG renderer on every moveend.
-->
<script lang="ts">
	import L from 'leaflet';
	import { untrack } from 'svelte';
	import { cluster } from '$lib/cluster';
	import { fromLatLng, toLatLng } from '$lib/geo';
	import { containerProjector, createVisibleCache, viewPoints } from '$lib/marker-view';
	import { browserScheduler, createSlicer } from '$lib/slicer';
	import {
		clusterHtml,
		clusterSize,
		clusterTooltip,
		pinHtml,
		type LayerKey,
		type MapMarker
	} from '$lib/markers';

	let {
		map,
		all,
		layers,
		mask,
		fog,
		selectedId,
		clustering = true,
		portalLinks = true,
		onselect
	}: {
		map: L.Map;
		all: MapMarker[];
		layers: Record<LayerKey, boolean>;
		mask: Uint8Array | undefined;
		fog: boolean;
		selectedId?: string;
		clustering?: boolean;
		portalLinks?: boolean;
		onselect: (id: string | undefined) => void;
	} = $props();

	const RADIUS = 26;
	const CULL = 40;
	const LABEL_ZOOM = 1.5;
	const CLUSTER_ZOOM_STEP = 1.25;
	// Leaflet's marker z-index is its layer-point y plus zIndexOffset (plus
	// riseOffset while hovered). The order step exceeds any y (16,384 px
	// across the world at max zoom); the order is capped so it stays below the
	// selected pin's offset, and every combination (a hovered selected pin is
	// 2·SELECTED_Z + y) stays inside int32.
	const ORDER_Z = 20_000;
	const MAX_ORDER = 49_000; // 9.8e8 < SELECTED_Z
	const SELECTED_Z = 1_000_000_000;

	interface Entry {
		marker: L.Marker;
		/** The icon HTML plus the tooltip: a change replaces the icon's content. */
		sig: string;
	}

	/** What one group should look like on the map. */
	interface Target {
		key: string;
		latlng: L.LatLng;
		html: string;
		title: string;
		iconSize: number;
		pin: boolean;
		id: string | undefined;
		zOffset: number;
		sig: string;
	}

	const entries = new Map<string, Entry>();
	const getVisible = createVisibleCache();
	// The marker DOM work (creating, removing, moving, re-iconing pins) runs
	// time-sliced, ~8 ms per frame, so a zoom that changes hundreds of pins
	// never blocks the main thread for long. A newer render supersedes the
	// queue: it diffs against `entries`, which only ever holds what is
	// actually on the map, so nothing is duplicated or left stale.
	const slicer = createSlicer(browserScheduler, 8);
	let frame = 0;

	const unproject = (px: number, py: number) => fromLatLng(map.containerPointToLatLng([px, py]));

	/**
	 * Restacks a pin without setZIndexOffset, which also reprojects and
	 * re-positions it. `_zIndex` and `_resetZIndex` are internal to Leaflet
	 * 1.9.4 (pinned; leaflet-internals.test.ts).
	 */
	function setZ(m: L.Marker, z: number): void {
		const was = m.options.zIndexOffset ?? 0;
		if (was === z) return;
		m.options.zIndexOffset = z;
		// Leaflet keeps `_zIndex` = layer-point y + zIndexOffset (set in
		// _setPos) and writes `_zIndex + rise` to the element: swap the offset
		// in it, then write it as a mouseout would.
		const lm = m as unknown as { _zIndex?: number; _resetZIndex(): void };
		if (lm._zIndex === undefined) return;
		lm._zIndex += z - was;
		lm._resetZIndex();
	}

	function render(): void {
		const started = performance.now();
		// A props render while a map-event one is pending stands in for it.
		if (frame) cancelAnimationFrame(frame);
		frame = 0;
		const zoom = map.getZoom();
		const { byId, rank, pairs, grid } = getVisible(all, layers, mask, fog, zoom);
		const size = map.getSize();
		const project = containerProjector(
			zoom,
			map.getPixelOrigin(),
			L.DomUtil.getPosition(map.getPane('mapPane')!) ?? L.point(0, 0)
		);
		const pts = viewPoints(grid, { project, unproject }, size, CULL);
		// At max zoom a cluster click can't zoom further, so markers closer
		// than the radius there (a portal inside a base) would be unreachable:
		// show them unclustered instead.
		const groups = clustering && zoom < map.getMaxZoom()
			? cluster(pts, RADIUS, selectedId)
			: pts.map((p) => ({ ids: [p.id], x: p.x, y: p.y }));
		const cx = size.x / 2;
		const cy = size.y / 2;
		const dist = (x: number, y: number) => (x - cx) ** 2 + (y - cy) ** 2;

		// Synchronously: each group's key, position and slicing priority
		// (the selected pin first, then nearest the view centre), and which
		// markers stay or go. Everything else (the pin HTML, tooltips, and all
		// DOM work) runs in the sliced tasks, built from this render's inputs
		// even in a later frame.
		const sel = selectedId;
		const keep = new Set<string>();
		const work: { prio: number; run: () => void }[] = [];
		for (const g of groups) {
			// Groups come in the order of their first member in the visible
			// set (the clusterer creates them in input order), so that member's
			// rank stacks them exactly as their order here would, but it stays
			// put when a pan adds or drops groups: kept pins aren't restacked.
			const order = rank.get(g.ids[0])!;
			// The ids come in the markers' stable order (viewPoints keeps it and
			// the clusterer appends in input order), so the same members always
			// make the same key without sorting.
			const key = g.ids.join('|');
			keep.add(key);
			const isSelected = g.ids.length === 1 && g.ids[0] === sel;
			const prio = isSelected ? -1 : dist(g.x, g.y);
			// A cluster sits at its centroid: converted now, while the view is
			// the one the points were projected in.
			const at = g.ids.length > 1 ? map.containerPointToLatLng([g.x, g.y]) : undefined;
			const target = () => targetFor(g.ids, key, order, at, byId, zoom, sel);
			const existing = entries.get(key);
			if (!existing) {
				work.push({ prio, run: () => create(target()) });
				continue;
			}
			work.push({
				prio,
				run: () => {
					const t = target();
					if (existing.sig !== t.sig) return apply(existing, t);
					// No DOM rebuild: only touch what changed.
					setZ(existing.marker, t.zOffset);
					if (!existing.marker.getLatLng().equals(t.latlng)) existing.marker.setLatLng(t.latlng);
				}
			});
		}

		// Markers to go. (Pooling them for the new groups, setLatLng plus
		// setIcon instead of remove plus create, measured no faster.)
		for (const [key, e] of entries) {
			if (keep.has(key)) continue;
			const ll = e.marker.getLatLng();
			const p = project(ll.lng, ll.lat);
			work.push({
				prio: dist(p.x, p.y),
				run: () => {
					e.marker.remove();
					entries.delete(key);
				}
			});
		}
		work.sort((a, b) => a.prio - b.prio);
		slicer.run(
			work.map((w) => w.run),
			started
		);

		renderLines(pairs);
		scheduleWarm();
	}

	// In idle time, one band per callback, precompute the visible sets for
	// the other zoom bands (marker minZooms), so zooming across one later
	// doesn't refilter thousands of markers in the zoom's first frame.
	let warmHandle: { cancel(): void } | undefined;
	function scheduleWarm(): void {
		if (warmHandle) return;
		const step = () => {
			warmHandle = undefined;
			if (getVisible.warm(all, layers, mask, fog)) scheduleWarm();
		};
		if (typeof requestIdleCallback === 'function') {
			const id = requestIdleCallback(step, { timeout: 2000 });
			warmHandle = { cancel: () => cancelIdleCallback(id) };
		} else {
			const id = setTimeout(step, 300);
			warmHandle = { cancel: () => clearTimeout(id) };
		}
	}

	/** A group's pin or cluster as it should look (pure; run inside the slices). */
	function targetFor(
		ids: string[],
		key: string,
		order: number,
		at: L.LatLng | undefined,
		byId: Map<string, MapMarker>,
		zoom: number,
		sel: string | undefined
	): Target {
		const members = ids.map((id) => byId.get(id)!);
		let html: string;
		let latlng: L.LatLng;
		let title: string;
		let iconSize: number;
		let single: MapMarker | undefined;
		if (members.length === 1) {
			single = members[0];
			const selected = single.id === sel;
			html = pinHtml(single, { selected, showLabel: zoom >= LABEL_ZOOM });
			latlng = toLatLng(single.x, single.z);
			title = single.tooltip;
			iconSize = single.pin.size;
		} else {
			html = clusterHtml(members.length);
			latlng = at!;
			title = clusterTooltip(members);
			iconSize = clusterSize(members.length);
		}
		// Stack in marker order like the design (later kinds on top, so a
		// portal inside a base stays visible), not Leaflet's by-latitude
		// order; the selected pin above everything.
		const isSelected = single !== undefined && single.id === sel;
		return {
			key,
			latlng,
			html,
			title,
			iconSize,
			pin: single !== undefined,
			id: single?.id,
			zOffset: isSelected ? SELECTED_Z : Math.min(order, MAX_ORDER) * ORDER_Z,
			sig: `${html}\n${title}`
		};
	}

	/** New icon content (and title, position, z) for a marker; keeps keyboard focus on it. */
	function apply(e: Entry, t: Target): void {
		const m = e.marker;
		const el = m.getElement();
		const hadFocus = el !== undefined && document.activeElement === el;
		m.options.title = t.title;
		m.options.alt = t.title;
		m.options.zIndexOffset = t.zOffset;
		if (!m.getLatLng().equals(t.latlng)) m.setLatLng(t.latlng);
		m.setIcon(divIcon(t.html, t.iconSize, t.pin));
		// A DivIcon reuses its element (setIcon rewrites its HTML and class
		// list, and _initIcon/_initInteraction put Leaflet's classes back), but
		// Leaflet only sets the title on a new element: set it here.
		const now = m.getElement();
		if (now) now.title = t.title;
		if (hadFocus && now !== el) now?.focus({ preventScroll: true });
		e.sig = t.sig;
	}

	function create(t: Target): void {
		const marker = L.marker(t.latlng, {
			icon: divIcon(t.html, t.iconSize, t.pin),
			title: t.title,
			alt: t.title,
			riseOnHover: true,
			riseOffset: SELECTED_Z,
			zIndexOffset: t.zOffset
		});
		// A key is a fixed set of members, so what activating it does never
		// changes for the marker's lifetime.
		const id = t.id;
		const activate = id
			? () => onselect(id)
			: () => map.setZoomAround(marker.getLatLng(), map.getZoom() + CLUSTER_ZOOM_STEP);
		marker.on('click', activate);
		// Leaflet gives markers role="button" and a tabindex but only
		// handles Enter for popups; activate on Enter here too.
		marker.on('keypress', (ev) => {
			if ((ev as L.LeafletKeyboardEvent).originalEvent.key === 'Enter') activate();
		});
		marker.addTo(map);
		entries.set(t.key, { marker, sig: t.sig });
	}

	function divIcon(html: string, size: number, pin: boolean): L.DivIcon {
		return L.divIcon({
			html,
			className: pin ? 'fs-pin' : 'fs-cluster-icon',
			iconSize: [size, size],
			iconAnchor: [size / 2, size / 2]
		});
	}

	// Portal lines: one polyline per class, drawn bottom to top in this order
	// (the selected pair's line above the rest).
	const LINE_CLASSES = ['pl', 'pl pl-dim', 'pl pl-sel'] as const;
	type LineClass = (typeof LINE_CLASSES)[number];
	const lines = new Map<LineClass, { line: L.Polyline; sig: string }>();
	let linesKey: { pairs: [MapMarker, MapMarker][]; selectedId: string | undefined; on: boolean } | undefined;

	function renderLines(allPairs: [MapMarker, MapMarker][]): void {
		const on = layers.portals && portalLinks;
		if (linesKey && linesKey.pairs === allPairs && linesKey.selectedId === selectedId && linesKey.on === on) return;
		linesKey = { pairs: allPairs, selectedId, on };
		const cls = (a: MapMarker, b: MapMarker): LineClass =>
			selectedId === a.id || selectedId === b.id ? 'pl pl-sel' : selectedId ? 'pl pl-dim' : 'pl';
		const groups = new Map<LineClass, [MapMarker, MapMarker][]>();
		if (on) {
			for (const p of allPairs) {
				const c = cls(p[0], p[1]);
				const g = groups.get(c);
				if (g) g.push(p);
				else groups.set(c, [p]);
			}
		}
		let added = false;
		for (const c of LINE_CLASSES) {
			const g = groups.get(c);
			const cur = lines.get(c);
			if (!g) {
				cur?.line.remove();
				lines.delete(c);
				continue;
			}
			const sig = g.map(([a, b]) => `${a.id}:${a.x},${a.z}-${b.id}:${b.x},${b.z}`).join(';');
			if (cur?.sig === sig) continue;
			const latlngs = g.map(([a, b]) => [toLatLng(a.x, a.z), toLatLng(b.x, b.z)]);
			if (cur) {
				cur.line.setLatLngs(latlngs);
				cur.sig = sig;
				continue;
			}
			const line = L.polyline(latlngs, {
				pane: 'portalLines',
				className: c,
				interactive: false,
				color: '#9cc4e0'
			}).addTo(map);
			lines.set(c, { line, sig });
			added = true;
		}
		// A new path is appended last in the SVG: restore the class order.
		if (added) for (const c of LINE_CLASSES) lines.get(c)?.line.bringToFront();
	}

	// Two frames after a map event: the first is the browser's own restyle
	// of everything Leaflet just moved (tiles, every marker on a zoom), so
	// the render's work lands in the next frame instead of piling onto it.
	function schedule(): void {
		if (frame) return;
		frame = requestAnimationFrame(() => {
			frame = requestAnimationFrame(render);
		});
	}

	// Map events: re-cluster after every zoom and pan.
	$effect(() => {
		const m = map;
		m.on('zoomend moveend resize viewreset', schedule);
		return () => {
			m.off('zoomend moveend resize viewreset', schedule);
		};
	});

	// Props: re-render when any input changes.
	$effect(() => {
		void all;
		void mask;
		void fog;
		void selectedId;
		void clustering;
		void portalLinks;
		for (const k in layers) void layers[k as LayerKey];
		untrack(render);
	});

	// Teardown.
	$effect(() => {
		return () => {
			if (frame) cancelAnimationFrame(frame);
			frame = 0;
			slicer.cancel();
			warmHandle?.cancel();
			warmHandle = undefined;
			for (const e of entries.values()) e.marker.remove();
			entries.clear();
			for (const l of lines.values()) l.line.remove();
			lines.clear();
			linesKey = undefined;
		};
	});
</script>
