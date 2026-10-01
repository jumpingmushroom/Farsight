// AtlasMap.svelte's tile-layer swap.
//
// Fix round 1, item 1: adding a new Leaflet tile layer and immediately
// removing the old one leaves the map bare (parchment disc showing through)
// until the new layer's tiles have loaded. Instead the old layer stays
// until the new one's first `load` event, or `timeoutMs` as a fallback,
// whichever comes first — so the view is always covered by *some* tiles.
//
// Fix round 2, item 1: that deferral is only correct for a fog-key-only
// change (same server id, same tile-set key — virtually every save while
// players explore). For any other change — the server id or tile key
// differs, or there is nothing to show — the old layer(s) must go
// immediately, or the previous server's or world's terrain lingers under
// the next one (or forever, if there never is a next one). `applyTileLayer`
// is the single entry point AtlasMap.svelte calls; it picks between the two.
//
// This is kept Leaflet-agnostic (duck-typed on the handful of methods used)
// so it can be unit tested without a real map or DOM.

/** The subset of L.TileLayer's API this module needs. */
export interface SwappableLayer {
	on(event: 'load', fn: () => void): unknown;
	off(event: 'load', fn: () => void): unknown;
	remove(): unknown;
}

/**
 * AtlasMap's tile identity (final review I1/N1): the server id and
 * tile-set key, as one string, or undefined when there's no tile key yet.
 * A fog-key-only change — virtually every save while players explore —
 * keeps this unchanged. Pulled out as a pure function so AtlasMap.svelte
 * can wrap it in a `$derived`: Svelte deriveds are cached by value, so an
 * effect that reads only this (and `tileSrc`, itself a derived) re-runs
 * only when the identity string actually changes — not on every 15 s card
 * poll, which hands the component a brand new `card` object whether or not
 * its id or tile key changed.
 */
export function tileLayerIdentity(id: string | undefined, tilesKey: string | undefined): string | undefined {
	return id && tilesKey ? `${id}|${tilesKey}` : undefined;
}

/** Falls back to removing the old layer(s) even if `load` never fires. */
export const TILE_SWAP_TIMEOUT_MS = 3000;

/**
 * Registers `layer` as the newest of a chain tracked in `pending` (oldest
 * first; mutated in place). If `pending` already held layers when called,
 * they are removed — together, in case an even earlier generation never
 * got the chance to retire its own predecessors — once `layer` loads or
 * `timeoutMs` elapses. `layer` itself is pushed onto `pending` and is never
 * removed here; a later call retires it in turn.
 *
 * Returns a cancel function for the caller's effect cleanup: it stops this
 * registration's own `load` listener and timeout, but deliberately leaves
 * `layer` (and anything it was scheduled to retire) exactly as they are,
 * since a later registration's `pending` snapshot — taken at ITS call time
 * — still includes them and will retire them when it is this generation's
 * turn. Safe to call more than once, and after `layer` has already loaded.
 */
export function scheduleTileSwap(
	layer: SwappableLayer,
	pending: SwappableLayer[],
	timeoutMs = TILE_SWAP_TIMEOUT_MS,
	setTimer: typeof setTimeout = setTimeout,
	clearTimer: typeof clearTimeout = clearTimeout
): () => void {
	const toRetire = pending.slice();
	pending.push(layer);
	if (toRetire.length === 0) return () => {};

	let timer: ReturnType<typeof setTimeout> | undefined;
	let done = false;
	const retire = () => {
		if (done) return;
		done = true;
		if (timer !== undefined) clearTimer(timer);
		layer.off('load', retire);
		for (const old of toRetire) {
			old.remove();
			const i = pending.indexOf(old);
			if (i !== -1) pending.splice(i, 1);
		}
	};
	timer = setTimer(retire, timeoutMs);
	layer.on('load', retire);

	return () => {
		if (done) return;
		if (timer !== undefined) clearTimer(timer);
		layer.off('load', retire);
	};
}

/** Removes and drops every layer currently tracked in `pending` (mutated in place, emptied). */
export function clearAllLayers(pending: SwappableLayer[]): void {
	for (const old of pending.splice(0)) old.remove();
}

/**
 * One AtlasMap tile-layer effect run's worth of reconciliation (fix round
 * 2, item 1). `next` is the new layer, already added to the map by the
 * caller, or undefined when there is nothing to show this run (no fog key
 * yet, no snapshot, a server switch, tiles not complete).
 *
 * - `sameIdentity` false (including whenever `next` is undefined): every
 *   layer in `pending` is removed and dropped immediately — the swap is
 *   never deferred across a server or tile-set change, so stale terrain
 *   never lingers under (or instead of) the next thing shown.
 * - `sameIdentity` true: `next` is handed to `scheduleTileSwap` instead, so
 *   the old layer(s) stay until `next` loads or a timeout elapses.
 *
 * Returns a cleanup function for the caller's effect, exactly as
 * `scheduleTileSwap` does (a no-op when `next` is undefined, since nothing
 * was scheduled).
 */
export function applyTileLayer(
	pending: SwappableLayer[],
	next: SwappableLayer | undefined,
	sameIdentity: boolean,
	timeoutMs = TILE_SWAP_TIMEOUT_MS,
	setTimer: typeof setTimeout = setTimeout,
	clearTimer: typeof clearTimeout = clearTimeout
): () => void {
	if (!sameIdentity) clearAllLayers(pending);
	if (!next) return () => {};
	return scheduleTileSwap(next, pending, timeoutMs, setTimer, clearTimer);
}
