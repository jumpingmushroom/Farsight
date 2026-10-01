// AtlasMap.svelte's tile-layer swap (fix round 1, item 1): adding a new
// Leaflet tile layer and immediately removing the old one leaves the map
// bare (parchment disc showing through) until the new layer's tiles have
// loaded. Instead the old layer stays until the new one's first `load`
// event, or `timeoutMs` as a fallback, whichever comes first — so the view
// is always covered by *some* tiles.
//
// This is kept Leaflet-agnostic (duck-typed on the handful of methods used)
// so it can be unit tested without a real map or DOM.

/** The subset of L.TileLayer's API this module needs. */
export interface SwappableLayer {
	on(event: 'load', fn: () => void): unknown;
	off(event: 'load', fn: () => void): unknown;
	remove(): unknown;
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
