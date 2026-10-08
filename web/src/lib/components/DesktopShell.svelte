<!--
  Desktop shell (DESIGN-NOTES §1.1): the full-viewport map with markers and
  the marker popover (§5.9), the side panel or its collapsed pill (§3.2–
  §3.10, §3.4), the map-updated pill (§3.11) or a state overlay (§3.22:
  waiting pill, charting card, offline/stale/can't-draw banner; while
  Farsight can't be reached the "Reconnecting…" banner takes the pill's or
  banner's slot), the time-and-weather pill under it (Plan 9; hidden while
  waiting/charting), the top-right search and layers cluster (§3.12, §3.13), the scale readout and
  zoom controls, and the join dialog (§3.19). The toast lives in the layout.

  Owns the UI flags: panelOpen, tab, selectedId, joinOpen, layersOpen,
  weatherOpen, layers and portalLinks. `mapView` (derive.ts) decides the
  state overlay, the tile filter (with the world clock's night/evening
  tint) and the pin-opacity class on the marker pane. While the world
  clock runs, a 1 s ticker drives the time pill and the tint. The map stays mounted across server switches (AtlasMap swaps
  its tiles by key); a switch clears the selection and resets the view. The
  panel floats over the map, so map container points are page points and
  `padLeft` (376 open, 0 collapsed) keeps programmatic centring in the area
  right of the panel.
-->
<script lang="ts">
	import type L from 'leaflet';
	import { onDestroy, tick, untrack } from 'svelte';
	import { toLatLng } from '$lib/geo';
	import { mapView } from '$lib/derive';
	import { timeView, tintOf } from '$lib/worldtime';
	import {
		buildMarkers,
		defaultLayers,
		enabledLayerCount,
		layerCounts,
		markerAt,
		markersKey,
		visibleMarkers,
		type LayerKey,
		type MapMarker
	} from '$lib/markers';
	import { app } from '$lib/state.svelte';
	import type { Marker } from '$lib/types';
	import ActivityPanel from './ActivityPanel.svelte';
	import AtlasMap from './AtlasMap.svelte';
	import ChartingCard from './ChartingCard.svelte';
	import CollapsedPill from './CollapsedPill.svelte';
	import ConnectionBanner from './ConnectionBanner.svelte';
	import JoinDialog from './JoinDialog.svelte';
	import LayersButton from './LayersButton.svelte';
	import LayersPanel from './LayersPanel.svelte';
	import MapUpdatedPill from './MapUpdatedPill.svelte';
	import MarkerCard from './MarkerCard.svelte';
	import MarkerLayer from './MarkerLayer.svelte';
	import ProfilePanel from './ProfilePanel.svelte';
	import ScaleReadout from './ScaleReadout.svelte';
	import SearchBox from './SearchBox.svelte';
	import SidePanel from './SidePanel.svelte';
	import StateBanner from './StateBanner.svelte';
	import WaitingPill from './WaitingPill.svelte';
	import WeatherPill from './WeatherPill.svelte';
	import ZoomControls from './ZoomControls.svelte';

	const PANEL_W = 376; // 16 + 344 + 16
	const ACTIVITY_W = 552; // 16 + 520 + 16
	const POPOVER_W = 312;
	const CULL = 40;

	// UI flags.
	let panelOpen = $state(true);
	let tab = $state<'players' | 'world'>('players');
	let selectedId = $state<string>();
	let joinOpen = $state(false);
	let layersOpen = $state(false);
	let weatherOpen = $state(false);
	let layers = $state(defaultLayers());
	let portalLinks = $state(true);

	let searchBox = $state<ReturnType<typeof SearchBox>>();
	let layersButton = $state<ReturnType<typeof LayersButton>>();
	/** Widths for the banner slot: the viewport and the top-right cluster (§1.1 item 6). */
	let shellW = $state(0);
	let clusterW = $state(0);
	let pillW = $state(0);
	/** The map-updated pill's or banner's height, to put the time pill under it. */
	let slotH = $state(0);

	let atlas = $state<ReturnType<typeof AtlasMap>>();
	let map = $state.raw<L.Map>();
	/** Kind and position of the selection, to drop it when a new snapshot reuses the id (§5.4). */
	let selectedKey: string | undefined;
	let zoom = $state(0);
	let popover = $state<{ left: number; top: number }>();
	let frame = 0;

	const card = $derived(app.card?.id === app.currentId ? app.card : undefined);
	/** The decoded 12 m explored mask, for the cursor readout's "Unexplored". */
	const mask = $derived(app.snapshot?.mask);
	/** The biome grid for the current tile set, once loaded (Task 4); stale once `card.tiles.key` has moved on. */
	const grid = $derived.by(() => {
		const b = app.biomes;
		return b && b.key === card?.tiles.key ? b.grid : undefined;
	});
	/** A profile or the Activity view (Plan 7) takes the panel's place, open or collapsed. */
	const profilePlayer = $derived(app.view?.kind === 'profile' ? app.view.player : undefined);
	const activityOpen = $derived(app.view?.kind === 'activity');
	const padLeft = $derived(activityOpen ? ACTIVITY_W : panelOpen || profilePlayer !== undefined ? PANEL_W : 0);

	// The state treatment (§3.22): overlay, tile filter and pin opacity.
	// `app.tileSamples` is replaced together with `app.card`, so reading it
	// here stays current.
	// The world clock (Plan 9): ticks every second while it runs (app.now
	// only ticks every 30 s); paused, netTimeNow ignores the time. It stops
	// while Farsight can't be reached: whether the world clock still runs
	// isn't known then.
	let clockNow = $state(new Date());
	const clockRunning = $derived(!!card?.clock?.running && !app.disconnected);
	$effect(() => {
		if (!clockRunning) return;
		clockNow = new Date();
		const id = setInterval(() => (clockNow = new Date()), 1000);
		return () => clearInterval(id);
	});
	const time = $derived(card ? timeView(card, app.cardAt ?? clockNow, clockNow) : undefined);
	/** A primitive, so mapView only re-runs when the phase's tint changes. */
	const tint = $derived(time ? tintOf(time.phase) : undefined);
	const view = $derived(card ? mapView(card, app.now, app.tileSamples, layers.biomes, tint) : undefined);
	const pillL = $derived(padLeft > 0 ? padLeft : 190);

	// Markers are hidden while waiting for a save and while charting.
	const markersOn = $derived(!!view?.markersOn);
	// The marker model is memoised on (server, save, defeated bosses): a card
	// poll (a new object every 15 s) returns the same array, so search
	// results, layer counts and MarkerLayer don't rework.
	let built: { key: string; all: MapMarker[] } = { key: '', all: [] };
	const all = $derived.by<MapMarker[]>(() => {
		const snap = app.snapshot;
		if (!markersOn || !snap || !card) return [];
		const key = markersKey(card.id, snap, card.world);
		if (built.key !== key) built = { key, all: buildMarkers(snap, card.world) };
		return built.all;
	});
	const counts = $derived(layerCounts(all));

	// The search box is 340 px (§3.12), narrowed on small desktops with the
	// panel open so the cluster clears it (376 + 16 + layers ≈ 137 + gaps).
	const searchW = $derived(panelOpen ? Math.max(180, Math.min(340, shellW - 555)) : 340);

	// The pill and the banners share one slot (§1.1 item 4) and must clear the
	// search/layers cluster; when the gap is too narrow they drop below it.
	const BANNER_MIN_W = 360;
	const BELOW_CLUSTER = 16 + 46 + 10;
	const clearRight = $derived(16 + clusterW + 12);
	const gap = $derived(shellW - pillL - clearRight);
	const bannerBox = $derived(
		gap >= BANNER_MIN_W ? { right: clearRight, top: 16 } : { right: 16, top: BELOW_CLUSTER }
	);
	const pillTop = $derived(gap >= Math.max(pillW, 360) ? 16 : BELOW_CLUSTER);
	/** The time pill: 12 px under the map-updated pill or banner (design: 106 under a 16 + 78 px pill). */
	const slotTop = $derived(app.disconnected || view?.overlay.kind === 'banner' ? bannerBox.top : pillTop);
	const weatherTop = $derived(slotH > 0 ? slotTop + slotH + 12 : 106);
	/** Hidden, like the map-updated pill, while waiting for a save or charting. */
	const weatherShown = $derived(!!card?.clock && (view?.overlay.kind === 'banner' || view?.overlay.kind === 'pill'));
	$effect(() => {
		if (!weatherShown) weatherOpen = false;
	});

	// Pin opacity for offline / stale lives on the marker pane (app.css).
	$effect(() => {
		const pane = map?.getPane('markerPane');
		if (!pane) return;
		const cls = view?.pinClass ?? '';
		pane.classList.remove('fs-pins-offline', 'fs-pins-stale');
		if (cls) pane.classList.add(cls);
	});
	const selected = $derived(selectedId === undefined ? undefined : all.find((m) => m.id === selectedId));
	const selectedShown = $derived(
		!!selected && visibleMarkers([selected], layers, zoom).length === 1
	);

	const keyOf = (m: MapMarker) => `${m.type}@${m.x},${m.z}`;

	/** Closes the layers panel; focus inside it goes back to the Layers button. */
	function closeLayers(): void {
		if (!layersOpen) return;
		const inside = !!document.activeElement?.closest('#layers-panel');
		layersOpen = false;
		if (inside) layersButton?.focus();
	}

	/** Closes the time-and-weather dropdown; focus inside it goes back to the pill. */
	function closeWeather(): void {
		if (!weatherOpen) return;
		const pill = document.activeElement?.closest('[data-testid="weather-pill"]');
		weatherOpen = false;
		if (pill) pill.querySelector<HTMLElement>('button')?.focus();
	}

	/** Closes the search results, the layers panel and the time-and-weather dropdown (§5.3). */
	function closeMenus(): void {
		closeLayers();
		closeWeather();
		searchBox?.close();
	}

	function select(id: string | undefined): void {
		if (id !== undefined) closeMenus();
		selectedId = id;
		const m = id === undefined ? undefined : all.find((x) => x.id === id);
		selectedKey = m ? keyOf(m) : undefined;
	}

	// A refreshed snapshot: keep the selection only if the id still names the
	// same kind at the same position.
	$effect(() => {
		if (selectedId === undefined) return;
		if (!selected || keyOf(selected) !== selectedKey) select(undefined);
	});

	// A server switch: drop the selection, close the menus, clear the
	// typed search and return to the default view.
	let lastServer: string | undefined;
	$effect(() => {
		const id = app.currentId;
		if (lastServer !== undefined && id !== lastServer) {
			untrack(() => {
				closeMenus();
				searchBox?.clear();
			});
			select(undefined);
			joinOpen = false;
			atlas?.resetView();
		}
		lastServer = id;
	});

	/**
	 * Desktop focus (fix round 1; extended for the Activity view, Plan 7):
	 * opening a profile or the timeline unmounts the SidePanel (and
	 * whatever row had focus — a "Profile →" row, or the "Full timeline →"
	 * link) in favour of ProfilePanel/ActivityPanel, which focus their own
	 * Back button on mount. Closing it remounts the SidePanel, but as a
	 * fresh instance — a captured element reference would just be
	 * disconnected — so once that settles (tick(), since the panel's own
	 * effects and PlayersTab's fetch-free render still need a beat) this
	 * looks up the row that opened it (by name for a profile, falling back
	 * to the Online tab when that row is gone — e.g. the player left and
	 * dropped off "Recently online" by the time the profile closed — or the
	 * "Full timeline →" link for the activity view) and focuses it.
	 */
	let closingFocusKind: 'profile' | 'activity' | undefined;
	let closingFocusName: string | undefined;
	$effect(() => {
		if (profilePlayer !== undefined || activityOpen) {
			untrack(() => {
				closingFocusKind = activityOpen ? 'activity' : 'profile';
				closingFocusName = app.viewOpenerName;
			});
			return;
		}
		if (closingFocusKind === undefined) return;
		const kind = closingFocusKind;
		const name = closingFocusName;
		closingFocusKind = undefined;
		closingFocusName = undefined;
		untrack(() => {
			void tick().then(() => {
				if (kind === 'activity') {
					(document.querySelector<HTMLElement>('.panel .full') ?? document.getElementById('tab-players'))?.focus();
					return;
				}
				const rows = document.querySelectorAll<HTMLButtonElement>('.panel .profile');
				const row = Array.from(rows).find((b) => b.getAttribute('aria-label') === `Profile of ${name}`);
				(row ?? document.getElementById('tab-players'))?.focus();
			});
		});
	});

	/** §5.9: right of the pin (or left when it would overflow), clamped vertically. */
	function placePopover(): void {
		frame = 0;
		if (!map) return;
		zoom = map.getZoom();
		if (!selected) {
			popover = undefined;
			return;
		}
		const p = map.latLngToContainerPoint(toLatLng(selected.x, selected.z));
		const size = map.getSize();
		// Hidden while the pin is culled (outside the viewport ±40 px, like
		// MarkerLayer); shown again when it comes back into view.
		if (p.x < -CULL || p.y < -CULL || p.x > size.x + CULL || p.y > size.y + CULL) {
			popover = undefined;
			return;
		}
		let left = p.x + 26;
		if (left + POPOVER_W > size.x - 16) left = p.x - 26 - POPOVER_W;
		// Never under the open side panel.
		left = Math.max(left, padLeft + 16);
		const top = Math.max(96, Math.min(size.y - 440, p.y - 60));
		popover = { left, top };
	}

	function schedulePopover(): void {
		if (!frame) frame = requestAnimationFrame(placePopover);
	}

	$effect(() => {
		void selected;
		void map;
		schedulePopover();
	});

	// Fix round 1, item 3: with zoom animation on, Leaflet fires 'move'/'zoom'
	// (which reposition the popover above) only once, at the END of the
	// ~250 ms transition, so the popover would otherwise sit still while its
	// pin animates away underneath it, then snap. Of the review's two
	// options — reproject it every animation frame via Leaflet's internal
	// zoom-anim machinery, or hide it for the animation's duration — this
	// takes the simpler, still-correct one: hide at zoomstart, let the
	// existing move/zoom listener recompute and show it at zoomend.
	$effect(() => {
		const m = map;
		if (!m) return;
		const hide = () => {
			if (popover) popover = undefined;
		};
		m.on('zoomstart', hide);
		m.on('zoomend', schedulePopover);
		return () => {
			m.off('zoomstart', hide);
			m.off('zoomend', schedulePopover);
		};
	});

	onDestroy(() => {
		if (frame) cancelAnimationFrame(frame);
	});

	function jump(id: string): void {
		const partner = all.find((m) => m.id === id);
		if (!partner || !map) return;
		atlas?.centerOn(partner.x, partner.z, Math.max(3.5, map.getZoom()));
		select(id);
	}

	/** World tab "Show altar": centre on it at zoom 3.5 and select it. */
	function showAltar(altar: Marker): void {
		atlas?.centerOn(altar.x, altar.z, 3.5);
		select(altar.id);
	}

	/** A profile's "Map →": centre on the item at zoom 4, selecting its marker when it's on the map. */
	function mapTo(x: number, z: number, id: string): void {
		atlas?.centerOn(x, z, 4);
		if (all.some((m) => m.id === id)) select(id);
	}

	/** The timeline's "Show on map →": centre at zoom 4, selecting the marker there if any. */
	function showOnMap(x: number, z: number): void {
		atlas?.centerOn(x, z, 4);
		const m = markerAt(all, x, z);
		if (m) select(m.id);
	}

	/** Search pick (§5.2): centre on the marker at zoom 4.25 and select it. */
	function pickResult(m: MapMarker): void {
		atlas?.centerOn(m.x, m.z, 4.25);
		select(m.id);
	}

	function toggleLayer(key: LayerKey): void {
		layers[key] = !layers[key];
	}

	function openJoin(): void {
		closeMenus();
		joinOpen = true;
	}

	function toast(title: string, sub: string): void {
		app.showToast(title, sub === '' ? undefined : sub);
	}

	function onkeydown(e: KeyboardEvent): void {
		if (e.key !== 'Escape' || e.defaultPrevented || joinOpen || app.unlockPrompt) return;
		if (layersOpen) {
			closeLayers();
			return;
		}
		if (weatherOpen) {
			closeWeather();
			return;
		}
		if (selectedId !== undefined) {
			select(undefined);
			return;
		}
		if (app.view) app.closeView();
	}
</script>

<svelte:window {onkeydown} />

<main class="shell" bind:clientWidth={shellW}>
	{#if profilePlayer !== undefined && app.currentId}
		<ProfilePanel serverId={app.currentId} player={profilePlayer} onback={() => app.closeView()} onmap={mapTo} />
	{:else if activityOpen && app.currentId}
		<ActivityPanel serverId={app.currentId} gameDay={card?.world?.day} onback={() => app.closeView()} onmap={showOnMap} />
	{:else if panelOpen}
		<SidePanel bind:tab oncollapse={() => (panelOpen = false)} onjoin={openJoin} onshowaltar={showAltar} />
	{:else}
		<CollapsedPill onopen={() => (panelOpen = true)} />
	{/if}

	{#if view?.overlay.kind === 'waiting'}
		<WaitingPill {padLeft} />
	{:else if view?.overlay.kind === 'charting'}
		{@const o = view.overlay}
		<ChartingCard pct={o.pct} done={o.done} total={o.total} etaMin={o.etaMin} {padLeft} />
	{/if}
	{#if app.disconnected}
		<ConnectionBanner left={pillL} right={bannerBox.right} top={bannerBox.top} bind:height={slotH} />
	{:else if view?.overlay.kind === 'banner'}
		{@const o = view.overlay}
		<StateBanner
			tone={o.tone}
			title={o.title}
			body={o.body}
			left={pillL}
			right={bannerBox.right}
			top={bannerBox.top}
			bind:height={slotH}
		/>
	{:else if view?.overlay.kind === 'pill' && card?.world}
		<MapUpdatedPill world={card.world} timeZone={card.timeZone} now={app.now} left={pillL} top={pillTop} bind:width={pillW} bind:height={slotH} />
	{/if}
	{#if time && weatherShown}
		<WeatherPill
			view={time}
			left={pillL}
			top={weatherTop}
			bind:open={weatherOpen}
			onopen={() => {
				closeLayers();
				searchBox?.close();
			}}
		/>
	{/if}

	<div class="cluster" bind:clientWidth={clusterW}>
		<SearchBox
			bind:this={searchBox}
			{all}
			width={searchW}
			disabled={!markersOn}
			disabledPlaceholder={view?.overlay.kind === 'charting' ? 'Charting the map…' : 'Waiting for the first save…'}
			onpick={pickResult}
			onopen={() => {
				layersOpen = false;
				weatherOpen = false;
			}}
		/>
		<div class="layers">
			<LayersButton
				bind:this={layersButton}
				count={enabledLayerCount(layers)}
				open={layersOpen}
				controls="layers-panel"
				onclick={() => {
					if (layersOpen) return closeLayers();
					weatherOpen = false;
					layersOpen = true;
				}}
			/>
			{#if layersOpen}
				<LayersPanel
					id="layers-panel"
					variant="desktop"
					{layers}
					{portalLinks}
					{counts}
					ontoggle={toggleLayer}
					onlinks={() => (portalLinks = !portalLinks)}
				/>
			{/if}
		</div>
	</div>

	{#if selected && selectedShown && popover}
		<div class="popover" style:left="{popover.left}px" style:top="{popover.top}px">
			<MarkerCard m={selected.card} onclose={() => select(undefined)} onjump={jump} />
		</div>
	{/if}

	<ScaleReadout {map} {mask} {grid} left={padLeft > 0 ? padLeft : 16} />
	<ZoomControls
		onzoomin={() => atlas?.zoomBy(0.75)}
		onzoomout={() => atlas?.zoomBy(-0.75)}
		onreset={() => {
			atlas?.resetView();
			select(undefined);
		}}
	/>

	<!-- Last in the DOM, so Tab reaches the panel and the controls before
	     the map and its pins (each one a tab stop); AtlasMap sits under
	     them all with z-index -1. -->
	<AtlasMap
		bind:this={atlas}
		{card}
		snapshot={app.snapshot}
		{padLeft}
		dim={1}
		filter={view?.filter ?? ''}
		onready={(m) => (map = m)}
		onclick={() => {
			closeMenus();
			select(undefined);
		}}
		onmove={schedulePopover}
	/>
	{#if map}
		<MarkerLayer {map} {all} {layers} {portalLinks} {selectedId} onselect={select} />
	{/if}
</main>

{#if joinOpen && card}
	<JoinDialog {card} onclose={() => (joinOpen = false)} oncopy={toast} />
{/if}

<style>
	.shell {
		position: fixed;
		inset: 0;
		overflow: hidden;
	}
	.cluster {
		position: absolute;
		top: 16px;
		right: 16px;
		display: flex;
		gap: 10px;
		align-items: flex-start;
		z-index: 600;
	}
	.layers {
		position: relative;
	}
	.popover {
		position: absolute;
		width: 312px;
		z-index: 500;
	}
</style>
