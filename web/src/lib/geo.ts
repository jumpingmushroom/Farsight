// World ↔ Leaflet coordinates (DESIGN-NOTES §7.3), the scale bar (§5.7),
// compass words (§6.2) and zone helpers.
//
// Leaflet is fed `[lat, lng] = [z, x]` in world metres. The custom
// transformation maps the world square [−R, R]² onto the 256-px zoom-0
// tile, with +x to the right and +z (north) up, matching the tile pyramid:
// x_px = 256/21000·x + 128, y_px = −256/21000·z + 128.

import L from 'leaflet';

export const WORLD_RADIUS = 10500;
const SPAN = 2 * WORLD_RADIUS; // 21000 m

export const CRS: L.CRS = L.extend({}, L.CRS.Simple, {
	transformation: new L.Transformation(256 / SPAN, 128, -256 / SPAN, 128)
});

export function toLatLng(x: number, z: number): L.LatLng {
	return L.latLng(z, x);
}

export function fromLatLng(ll: L.LatLng): { x: number; z: number } {
	return { x: ll.lng, z: ll.lat };
}

export const WORLD_BOUNDS: L.LatLngBounds = L.latLngBounds(
	[-WORLD_RADIUS, -WORLD_RADIUS],
	[WORLD_RADIUS, WORLD_RADIUS]
);

const MAX_R = WORLD_RADIUS * 1.2;
export const MAX_BOUNDS: L.LatLngBounds = L.latLngBounds([-MAX_R, -MAX_R], [MAX_R, MAX_R]);

/** Metres per screen pixel at a Leaflet zoom. */
export function metresPerPixel(zoom: number): number {
	return SPAN / (256 * 2 ** zoom);
}

const NICE = [50, 100, 200, 250, 500, 1000, 2000, 2500, 5000];

/** §5.7: a bar about 110 px long, snapped to a nice distance. */
export function scaleBar(zoom: number): { px: number; label: string } {
	const mpp = metresPerPixel(zoom);
	const target = 110 * mpp;
	const nice = NICE.find((n) => n >= target * 0.7) ?? 5000;
	return {
		px: Math.round(nice / mpp),
		label: nice >= 1000 ? `${nice / 1000} km` : `${nice} m`
	};
}

const DIRS = ['north', 'north-east', 'east', 'south-east', 'south', 'south-west', 'west', 'north-west'];

/** Compass word for a vector (dx east, dz north), §6.2. */
export function dir8(dx: number, dz: number): string {
	const deg = ((Math.atan2(dx, dz) * 180) / Math.PI + 360) % 360;
	return DIRS[Math.round(deg / 45) % 8];
}

export function distance(a: { x: number; z: number }, b: { x: number; z: number }): number {
	return Math.hypot(b.x - a.x, b.z - a.z);
}

/** Backend zone convention (`locationZone`): floor((v + 32) / 64). */
export function zoneOf(x: number, z: number): [number, number] {
	// `+ 0` turns a −0 into 0.
	return [Math.floor((x + 32) / 64) + 0, Math.floor((z + 32) / 64) + 0];
}

export function insideWorld(x: number, z: number): boolean {
	return x * x + z * z <= WORLD_RADIUS * WORLD_RADIUS;
}
