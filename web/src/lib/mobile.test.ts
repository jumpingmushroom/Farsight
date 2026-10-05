declare const process: { env: Record<string, string | undefined> };
process.env.TZ = 'UTC';

import { describe, expect, test } from 'vitest';
import type { MapView } from './derive';
import {
	BANNER_SHORT,
	FLICK_V,
	TAP_SLOP,
	cardPadBottom,
	centerDy,
	dragHeight,
	isTap,
	mobileDim,
	releaseDismiss,
	releaseSnap,
	topBarSub,
	velocity,
	zoomBottom
} from './mobile';
import type { WorldCard } from './types';

const NOW = new Date('2026-09-30T12:00:00Z');
const ago = (sec: number) => new Date(NOW.getTime() - sec * 1000).toISOString();

function world(overrides: Partial<WorldCard> = {}): WorldCard {
	return {
		name: 'Example',
		day: 214,
		savedAt: ago(12 * 60),
		saveIntervalSec: 20 * 60,
		exploredPct: 12.4,
		bosses: [],
		modifiers: {},
		...overrides
	} as WorldCard;
}

const base = { markersOn: true, filter: '', pinClass: '' as const };
const pill: MapView = { overlay: { kind: 'pill' }, ...base };

describe('dragHeight', () => {
	test('dragging up grows the sheet by the distance moved', () => {
		expect(dragHeight(161, -100, 161, 668)).toBe(261);
	});
	test('dragging down shrinks it', () => {
		expect(dragHeight(668, 200, 161, 668)).toBe(468);
	});
	test('clamps to [min, max]', () => {
		expect(dragHeight(161, 300, 161, 668)).toBe(161);
		expect(dragHeight(161, -900, 161, 668)).toBe(668);
	});
});

describe('releaseSnap', () => {
	test('slow release snaps to the nearest height', () => {
		expect(releaseSnap(300, 161, 668, 0)).toBe('peek');
		expect(releaseSnap(500, 161, 668, 0)).toBe('pulled');
	});
	test('the midpoint goes to pulled', () => {
		expect(releaseSnap((161 + 668) / 2, 161, 668, 0)).toBe('pulled');
	});
	test('an upward flick pulls even when nearer peek', () => {
		expect(releaseSnap(200, 161, 668, -FLICK_V)).toBe('pulled');
	});
	test('a downward flick drops to peek even when nearer pulled', () => {
		expect(releaseSnap(620, 161, 668, FLICK_V)).toBe('peek');
	});
	test('a slower move than a flick uses distance', () => {
		expect(releaseSnap(620, 161, 668, FLICK_V * 0.9)).toBe('pulled');
	});
});

describe('releaseDismiss (full and auto sheets)', () => {
	test('a short drag down springs back', () => {
		expect(releaseDismiss(40, 700, 0)).toBe(false);
	});
	test('dragging past a quarter of the height (or 120 px) dismisses', () => {
		expect(releaseDismiss(180, 700, 0)).toBe(true);
		expect(releaseDismiss(120, 300, 0)).toBe(true);
		expect(releaseDismiss(119, 1000, 0)).toBe(false);
	});
	test('a downward flick dismisses after a small drag', () => {
		expect(releaseDismiss(30, 700, FLICK_V)).toBe(true);
	});
	test('an upward flick never dismisses', () => {
		expect(releaseDismiss(200, 700, -FLICK_V)).toBe(false);
	});
	test('dragging up never dismisses', () => {
		expect(releaseDismiss(-50, 700, 0)).toBe(false);
	});
});

describe('velocity', () => {
	test('px per ms over the samples inside the window', () => {
		const s = [
			{ t: 0, y: 0 },
			{ t: 200, y: 10 },
			{ t: 250, y: 30 },
			{ t: 300, y: 60 }
		];
		// window 100 ms from t=300: samples at 200..300 → (60 − 10) / 100
		expect(velocity(s, 100)).toBeCloseTo(0.5);
	});
	test('0 with fewer than two samples or no elapsed time', () => {
		expect(velocity([], 100)).toBe(0);
		expect(velocity([{ t: 5, y: 5 }], 100)).toBe(0);
		expect(velocity([{ t: 5, y: 5 }, { t: 5, y: 9 }], 100)).toBe(0);
	});
	test('a pause before release reads as slow', () => {
		const s = [
			{ t: 0, y: 0 },
			{ t: 50, y: 80 },
			{ t: 400, y: 80 }
		];
		expect(velocity(s, 100)).toBe(0);
	});
});

describe('isTap', () => {
	test('movement within the slop is a tap', () => {
		expect(isTap(0)).toBe(true);
		expect(isTap(TAP_SLOP - 1)).toBe(true);
		expect(isTap(-(TAP_SLOP - 1))).toBe(true);
		expect(isTap(TAP_SLOP)).toBe(false);
	});
});

describe('mobileDim (§3.18)', () => {
	test('peek and no overlay: undimmed', () => {
		expect(mobileDim('peek', 'none')).toBe(1);
	});
	test('pulled: .7', () => {
		expect(mobileDim('pulled', 'none')).toBe(0.7);
	});
	test('menu, join, server and weather: .55, whatever the sheet snap', () => {
		for (const o of ['menu', 'join', 'server', 'weather'] as const) {
			expect(mobileDim('peek', o)).toBe(0.55);
			expect(mobileDim('pulled', o)).toBe(0.55);
		}
	});
});

describe('zoomBottom and centerDy', () => {
	test('zoom column sits 47 px above the peek sheet (208 with the 161 px design sheet)', () => {
		expect(zoomBottom(161)).toBe(208);
	});
	test('a marker centred at y = 300 on an 844 px screen sits 122 px above the centre', () => {
		expect(centerDy(844)).toBe(-122);
		expect(centerDy(600, 300)).toBe(0);
	});
});

describe('cardPadBottom', () => {
	test('covers everything below 2 × y, so the visible centre is y (844 → 244)', () => {
		expect(cardPadBottom(844)).toBe(244);
		expect(cardPadBottom(700, 300)).toBe(100);
	});
	test('never negative on short screens', () => {
		expect(cardPadBottom(500)).toBe(0);
	});
});

describe('topBarSub (§3.17, ruling 5)', () => {
	test('ready: "Map {N} min ago · next ~{M} min" with the cold dot', () => {
		expect(topBarSub(pill, world(), NOW, false)).toEqual({ text: 'Map 12 min ago · next ~8 min', tone: 'cold' });
	});
	test('ready, saved under a minute ago', () => {
		expect(topBarSub(pill, world({ savedAt: ago(20) }), NOW, false).text).toBe('Map updated just now · next ~20 min');
	});
	test('ready, save overdue: "next save any moment"', () => {
		expect(topBarSub(pill, world({ savedAt: ago(25 * 60) }), NOW, false).text).toBe(
			'Map 25 min ago · next save any moment'
		);
	});
	test('ready without a save interval drops the next clause', () => {
		expect(topBarSub(pill, world({ saveIntervalSec: undefined }), NOW, false).text).toBe('Map 12 min ago');
	});
	test('banners use the short StateBanner titles', () => {
		const b = (tone: 'offline' | 'stale' | 'refused'): MapView => ({
			overlay: { kind: 'banner', tone, title: 'long', body: 'body' },
			...base
		});
		expect(topBarSub(b('offline'), world(), NOW, false)).toEqual({ text: BANNER_SHORT.offline, tone: 'offline' });
		expect(topBarSub(b('stale'), world(), NOW, false)).toEqual({ text: BANNER_SHORT.stale, tone: 'warn' });
		expect(topBarSub(b('refused'), world(), NOW, false)).toEqual({ text: BANNER_SHORT.refused, tone: 'warn' });
		expect(BANNER_SHORT).toEqual({
			offline: 'Server offline',
			stale: 'Map data may be out of date',
			refused: 'Can’t draw this world’s map yet'
		});
	});
	test('charting shows the percentage', () => {
		const v: MapView = { overlay: { kind: 'charting', pct: 38, done: 519, total: 1365 }, ...base, markersOn: false };
		expect(topBarSub(v, world(), NOW, false)).toEqual({ text: 'Charting the world · 38%', tone: 'cold' });
	});
	test('waiting for the first save', () => {
		const v: MapView = { overlay: { kind: 'waiting' }, ...base, markersOn: false };
		expect(topBarSub(v, undefined, NOW, false)).toEqual({ text: 'Waiting for the first world save…', tone: 'none' });
	});
	test('switcher open: "Choose a server", whatever the state', () => {
		expect(topBarSub(pill, world(), NOW, true)).toEqual({ text: 'Choose a server', tone: 'none' });
	});
	test('no card yet (a server switch loading): empty', () => {
		expect(topBarSub(undefined, undefined, NOW, false)).toEqual({ text: '', tone: 'none' });
	});
});
