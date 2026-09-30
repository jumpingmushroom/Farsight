import './testing/leaflet-node';
import L from 'leaflet';
import { describe, expect, test } from 'vitest';

// AtlasMap overrides Leaflet's internal Map#_getBoundsOffset so maxBounds
// limit the area right of the side panel. Fail loudly if an upgrade drops it.
describe('Leaflet internals used by AtlasMap', () => {
	test('Map.prototype._getBoundsOffset exists', () => {
		expect(typeof (L.Map.prototype as unknown as Record<string, unknown>)._getBoundsOffset).toBe('function');
	});
});

// fog.ts subclasses L.Renderer (the base of L.Canvas) and relies on its
// view bookkeeping: _update sets _bounds/_center/_zoom for the padded view,
// _updateTransform/_onAnimZoom/_onZoom scale the container during zoom
// animation (via Map#_getNewPixelOrigin), and _reset handles viewreset. It
// skips redraws while Map#_animatingZoom is set, as L.Canvas does.
describe('Leaflet internals used by the fog layer', () => {
	const R = (L.Renderer as unknown as { prototype: Record<string, unknown> }).prototype;
	test.each(['_update', '_updateTransform', '_onAnimZoom', '_onZoom', '_onZoomEnd', '_reset', 'getEvents'])(
		'Renderer.prototype.%s exists',
		(name) => {
			expect(typeof R[name]).toBe('function');
		}
	);
	test('Map.prototype._getNewPixelOrigin exists', () => {
		expect(typeof (L.Map.prototype as unknown as Record<string, unknown>)._getNewPixelOrigin).toBe('function');
	});
	test('Map sets _animatingZoom during zoom animation', () => {
		const src = String((L.Map.prototype as unknown as Record<string, unknown>)._animateZoom);
		expect(src).toContain('_animatingZoom = true');
	});
	test('Renderer._update fills _bounds, _center and _zoom', () => {
		const src = String(R._update);
		for (const k of ['_bounds', '_center', '_zoom', 'padding']) expect(src).toContain(k);
	});
});
