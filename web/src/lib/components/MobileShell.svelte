<!--
  Mobile shell (< 768 px; DESIGN-NOTES §1.2, §1.6, §1.7, §3.17, §3.18,
  §3.22 and the Mobile ruling): the full-screen map with markers, the top
  bar, the players sheet (peek ↔ pulled), and one overlay sheet at a time
  (menu, join or server) over it. The toast lives in the layout.

  - Dimming (AtlasMap `dim`): .7 pulled, .55 behind the menu, join and
    server sheets (mobile.ts `mobileDim`). A transparent scrim over the
    dimmed map closes the overlay sheet, or collapses the pulled sheet.
  - Selecting a marker (a tap, a search pick, "Jump to partner") centres it
    at y ≈ 300 px with AtlasMap.centerOn's vertical offset (`centerDy`) and
    docks MobileMarkerCard at the bottom. While the card is docked the peek
    sheet and zoom buttons slide away (they would sit under the card; design
    frame 4 shows neither). A map tap or the card's close button closes it.
  - Esc closes the overlay sheet, then the card, then collapses pulled.
  - States (§3.22): offline/stale/can't-draw banners sit compact under the
    top bar; charting shows ChartingCard centred; no save shows the waiting
    pill. The top-bar sub-line carries the short state text. While
    Farsight can't be reached the "Reconnecting…" banner takes the state
    banner's place (over the waiting pill or charting card too).
  - Time and weather (Plan 9): the compact chip sits under the top bar
    (under the banner when there is one; not while waiting or charting);
    a tap opens WeatherSheet. While the world clock runs, a 1 s ticker
    drives the chip, the sheet and the night/evening tile tint.
  - Zoom buttons: the mobile variant 47 px above the measured peek sheet,
    hidden while waiting/charting (nothing to zoom; they'd cover the card).
    No scale readout and no desktop pills on mobile.
  - A server switch clears the selection, closes the sheets and resets the
    view (the map stays mounted, like the desktop shell).
-->
<script lang="ts">
	import type L from 'leaflet';
	import { tick, untrack } from 'svelte';
	import { mapView } from '$lib/derive';
	import {
		buildMarkers,
		defaultLayers,
		layerCounts,
		markerAt,
		markersKey,
		visibleMarkers,
		type LayerKey,
		type MapMarker
	} from '$lib/markers';
	import { cardPadBottom, centerDy, mobileDim, topBarSub, zoomBottom, type MobileOverlay, type Snap } from '$lib/mobile';
	import { app } from '$lib/state.svelte';
	import { timeView, tintOf } from '$lib/worldtime';
	import ActivitySheet from './ActivitySheet.svelte';
	import AtlasMap from './AtlasMap.svelte';
	import ChartingCard from './ChartingCard.svelte';
	import ConnectionBanner from './ConnectionBanner.svelte';
	import JoinSheet from './JoinSheet.svelte';
	import MarkerLayer from './MarkerLayer.svelte';
	import MenuSheet from './MenuSheet.svelte';
	import MobileMarkerCard from './MobileMarkerCard.svelte';
	import MobileTopBar from './MobileTopBar.svelte';
	import PeekSheet from './PeekSheet.svelte';
	import ProfileSheet from './ProfileSheet.svelte';
	import ServerSheet from './ServerSheet.svelte';
	import StateBanner from './StateBanner.svelte';
	import WaitingPill from './WaitingPill.svelte';
	import WeatherPill from './WeatherPill.svelte';
	import WeatherSheet from './WeatherSheet.svelte';
	import ZoomControls from './ZoomControls.svelte';

	const DEFAULT_ZOOM = 1.5;
	const SEARCH_ZOOM = 4.25;

	// UI flags.
	let snap = $state<Snap>('peek');
	let overlay = $state<MobileOverlay>('none');
	let selectedId = $state<string>();
	let layers = $state(defaultLayers());
	let portalLinks = $state(true);
	/** The measured peek-sheet height (§1.2: ≈161 px). */
	let peekH = $state(161);

	let atlas = $state<ReturnType<typeof AtlasMap>>();
	let map = $state.raw<L.Map>();
	let zoom = $state(DEFAULT_ZOOM);
	/** Kind and position of the selection, to drop it when a new snapshot reuses the id (§5.4). */
	let selectedKey: string | undefined;
	/** Focus to restore when an overlay sheet closes. */
	let opener: HTMLElement | null = null;
	/**
	 * Adaptation (Plan 7, brief predates it; fix round 1: via focusTrap's
	 * own `returnTo` rather than a shell-level effect): the Activity
	 * view's only mobile entry point is "Full timeline" inside the menu
	 * sheet, which unmounts as the sheet closes to open ActivitySheet —
	 * unlike a profile, always opened from a row that stays mounted
	 * underneath, there is nothing left for ActivitySheet's own focusTrap
	 * to capture as its opener. This carries the menu's own opener (the
	 * top bar's menu button, which does stay mounted) across that gap;
	 * ActivitySheet passes it straight through to focusTrap as `returnTo`.
	 */
	let activityOpener: HTMLElement | null = null;

	const card = $derived(app.card?.id === app.currentId ? app.card : undefined);
	const summary = $derived(app.servers.find((s) => s.id === app.currentId));
	// The world clock (Plan 9), as in DesktopShell: a 1 s ticker while it
	// runs and Farsight can be reached.
	let clockNow = $state(new Date());
	const clockRunning = $derived(!!card?.clock?.running && !app.disconnected);
	$effect(() => {
		if (!clockRunning) return;
		clockNow = new Date();
		const id = setInterval(() => (clockNow = new Date()), 1000);
		return () => clearInterval(id);
	});
	const time = $derived(card ? timeView(card, app.cardAt ?? clockNow, clockNow) : undefined);
	const tint = $derived(time ? tintOf(time.phase) : undefined);
	const view = $derived(card ? mapView(card, app.now, app.tileSamples, layers.biomes, tint) : undefined);
	/** The chip: not while waiting or charting (like the desktop pill). */
	const chipShown = $derived(!!card?.clock && (view?.overlay.kind === 'banner' || view?.overlay.kind === 'pill'));
	const markersOn = $derived(!!view?.markersOn);

	// Memoised on (server, save, defeated bosses), as in DesktopShell.
	let built: { key: string; all: MapMarker[] } = { key: '', all: [] };
	const all = $derived.by<MapMarker[]>(() => {
		const snapView = app.snapshot;
		if (!markersOn || !snapView || !card) return [];
		const key = markersKey(card.id, snapView, card.world);
		if (built.key !== key) built = { key, all: buildMarkers(snapView, card.world) };
		return built.all;
	});
	const counts = $derived(layerCounts(all));

	const selected = $derived(selectedId === undefined ? undefined : all.find((m) => m.id === selectedId));
	const cardShown = $derived(!!selected && visibleMarkers([selected], layers, zoom).length === 1);

	const sub = $derived(topBarSub(view, card?.world, app.now, overlay === 'server'));
	/** A profile (Plan 7) opens as a full-height sheet over everything. */
	const profilePlayer = $derived(app.view?.kind === 'profile' ? app.view.player : undefined);
	const dim = $derived(app.view ? 0.55 : mobileDim(snap, overlay));
	// Hidden while there is nothing to zoom (waiting, charting) and whenever
	// a raised sheet or the docked card would cover them.
	const zoomShown = $derived(markersOn && snap === 'peek' && overlay === 'none' && !cardShown && !app.view);

	// Pin opacity for offline / stale lives on the marker pane (app.css).
	$effect(() => {
		const pane = map?.getPane('markerPane');
		if (!pane) return;
		const cls = view?.pinClass ?? '';
		pane.classList.remove('fs-pins-offline', 'fs-pins-stale');
		if (cls) pane.classList.add(cls);
	});

	const keyOf = (m: MapMarker) => `${m.type}@${m.x},${m.z}`;

	// The clock went away (offline, a server switch) or the map started
	// charting: close the weather sheet with it.
	$effect(() => {
		if (overlay === 'weather' && !chipShown) untrack(() => closeOverlay(false));
	});

	function select(id: string | undefined): void {
		if (id === undefined) atlas?.setPadBottom(0);
		selectedId = id;
		const m = id === undefined ? undefined : all.find((x) => x.id === id);
		selectedKey = m ? keyOf(m) : undefined;
	}

	/** Selects a marker and centres it at y ≈ 300 px (design frame 4). */
	function focusMarker(m: MapMarker, z: number): void {
		if (map) {
			const h = map.getSize().y;
			// Let maxBounds pan far enough to put the pin at y ≈ 300 (at the
			// default zoom the world barely exceeds the screen).
			atlas?.setPadBottom(cardPadBottom(h));
			atlas?.centerOn(m.x, m.z, z, centerDy(h));
		}
		select(m.id);
	}

	// A refreshed snapshot: keep the selection only if the id still names the
	// same kind at the same position.
	$effect(() => {
		if (selectedId === undefined) return;
		if (!selected || keyOf(selected) !== selectedKey) select(undefined);
	});

	// A server switch: drop the selection, close the sheets, reset the view.
	let lastServer: string | undefined;
	$effect(() => {
		const id = app.currentId;
		if (lastServer !== undefined && id !== lastServer) {
			select(undefined);
			overlay = 'none';
			snap = 'peek';
			atlas?.resetView();
		}
		lastServer = id;
	});

	function openOverlay(o: Exclude<MobileOverlay, 'none'>): void {
		if (overlay === 'none') opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
		select(undefined);
		overlay = o;
	}

	/**
	 * Closes the overlay sheet and (unless `restoreFocus` is false, e.g. when
	 * a modal takes over) returns focus to its opener, skipped if a modal
	 * such as the unlock dialog opened meanwhile.
	 */
	function closeOverlay(restoreFocus = true): void {
		if (overlay === 'none') return;
		overlay = 'none';
		const back = opener;
		opener = null;
		if (!restoreFocus || !back?.isConnected) return;
		requestAnimationFrame(() => {
			if (app.unlockPrompt || document.querySelector('[aria-modal="true"]')) return;
			back.focus({ preventScroll: true });
		});
	}

	function onmarker(id: string | undefined): void {
		const m = id === undefined ? undefined : all.find((x) => x.id === id);
		if (!m || !map) {
			select(undefined);
			return;
		}
		focusMarker(m, map.getZoom());
	}

	function jump(id: string): void {
		const partner = all.find((m) => m.id === id);
		if (!partner || !map) return;
		focusMarker(partner, Math.max(3.5, map.getZoom()));
	}

	/**
	 * A profile's "Map →": close the sheet, then centre on the item
	 * (selecting its marker when on the map). The row with focus closes
	 * along with the sheet, so once things settle (fix round 1), focus
	 * moves to the docked marker card's Close button when the item landed
	 * on one, or to the map itself otherwise (an item not currently on the
	 * map, e.g. filtered out or still unexplored).
	 */
	function mapTo(x: number, z: number, id: string): void {
		app.closeView();
		snap = 'peek';
		const m = all.find((mm) => mm.id === id);
		if (m) {
			focusMarker(m, 4);
		} else if (map) {
			atlas?.centerOn(x, z, 4, centerDy(map.getSize().y));
		}
		void tick().then(() => {
			const close = document.querySelector<HTMLElement>('[data-testid="mobile-marker-card"] [aria-label="Close"]');
			(close ?? map?.getContainer())?.focus();
		});
	}

	/**
	 * The timeline's "Show on map →" (fix round 1: focus lands on the
	 * docked marker card's Close button when one opens, matching the
	 * profile's own `mapTo` — not on the menu's carried-over opener).
	 */
	function showOnMap(x: number, z: number): void {
		app.closeView();
		snap = 'peek';
		const m = markerAt(all, x, z);
		if (m) {
			focusMarker(m, 4);
		} else if (map) {
			atlas?.centerOn(x, z, 4, centerDy(map.getSize().y));
		}
		void tick().then(() => {
			const close = document.querySelector<HTMLElement>('[data-testid="mobile-marker-card"] [aria-label="Close"]');
			(close ?? map?.getContainer())?.focus();
		});
	}

	/** Search pick (§5.2, Mobile ruling): close the menu, select the marker at zoom 4.25. */
	function pickResult(m: MapMarker): void {
		closeOverlay();
		focusMarker(m, SEARCH_ZOOM);
	}

	function toggleLayer(key: LayerKey): void {
		layers[key] = !layers[key];
	}

	function toast(title: string, sub: string): void {
		app.showToast(title, sub === '' ? undefined : sub);
	}

	function onscrim(): void {
		if (overlay !== 'none') closeOverlay();
		else snap = 'peek';
	}

	function onkeydown(e: KeyboardEvent): void {
		if (e.key !== 'Escape' || e.defaultPrevented || app.unlockPrompt) return;
		if (app.view) {
			e.preventDefault();
			app.closeView();
		} else if (overlay !== 'none') {
			e.preventDefault();
			closeOverlay();
		} else if (selectedId !== undefined) {
			select(undefined);
		} else if (snap === 'pulled') {
			snap = 'peek';
		}
	}

	let frame = 0;
	function onmove(): void {
		if (frame) return;
		frame = requestAnimationFrame(() => {
			frame = 0;
			if (map) zoom = map.getZoom();
		});
	}
	$effect(() => () => {
		if (frame) cancelAnimationFrame(frame);
	});
</script>

<svelte:window {onkeydown} />

<main class="shell" data-layout="mobile">
	{#if view?.overlay.kind === 'waiting' || view?.overlay.kind === 'charting'}
		<!-- Centred in the map area between the top bar and the peek sheet. -->
		<div class="centre-slot" style:bottom="{peekH}px">
			{#if view.overlay.kind === 'waiting'}
				<WaitingPill padLeft={0} />
			{:else}
				{@const o = view.overlay}
				<ChartingCard pct={o.pct} done={o.done} total={o.total} etaMin={o.etaMin} padLeft={0} />
			{/if}
		</div>
	{/if}
	{#if (app.disconnected || view?.overlay.kind === 'banner' || chipShown) && overlay !== 'join'}
		<div class="banner-slot">
			{#if app.disconnected}
				<ConnectionBanner mobile />
			{:else if view?.overlay.kind === 'banner'}
				{@const o = view.overlay}
				<StateBanner tone={o.tone} title={o.title} body={o.body} mobile />
			{/if}
			{#if time && chipShown}
				<WeatherPill
					view={time}
					mobile
					open={overlay === 'weather'}
					onopen={() => (overlay === 'weather' ? closeOverlay() : openOverlay('weather'))}
				/>
			{/if}
		</div>
	{/if}

	{#if zoomShown}
		<ZoomControls
			mobile
			bottom={zoomBottom(peekH)}
			onzoomin={() => atlas?.zoomBy(0.75)}
			onzoomout={() => atlas?.zoomBy(-0.75)}
			onreset={() => {
				atlas?.resetView();
				select(undefined);
			}}
		/>
	{/if}

	{#if snap === 'pulled' || overlay !== 'none'}
		<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions (Esc closes via the window handler) -->
		<div class="scrim" class:over={overlay !== 'none'} onclick={onscrim} data-testid="mobile-scrim"></div>
	{/if}

	<PeekSheet
		{card}
		now={app.now}
		{snap}
		onsnap={(s) => (snap = s)}
		onjoin={() => openOverlay('join')}
		hidden={cardShown}
		bind:peekHeight={peekH}
	/>

	{#if selected && cardShown}
		<MobileMarkerCard m={selected.card} onclose={() => select(undefined)} onjump={jump} />
	{/if}

	<MobileTopBar
		name={card?.name ?? summary?.name ?? ''}
		{sub}
		menuOpen={overlay === 'menu'}
		switcherOpen={overlay === 'server'}
		flat={snap === 'pulled' || overlay !== 'none'}
		onname={() => (overlay === 'server' ? closeOverlay() : openOverlay('server'))}
		onmenu={() => (overlay === 'menu' ? closeOverlay() : openOverlay('menu'))}
	/>

	{#if overlay === 'menu'}
		<!-- The menu and server sheets are not aria-modal: their toggle (the
		     top-bar menu button / server name) stays visible and must stay
		     reachable to close them; Esc and the scrim close them too. The
		     full-height join sheet covers the top bar and is modal. -->
		<MenuSheet
			{all}
			disabled={!markersOn}
			disabledPlaceholder={view?.overlay.kind === 'charting' ? 'Charting the map…' : 'Waiting for the first save…'}
			{layers}
			{portalLinks}
			{counts}
			ontoggle={toggleLayer}
			onlinks={() => (portalLinks = !portalLinks)}
			onpick={pickResult}
			ontimeline={() => {
				activityOpener = opener;
				closeOverlay(false);
				app.openView({ kind: 'activity' });
			}}
			onclose={() => closeOverlay()}
		/>
	{:else if overlay === 'server'}
		<ServerSheet onclose={closeOverlay} />
	{:else if overlay === 'join' && card}
		<JoinSheet {card} onclose={() => closeOverlay()} oncopy={toast} />
	{:else if overlay === 'weather' && time}
		<WeatherSheet view={time} onclose={() => closeOverlay()} />
	{/if}

	{#if profilePlayer !== undefined && app.currentId}
		<ProfileSheet serverId={app.currentId} player={profilePlayer} onclose={() => app.closeView()} onmap={mapTo} />
	{:else if app.view?.kind === 'activity' && app.currentId}
		<ActivitySheet
			serverId={app.currentId}
			gameDay={card?.world?.day}
			returnTo={() => activityOpener}
			onclose={() => app.closeView()}
			onmap={showOnMap}
		/>
	{/if}

	<!-- Last in the DOM, so Tab reaches the top bar, the sheets and the
	     controls before the map and its pins (each one a tab stop);
	     AtlasMap sits under them all with z-index -1. -->
	<AtlasMap
		bind:this={atlas}
		{card}
		snapshot={app.snapshot}
		padLeft={0}
		defaultZoom={DEFAULT_ZOOM}
		{dim}
		filter={view?.filter ?? ''}
		onready={(m) => {
			map = m;
			zoom = m.getZoom();
		}}
		onclick={() => select(undefined)}
		{onmove}
	/>
	{#if map}
		<MarkerLayer {map} {all} {layers} {portalLinks} {selectedId} onselect={onmarker} />
	{/if}
</main>

<style>
	.shell {
		position: fixed;
		inset: 0;
		overflow: hidden;
		height: 100%;
	}
	.banner-slot {
		position: absolute;
		left: 12px;
		right: 12px;
		top: calc(env(safe-area-inset-top) + 78px);
		z-index: 10;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 8px;
		/* The slot spans the width; only its banner and chip take touches. */
		pointer-events: none;
	}
	.banner-slot > :global(*) {
		align-self: stretch;
		pointer-events: auto;
	}
	.banner-slot > :global([data-testid='weather-chip']) {
		align-self: flex-start;
	}
	.centre-slot {
		position: absolute;
		left: 0;
		right: 0;
		top: calc(env(safe-area-inset-top) + 70px);
		pointer-events: none;
	}
	.scrim {
		position: absolute;
		inset: 0;
		z-index: 25;
	}
	.scrim.over {
		z-index: 38;
	}
</style>
