import { describe, expect, test, vi } from 'vitest';
import { TILE_SWAP_TIMEOUT_MS, scheduleTileSwap, type SwappableLayer } from './tile-swap';

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
