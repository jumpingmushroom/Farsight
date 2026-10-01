import { describe, expect, test, vi } from 'vitest';
import { TILE_SWAP_TIMEOUT_MS, applyTileLayer, clearAllLayers, scheduleTileSwap, type SwappableLayer } from './tile-swap';

class FakeLayer implements SwappableLayer {
	removed = false;
	private listeners: (() => void)[] = [];
	on(_event: 'load', fn: () => void): this {
		this.listeners.push(fn);
		return this;
	}
	off(_event: 'load', fn: () => void): this {
		this.listeners = this.listeners.filter((f) => f !== fn);
		return this;
	}
	remove(): this {
		this.removed = true;
		return this;
	}
	fireLoad(): void {
		for (const fn of [...this.listeners]) fn();
	}
}

describe('scheduleTileSwap', () => {
	test('the first layer has nothing to retire: it is pushed, nothing removed, cancel is harmless', () => {
		const pending: SwappableLayer[] = [];
		const a = new FakeLayer();
		const cancel = scheduleTileSwap(a, pending);
		expect(pending).toEqual([a]);
		expect(a.removed).toBe(false);
		expect(() => cancel()).not.toThrow();
	});

	test('load fires before the timeout: the old layer is removed, the new one stays', () => {
		vi.useFakeTimers();
		const pending: SwappableLayer[] = [];
		const a = new FakeLayer();
		scheduleTileSwap(a, pending);
		const b = new FakeLayer();
		scheduleTileSwap(b, pending);
		expect(pending).toEqual([a, b]);

		b.fireLoad();
		expect(a.removed).toBe(true);
		expect(b.removed).toBe(false);
		expect(pending).toEqual([b]);
		vi.useRealTimers();
	});

	test('load never fires: the timeout retires the old layer instead', () => {
		vi.useFakeTimers();
		const pending: SwappableLayer[] = [];
		const a = new FakeLayer();
		scheduleTileSwap(a, pending);
		const b = new FakeLayer();
		scheduleTileSwap(b, pending);

		vi.advanceTimersByTime(TILE_SWAP_TIMEOUT_MS - 1);
		expect(a.removed).toBe(false);
		vi.advanceTimersByTime(1);
		expect(a.removed).toBe(true);
		expect(pending).toEqual([b]);

		// A load that fires after the timeout already retired is a no-op.
		expect(() => b.fireLoad()).not.toThrow();
		vi.useRealTimers();
	});

	test('cancel before load/timeout leaves the layer and its backlog in place for a later generation to retire', () => {
		vi.useFakeTimers();
		const pending: SwappableLayer[] = [];
		const a = new FakeLayer();
		scheduleTileSwap(a, pending);
		const b = new FakeLayer();
		const cancelB = scheduleTileSwap(b, pending); // effect cleaned up before b loaded
		cancelB();
		// Neither removed, and b's own timer/listener no longer fire.
		vi.advanceTimersByTime(TILE_SWAP_TIMEOUT_MS + 1);
		expect(a.removed).toBe(false);
		expect(b.removed).toBe(false);

		const c = new FakeLayer();
		scheduleTileSwap(c, pending);
		expect(pending).toEqual([a, b, c]);
		c.fireLoad();
		expect(a.removed).toBe(true);
		expect(b.removed).toBe(true);
		expect(c.removed).toBe(false);
		expect(pending).toEqual([c]);
		vi.useRealTimers();
	});

	test('cancel after retire already ran does not throw or double-remove', () => {
		vi.useFakeTimers();
		const pending: SwappableLayer[] = [];
		const a = new FakeLayer();
		scheduleTileSwap(a, pending);
		const b = new FakeLayer();
		const cancelB = scheduleTileSwap(b, pending);
		b.fireLoad();
		expect(a.removed).toBe(true);
		expect(() => cancelB()).not.toThrow();
		vi.useRealTimers();
	});
});

describe('clearAllLayers', () => {
	test('removes and drops every tracked layer, leaving the array empty', () => {
		const pending: SwappableLayer[] = [];
		const a = new FakeLayer();
		const b = new FakeLayer();
		scheduleTileSwap(a, pending);
		scheduleTileSwap(b, pending); // b never loads: a is still pending retirement
		expect(pending).toEqual([a, b]);

		clearAllLayers(pending);
		expect(a.removed).toBe(true);
		expect(b.removed).toBe(true);
		expect(pending).toEqual([]);
	});

	test('an empty array is a no-op', () => {
		const pending: SwappableLayer[] = [];
		expect(() => clearAllLayers(pending)).not.toThrow();
		expect(pending).toEqual([]);
	});
});

// Fix round 2, item 1: AtlasMap must defer removing the old layer only for
// a fog-key-only change (same server id, same tile-set key). Any other
// change — the server or tile key differs, or there's nothing to show —
// must clear every live layer immediately, or the previous server's or
// world's terrain lingers on screen (the re-review's I1).
describe('applyTileLayer', () => {
	test('same identity: defers to scheduleTileSwap (the old layer waits for load/timeout)', () => {
		vi.useFakeTimers();
		const pending: SwappableLayer[] = [];
		const a = new FakeLayer();
		applyTileLayer(pending, a, false); // the very first layer: nothing to defer
		const b = new FakeLayer();
		applyTileLayer(pending, b, true);
		expect(pending).toEqual([a, b]);
		expect(a.removed).toBe(false);

		b.fireLoad();
		expect(a.removed).toBe(true);
		expect(pending).toEqual([b]);
		vi.useRealTimers();
	});

	test('different identity (server or tile key changed): every live layer, including one still pending retirement, is removed at once', () => {
		vi.useFakeTimers();
		const pending: SwappableLayer[] = [];
		const a = new FakeLayer();
		applyTileLayer(pending, a, false);
		const b = new FakeLayer();
		applyTileLayer(pending, b, true); // a fog-key-only change: b defers a's removal
		expect(a.removed).toBe(false); // still waiting on b's load/timeout

		const c = new FakeLayer(); // a different server or tile key
		applyTileLayer(pending, c, false);
		expect(a.removed).toBe(true);
		expect(b.removed).toBe(true);
		expect(c.removed).toBe(false);
		expect(pending).toEqual([c]);
		vi.useRealTimers();
	});

	test('no next layer (nothing to show): every live layer is removed immediately, and the cleanup is a harmless no-op', () => {
		const pending: SwappableLayer[] = [];
		const a = new FakeLayer();
		applyTileLayer(pending, a, false);
		expect(pending).toEqual([a]);

		const cancel = applyTileLayer(pending, undefined, false);
		expect(a.removed).toBe(true);
		expect(pending).toEqual([]);
		expect(() => cancel()).not.toThrow();
	});

	test('cleanup (effect rerun or unmount) of a same-identity call behaves exactly like scheduleTileSwap’s own cancel', () => {
		vi.useFakeTimers();
		const pending: SwappableLayer[] = [];
		const a = new FakeLayer();
		applyTileLayer(pending, a, false);
		const b = new FakeLayer();
		const cancel = applyTileLayer(pending, b, true);
		cancel(); // the effect cleaned up before b loaded
		vi.advanceTimersByTime(TILE_SWAP_TIMEOUT_MS + 1);
		expect(a.removed).toBe(false); // left for a later generation, per scheduleTileSwap
		expect(b.removed).toBe(false);
		vi.useRealTimers();
	});
});
