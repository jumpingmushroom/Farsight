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
