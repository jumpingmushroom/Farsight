// The map shells' shared model (review fix): what DesktopShell and
// MobileShell used to derive and do line for line — the current server's
// card, the world clock, the map view, the memoised marker model and its
// layer counts, the selection, the layer toggles and the pin-opacity class
// on the marker pane. Each shell creates one ShellModel during its own
// initialisation (its effects belong to the shell) and adds what differs
// through `ShellHooks`.
//
// The layer toggles and the selection live in `shellPrefs`, outside either
// shell: a tablet rotating across the 768 px breakpoint swaps one shell for
// the other, which used to reset them silently.

import type L from 'leaflet';
import { untrack } from 'svelte';
import { mapView } from './derive';
import { buildMarkers, defaultLayers, layerCounts, markersKey, visibleMarkers, type LayerKey, type MapMarker } from './markers';
import { app as defaultApp, type AppState } from './state.svelte';
import type { Card } from './types';
import { timeView, tintOf } from './worldtime';

/** What survives a shell switch: the layer toggles and the selection. */
export class ShellPrefs {
	layers = $state(defaultLayers());
	portalLinks = $state(true);
	selectedId = $state<string | undefined>(undefined);
	/** Kind and position of the selection, to drop it when a new snapshot reuses the id (§5.4). */
	selectedKey: string | undefined;
}

export const shellPrefs = new ShellPrefs();

export interface ShellHooks {
	/** Runs first in every select(): desktop closes its menus, mobile releases the docked card's padding. */
	onselect?: (id: string | undefined) => void;
	/** A server switch, after the selection is dropped: each shell closes its own menus or sheets and resets the view. */
	onswitch?: () => void;
}

export interface ShellOptions extends ShellHooks {
	/** The zoom assumed until the map reports its own (mobile: its default zoom). */
	zoom?: number;
	/** For tests; the app singleton and the shared prefs otherwise. */
	app?: AppState;
	prefs?: ShellPrefs;
}

/** Kind and position: what a selection must still match after a new snapshot. */
export const keyOf = (m: MapMarker) => `${m.type}@${m.x},${m.z}`;

export class ShellModel {
	readonly prefs: ShellPrefs;
	/** Set by the shell once AtlasMap is ready. */
	map = $state.raw<L.Map | undefined>(undefined);
	/** The map's zoom, as last reported by the shell (it decides whether the selection's pin is shown). */
	zoom = $state(0);

	/** The current server's card: one still held for the previous server reads as none. */
	readonly card = $derived.by<Card | undefined>(() => (this.app.card?.id === this.app.currentId ? this.app.card : undefined));

	// The world clock (Plan 9): ticks every second while it runs (app.now
	// only ticks every 30 s); paused, netTimeNow ignores the time. It stops
	// while Farsight can't be reached: whether the world clock still runs
	// isn't known then.
	clockNow = $state(new Date());
	readonly clockRunning = $derived.by(() => !!this.card?.clock?.running && !this.app.disconnected);
	readonly time = $derived.by(() => (this.card ? timeView(this.card, this.app.cardAt ?? this.clockNow, this.clockNow) : undefined));
	/** A primitive, so mapView only re-runs when the phase's tint changes. */
	readonly tint = $derived(this.time ? tintOf(this.time.phase) : undefined);

	// The state treatment (§3.22): overlay, tile filter and pin opacity.
	// `app.tileSamples` is replaced together with `app.card`, so reading it
	// here stays current.
	readonly view = $derived.by(() =>
		this.card ? mapView(this.card, this.app.now, this.app.tileSamples, this.prefs.layers.biomes, this.tint) : undefined
	);
	/** Markers are hidden while waiting for a save and while charting. */
	readonly markersOn = $derived(!!this.view?.markersOn);

	// The marker model is memoised on (server, save, defeated bosses): a card
	// poll (a new object every 15 s) returns the same array, so search
	// results, layer counts and MarkerLayer don't rework.
	private built: { key: string; all: MapMarker[] } = { key: '', all: [] };
	readonly all = $derived.by<MapMarker[]>(() => {
		const snap = this.app.snapshot;
		const card = this.card;
		if (!this.markersOn || !snap || !card) return [];
		const key = markersKey(card.id, snap, card.world);
		if (this.built.key !== key) this.built = { key, all: buildMarkers(snap, card.world) };
		return this.built.all;
	});
	readonly counts = $derived(layerCounts(this.all));

	readonly selected = $derived.by(() => {
		const id = this.prefs.selectedId;
		return id === undefined ? undefined : this.all.find((m) => m.id === id);
	});
	/** The selection's pin is on the map: its layer is on and the zoom is past its minZoom. */
	readonly selectedShown = $derived.by(() => !!this.selected && visibleMarkers([this.selected], this.prefs.layers, this.zoom).length === 1);

	private app: AppState;
	private hooks: ShellHooks;
	private lastServer: string | undefined;

	/** Call during a shell's initialisation: the effects below are the shell's. */
	constructor(opts: ShellOptions = {}) {
		this.app = opts.app ?? defaultApp;
		this.prefs = opts.prefs ?? shellPrefs;
		this.hooks = { onselect: opts.onselect, onswitch: opts.onswitch };
		this.zoom = opts.zoom ?? 0;

		$effect(() => {
			if (!this.clockRunning) return;
			this.clockNow = new Date();
			const id = setInterval(() => (this.clockNow = new Date()), 1000);
			return () => clearInterval(id);
		});

		// Pin opacity for offline / stale lives on the marker pane (app.css).
		$effect(() => {
			const pane = this.map?.getPane('markerPane');
			if (!pane) return;
			const cls = this.view?.pinClass ?? '';
			pane.classList.remove('fs-pins-offline', 'fs-pins-stale');
			if (cls) pane.classList.add(cls);
		});

		$effect(() => this.checkSelection());
		$effect(() => this.checkServer());
	}

	get layers(): Record<LayerKey, boolean> {
		return this.prefs.layers;
	}

	get portalLinks(): boolean {
		return this.prefs.portalLinks;
	}

	get selectedId(): string | undefined {
		return this.prefs.selectedId;
	}

	select = (id: string | undefined): void => {
		this.hooks.onselect?.(id);
		this.prefs.selectedId = id;
		const m = id === undefined ? undefined : this.all.find((x) => x.id === id);
		this.prefs.selectedKey = m ? keyOf(m) : undefined;
	};

	/**
	 * A refreshed snapshot: keeps the selection only if the id still names
	 * the same kind at the same position (an effect; public for tests).
	 */
	checkSelection(): void {
		if (this.prefs.selectedId === undefined) return;
		const sel = this.selected;
		if (!sel || keyOf(sel) !== this.prefs.selectedKey) this.select(undefined);
	}

	/**
	 * A server switch drops the selection, then runs the shell's `onswitch`
	 * (an effect; public for tests). Only `app.currentId` is tracked.
	 */
	checkServer(): void {
		const id = this.app.currentId;
		if (this.lastServer !== undefined && id !== this.lastServer) {
			untrack(() => {
				this.select(undefined);
				this.hooks.onswitch?.();
			});
		}
		this.lastServer = id;
	}

	toggleLayer = (key: LayerKey): void => {
		this.prefs.layers[key] = !this.prefs.layers[key];
	};

	togglePortalLinks = (): void => {
		this.prefs.portalLinks = !this.prefs.portalLinks;
	};

	/** JoinDialog/JoinSheet's `oncopy`: an empty sub-line means none. */
	toast = (title: string, sub: string): void => {
		this.app.showToast(title, sub === '' ? undefined : sub);
	};
}
