import './testing/leaflet-node';
import { describe, expect, test } from 'vitest';
import {
	CRS,
	MAX_BOUNDS,
	WORLD_BOUNDS,
	WORLD_RADIUS,
	dir8,
	distance,
	fromLatLng,
	insideWorld,
	metresPerPixel,
	scaleBar,
	toLatLng,
	zoneOf
} from './geo';

function px(x: number, z: number, zoom: number) {
	const p = CRS.latLngToPoint(toLatLng(x, z), zoom);
	return [p.x, p.y];
}

describe('CRS', () => {
	test('north-west world corner is pixel (0,0) at zoom 0', () => {
		const [x, y] = px(-10500, 10500, 0);
		expect(x).toBeCloseTo(0, 9);
		expect(y).toBeCloseTo(0, 9);
	});
	test('south-east world corner is pixel (256,256) at zoom 0', () => {
		const [x, y] = px(10500, -10500, 0);
		expect(x).toBeCloseTo(256, 9);
		expect(y).toBeCloseTo(256, 9);
	});
	test('origin is the centre of the 8192-px z5 image', () => {
		const [x, y] = px(0, 0, 5);
		expect(x).toBeCloseTo(4096, 9);
		expect(y).toBeCloseTo(4096, 9);
	});
	test('north is up: larger z is a smaller pixel y', () => {
		expect(px(0, 100, 3)[1]).toBeLessThan(px(0, 0, 3)[1]);
		expect(px(100, 0, 3)[0]).toBeGreaterThan(px(0, 0, 3)[0]);
	});
	test('fromLatLng round-trips toLatLng', () => {
		expect(fromLatLng(toLatLng(123, -456))).toEqual({ x: 123, z: -456 });
	});
	test('pointToLatLng inverts latLngToPoint', () => {
		const ll = CRS.pointToLatLng(CRS.latLngToPoint(toLatLng(-2500, 777), 4), 4);
		const w = fromLatLng(ll);
		expect(w.x).toBeCloseTo(-2500, 6);
		expect(w.z).toBeCloseTo(777, 6);
	});
});

describe('bounds', () => {
	test('world bounds span ±R on both axes', () => {
		expect(WORLD_RADIUS).toBe(10500);
		expect(WORLD_BOUNDS.getSouthWest()).toMatchObject({ lat: -10500, lng: -10500 });
		expect(WORLD_BOUNDS.getNorthEast()).toMatchObject({ lat: 10500, lng: 10500 });
	});
	test('max bounds are the world square plus 20%', () => {
		expect(MAX_BOUNDS.getSouthWest().lat).toBeCloseTo(-12600, 9);
		expect(MAX_BOUNDS.getNorthEast().lng).toBeCloseTo(12600, 9);
	});
});

describe('scale', () => {
	test('metresPerPixel', () => {
		expect(metresPerPixel(5)).toBe(2.5634765625);
		expect(metresPerPixel(0)).toBeCloseTo(82.03125, 9);
	});
	test('scaleBar(3) is a 98 px 1 km bar', () => {
		expect(scaleBar(3)).toEqual({ px: 98, label: '1 km' });
	});
	test('scaleBar labels metres below 1 km and decimals above', () => {
		// z5: mpp 2.5635, target 282 m, 0.7× = 197.4 → 200 m, 78 px
		expect(scaleBar(5)).toEqual({ px: 78, label: '200 m' });
		// z1: mpp 41.0156, target 4511.7 m, 0.7× = 3158.2 → 5000 m, 122 px
		expect(scaleBar(1)).toEqual({ px: 122, label: '5 km' });
		// z1.75: mpp 24.3907, target 2683 m, 0.7× = 1878.1 → 2000 m, 82 px
		expect(scaleBar(1.75)).toEqual({ px: 82, label: '2 km' });
		// z2: mpp 20.5078, target 2255.9 m, 0.7× = 1579.1 → 2000 m, 98 px
		expect(scaleBar(2)).toEqual({ px: 98, label: '2 km' });
	});
	test('scaleBar falls back to 5 km when nothing is big enough', () => {
		expect(scaleBar(0)).toEqual({ px: 61, label: '5 km' });
	});
});

describe('dir8 and distance', () => {
	test('compass words', () => {
		expect(dir8(0, 1)).toBe('north');
		expect(dir8(1, 0)).toBe('east');
		expect(dir8(-1, -1)).toBe('south-west');
		expect(dir8(1, 1)).toBe('north-east');
		expect(dir8(0, -1)).toBe('south');
		expect(dir8(-1, 0)).toBe('west');
	});
	test('distance is Euclidean in x/z', () => {
		expect(distance({ x: 0, z: 0 }, { x: 300, z: 400 })).toBe(500);
	});
});

describe('zones and world', () => {
	test('zoneOf uses the backend floor((v+32)/64) convention', () => {
		expect(zoneOf(31.9, -32)).toEqual([0, 0]);
		expect(zoneOf(32, -32.1)).toEqual([1, -1]);
		expect(Object.is(zoneOf(-0.5, 0)[0], -0)).toBe(false);
	});
	test('insideWorld', () => {
		expect(insideWorld(10500, 0)).toBe(true);
		expect(insideWorld(10500, 1)).toBe(false);
		expect(insideWorld(0, 0)).toBe(true);
	});
});
