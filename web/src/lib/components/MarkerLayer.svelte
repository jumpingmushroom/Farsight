<!--
  Map markers (DESIGN-NOTES §4.1, §5.1, §5.4). Renders nothing itself: it
  keeps Leaflet layers on `map` in sync with the visible markers.

  - On zoomend / moveend (throttled to one rAF) and on prop changes it
    projects the visible markers to container points, culls to the viewport
    ±40 px, and clusters them (radius 26, the selected marker locked; not
    at max zoom, where a cluster could no longer be split).
  - One L.marker per group with an L.divIcon, diffed by group key (the sorted
    ids), so unchanged pins keep their DOM: a pin is only moved, and its icon
    only replaced, when its HTML changes.
  - Portal lines are L.polylines in the `portalLines` pane, styled by class
    (.pl, .pl-sel, .pl-dim in app.css).
-->
<script lang="ts">
	import L from 'leaflet';
	import { untrack } from 'svelte';
	import { cluster } from '$lib/cluster';
	import { toLatLng } from '$lib/geo';
	import {
		clusterHtml,
		clusterSize,
		clusterTooltip,
		pinHtml,
		portalPairs,
		visibleMarkers,
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
	// selected pin's offset, and every combination stays well inside int32.
	const ORDER_Z = 20_000;
	const MAX_ORDER = 4_000; // 8e7 < SELECTED_Z
	const SELECTED_Z = 100_000_000;

	interface Entry {
		marker: L.Marker;
		/** The icon HTML plus the tooltip: a change replaces the icon element. */
		sig: string;
	}

	const entries = new Map<string, Entry>();
	let lines: L.Polyline[] = [];
	let linesSig = '';
	let frame = 0;

	function render(): void {
		frame = 0;
		const zoom = map.getZoom();
		const visible = visibleMarkers(all, layers, mask, fog, zoom);
		const byId = new Map(visible.map((m) => [m.id, m]));
		const size = map.getSize();
		const pts: { id: string; x: number; y: number }[] = [];
		for (const m of visible) {
			const p = map.latLngToContainerPoint(toLatLng(m.x, m.z));
			if (p.x < -CULL || p.y < -CULL || p.x > size.x + CULL || p.y > size.y + CULL) continue;
			pts.push({ id: m.id, x: p.x, y: p.y });
		}
		// At max zoom a cluster click can't zoom further, so markers closer
		// than the radius there (a portal inside a base) would be unreachable:
		// show them unclustered instead.
		const groups = clustering && zoom < map.getMaxZoom()
			? cluster(pts, RADIUS, selectedId)
			: pts.map((p) => ({ ids: [p.id], x: p.x, y: p.y }));

		const keep = new Set<string>();
		for (const [order, g] of groups.entries()) {
			const members = g.ids.map((id) => byId.get(id)!);
			const key = [...g.ids].sort().join('|');
			keep.add(key);
			let html: string;
			let latlng: L.LatLng;
			let title: string;
			let iconSize: number;
			let single: MapMarker | undefined;
			if (members.length === 1) {
				single = members[0];
				const selected = single.id === selectedId;
				html = pinHtml(single, { selected, showLabel: zoom >= LABEL_ZOOM });
				latlng = toLatLng(single.x, single.z);
				title = single.tooltip;
				iconSize = single.pin.size;
			} else {
				html = clusterHtml(members.length);
				latlng = map.containerPointToLatLng([g.x, g.y]);
				title = clusterTooltip(members);
				iconSize = clusterSize(members.length);
			}
			// Stack in marker order like the design (later kinds on top, so a
			// portal inside a base stays visible), not Leaflet's by-latitude
			// order; the selected pin above everything.
			const zOffset = single !== undefined && single.id === selectedId ? SELECTED_Z : Math.min(order, MAX_ORDER) * ORDER_Z;
			const sig = `${html}\n${title}`;
			const existing = entries.get(key);
			if (existing) {
				if (!existing.marker.getLatLng().equals(latlng)) existing.marker.setLatLng(latlng);
				if (existing.sig !== sig) {
					existing.marker.options.title = title;
					existing.marker.options.alt = title;
					// setIcon replaces the element: keep keyboard focus on it.
					const hadFocus = document.activeElement === existing.marker.getElement();
					existing.marker.setIcon(divIcon(html, iconSize, single !== undefined));
					if (hadFocus) existing.marker.getElement()?.focus({ preventScroll: true });
					existing.sig = sig;
				}
				existing.marker.setZIndexOffset(zOffset);
				continue;
			}
			const marker = L.marker(latlng, {
				icon: divIcon(html, iconSize, single !== undefined),
				title,
				alt: title,
				riseOnHover: true,
				riseOffset: SELECTED_Z,
				zIndexOffset: zOffset
			});
			const id = single?.id;
			const activate = id
				? () => onselect(id)
				: () => map.setZoomAround(marker.getLatLng(), map.getZoom() + CLUSTER_ZOOM_STEP);
			marker.on('click', activate);
			// Leaflet gives markers role="button" and a tabindex but only
			// handles Enter for popups; activate on Enter here too.
			marker.on('keypress', (e) => {
				if ((e as L.LeafletKeyboardEvent).originalEvent.key === 'Enter') activate();
			});
			marker.addTo(map);
			entries.set(key, { marker, sig });
		}
		for (const [key, e] of entries) {
			if (keep.has(key)) continue;
			e.marker.remove();
			entries.delete(key);
		}

		renderLines(visible);
	}

	function divIcon(html: string, size: number, pin: boolean): L.DivIcon {
		return L.divIcon({
			html,
			className: pin ? 'fs-pin' : 'fs-cluster-icon',
			iconSize: [size, size],
			iconAnchor: [size / 2, size / 2]
		});
	}

	function renderLines(visible: MapMarker[]): void {
		const pairs = layers.portals && portalLinks ? portalPairs(visible) : [];
		const cls = (a: MapMarker, b: MapMarker) =>
			selectedId === a.id || selectedId === b.id ? 'pl pl-sel' : selectedId ? 'pl pl-dim' : 'pl';
		const sig = pairs.map(([a, b]) => `${a.id}:${a.x},${a.z}-${b.id}:${b.x},${b.z}:${cls(a, b)}`).join(';');
		if (sig === linesSig) return;
		linesSig = sig;
		for (const l of lines) l.remove();
		lines = pairs.map(([a, b]) =>
			L.polyline([toLatLng(a.x, a.z), toLatLng(b.x, b.z)], {
				pane: 'portalLines',
				className: cls(a, b),
				interactive: false,
				color: '#9cc4e0'
			}).addTo(map)
		);
	}

	function schedule(): void {
		if (frame) return;
		frame = requestAnimationFrame(render);
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
			for (const e of entries.values()) e.marker.remove();
			entries.clear();
			for (const l of lines) l.remove();
			lines = [];
			linesSig = '';
		};
	});
</script>
