import './testing/leaflet-node';
import L from 'leaflet';
import { describe, expect, test } from 'vitest';

// AtlasMap overrides Leaflet's internal Map#_getBoundsOffset so maxBounds
// limit the area right of the side panel. Fail loudly if an upgrade drops it.
describe('Leaflet internals used by AtlasMap', () => {
	test('Map.prototype._getBoundsOffset exists', () => {
		expect(typeof (L.Map.prototype as unknown as Record<string, unknown>)._getBoundsOffset).toBe('function');
	});

	// MarkerLayer restacks pins by swapping the offset inside Marker#_zIndex
	// and calling _resetZIndex (setZIndexOffset would also reproject and move
	// every pin), and its exact fast projection mirrors
	// Map#latLngToContainerPoint's steps.
	test('Marker#_zIndex is layer-point y + zIndexOffset, written by _resetZIndex', () => {
		const proto = L.Marker.prototype as unknown as Record<string, () => void>;
		expect(proto._setPos.toString()).toMatch(/this\._zIndex = pos\.y \+ this\.options\.zIndexOffset/);
		expect(proto._resetZIndex.toString()).toMatch(/this\._updateZIndex\(0\)/);
		expect(proto._updateZIndex.toString()).toMatch(/style\.zIndex = this\._zIndex \+ offset/);
	});
	test('Map#latLngToContainerPoint is project, round, − pixel origin, + map pane position', () => {
		const src = L.Map.prototype.latLngToLayerPoint.toString();
		expect(src).toMatch(/project\(.*\)\._round\(\)/);
		expect(src).toMatch(/_subtract\(this\.getPixelOrigin\(\)\)/);
		expect(L.Map.prototype.layerPointToContainerPoint.toString()).toMatch(/add\(this\._getMapPanePos\(\)\)/);
	});
	// AtlasMap clears _animatingZoom before remove() so the zoom
	// animation's pending end timer returns without touching the removed map.
	test('Map#_onZoomTransitionEnd returns early unless _animatingZoom', () => {
		const proto = L.Map.prototype as unknown as Record<string, () => void>;
		expect(proto._onZoomTransitionEnd.toString()).toMatch(/if \(!this\._animatingZoom\) \{ return; \}/);
	});
});
