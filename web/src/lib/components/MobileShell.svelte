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
    pill. The top-bar sub-line carries the short state text.
  - Zoom buttons: the mobile variant 47 px above the measured peek sheet,
    hidden while waiting/charting (nothing to zoom; they'd cover the card).
    No scale readout and no desktop pills on mobile.
  - A server switch clears the selection, closes the sheets and resets the
    view (the map stays mounted, like the desktop shell).
-->
<script lang="ts">
	import type L from 'leaflet';
	import { mapView } from '$lib/derive';
	import {
		buildMarkers,
		defaultLayers,
		layerCounts,
		markersKey,
		visibleMarkers,
		type LayerKey,
		type MapMarker
	} from '$lib/markers';
	import { cardPadBottom, centerDy, mobileDim, topBarSub, zoomBottom, type MobileOverlay, type Snap } from '$lib/mobile';
	import { app } from '$lib/state.svelte';
	import AtlasMap from './AtlasMap.svelte';
	import ChartingCard from './ChartingCard.svelte';
	import JoinSheet from './JoinSheet.svelte';
	import MarkerLayer from './MarkerLayer.svelte';
	import MenuSheet from './MenuSheet.svelte';
	import MobileMarkerCard from './MobileMarkerCard.svelte';
	import MobileTopBar from './MobileTopBar.svelte';
	import PeekSheet from './PeekSheet.svelte';
	import ServerSheet from './ServerSheet.svelte';
	import StateBanner from './StateBanner.svelte';
	import WaitingPill from './WaitingPill.svelte';
	import ZoomControls from './ZoomControls.svelte';

	const fog = true;
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

	const card = $derived(app.card?.id === app.currentId ? app.card : undefined);
	const summary = $derived(app.servers.find((s) => s.id === app.currentId));
	const mask = $derived(app.snapshot?.mask);
	const view = $derived(card ? mapView(card, app.now, app.tileSamples, layers.biomes) : undefined);
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
	const counts = $derived(layerCounts(all, mask, fog));

	const selected = $derived(selectedId === undefined ? undefined : all.find((m) => m.id === selectedId));
	const cardShown = $derived(!!selected && visibleMarkers([selected], layers, mask, fog, zoom).length === 1);

	const sub = $derived(topBarSub(view, card?.world, app.now, overlay === 'server'));
	const dim = $derived(mobileDim(snap, overlay));
	// Hidden while there is nothing to zoom (waiting, charting) and whenever
	// a raised sheet or the docked card would cover them.
	const zoomShown = $derived(markersOn && snap === 'peek' && overlay === 'none' && !cardShown);

	// Pin opacity for offline / stale lives on the marker pane (app.css).
	$effect(() => {
		const pane = map?.getPane('markerPane');
		if (!pane) return;
		const cls = view?.pinClass ?? '';
		pane.classList.remove('fs-pins-offline', 'fs-pins-stale');
		if (cls) pane.classList.add(cls);
	});

	const keyOf = (m: MapMarker) => `${m.type}@${m.x},${m.z}`;

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
		if (overlay !== 'none') {
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
		<MarkerLayer {map} {all} {layers} {mask} {fog} {portalLinks} {selectedId} onselect={onmarker} />
	{/if}

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
	{:else if view?.overlay.kind === 'banner' && overlay !== 'join'}
		{@const o = view.overlay}
		<div class="banner-slot">
			<StateBanner tone={o.tone} title={o.title} body={o.body} mobile />
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
			{mask}
			{fog}
			disabled={!markersOn}
			disabledPlaceholder={view?.overlay.kind === 'charting' ? 'Charting the map…' : 'Waiting for the first save…'}
			{layers}
			{portalLinks}
			{counts}
			ontoggle={toggleLayer}
			onlinks={() => (portalLinks = !portalLinks)}
			onpick={pickResult}
			onclose={() => closeOverlay()}
		/>
	{:else if overlay === 'server'}
		<ServerSheet onclose={closeOverlay} />
	{:else if overlay === 'join' && card}
		<JoinSheet {card} onclose={() => closeOverlay()} oncopy={toast} />
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
