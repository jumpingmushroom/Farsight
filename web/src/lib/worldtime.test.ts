// `@types/node` isn't an allowed dependency, so `process` is declared
// ambiently rather than typed via node's globals.
declare const process: { env: Record<string, string | undefined> };
process.env.TZ = 'UTC';

import { describe, expect, test } from 'vitest';
import type { Clock, Weather } from './types';
import {
	clockFraction,
	clockText,
	dayFraction,
	dayOf,
	netTimeNow,
	nextText,
	periodIndex,
	phase,
	tintOf
} from './worldtime';

const DAY_SEC = 1800;

function makeClock(partial: Partial<Clock>): Clock {
	return { netTime: 0, at: '2026-10-06T00:00:00Z', running: true, source: 'save', ...partial };
}

describe('dayFraction', () => {
	test('raw 0', () => {
		expect(dayFraction(0)).toBe(0);
	});
	test('wraps across day boundaries', () => {
		expect(dayFraction(DAY_SEC)).toBe(0);
		expect(dayFraction(DAY_SEC * 3 + 900)).toBeCloseTo(0.5);
	});
});

describe('clockFraction', () => {
	test('boundaries', () => {
		expect(clockFraction(0)).toBeCloseTo(0);
		expect(clockFraction(0.15)).toBeCloseTo(0.25);
		expect(clockFraction(0.5)).toBeCloseTo(0.5);
		expect(clockFraction(0.85)).toBeCloseTo(0.75);
	});
});

describe('clockText', () => {
	test('raw 0 -> 00:00', () => {
		expect(clockText(0)).toBe('00:00');
	});
	test('raw 0.15 -> 06:00', () => {
		expect(clockText(0.15 * DAY_SEC)).toBe('06:00');
	});
	test('raw 0.5 -> 12:00', () => {
		expect(clockText(0.5 * DAY_SEC)).toBe('12:00');
	});
	test('raw 0.85 -> 18:00', () => {
		expect(clockText(0.85 * DAY_SEC)).toBe('18:00');
	});
});

describe('phase', () => {
	test('raw 0 -> night', () => {
		expect(phase(0)).toBe('night');
	});
	test('raw 0.15 -> morning', () => {
		expect(phase(0.15 * DAY_SEC)).toBe('morning');
	});
	test('raw 0.5 -> day', () => {
		expect(phase(0.5 * DAY_SEC)).toBe('day');
	});
	test('raw 0.85 -> night', () => {
		expect(phase(0.85 * DAY_SEC)).toBe('night');
	});
});

describe('nextText', () => {
	test('raw 0 -> Dawn in ~5 min (270s rounds up)', () => {
		expect(nextText(0)).toBe('Dawn in ~5 min');
	});
	test('raw 0.5 (day) -> Evening in ~N min', () => {
		expect(nextText(0.5 * DAY_SEC)).toMatch(/^Evening in ~\d+ min$/);
	});
	test('raw 0.15 (morning) -> Midday in ~N min', () => {
		expect(nextText(0.15 * DAY_SEC)).toMatch(/^Midday in ~\d+ min$/);
	});
	test('raw 0.85 (night) -> Dawn in ~N min', () => {
		expect(nextText(0.85 * DAY_SEC)).toMatch(/^Dawn in ~\d+ min$/);
	});
});

describe('dayOf', () => {
	test('floor(netTime / 1800)', () => {
		expect(dayOf(0)).toBe(0);
		expect(dayOf(DAY_SEC - 1)).toBe(0);
		expect(dayOf(DAY_SEC)).toBe(1);
		expect(dayOf(DAY_SEC * 270 + 900)).toBe(270);
	});
});

describe('netTimeNow', () => {
	test('adds elapsed seconds only when running', () => {
		const at = new Date('2026-10-06T00:00:00Z');
		const running = makeClock({ netTime: 1000, at: at.toISOString(), running: true });
		const paused = makeClock({ netTime: 1000, at: at.toISOString(), running: false });
		const now = new Date(at.getTime() + 60_000);
		expect(netTimeNow(running, now)).toBe(1060);
		expect(netTimeNow(paused, now)).toBe(1000);
	});
	test('no negative elapsed when now is before at', () => {
		const at = new Date('2026-10-06T00:00:00Z');
		const clock = makeClock({ netTime: 1000, at: at.toISOString(), running: true });
		const now = new Date(at.getTime() - 5000);
		expect(netTimeNow(clock, now)).toBe(1000);
	});
});

describe('periodIndex', () => {
	function makeWeather(startPeriod: number): Weather {
		const periodSec = 666;
		return {
			periodSec,
			periods: [0, 1, 2].map((i) => ({ start: (startPeriod + i) * periodSec, byBiome: {} })),
			biomes: ['Meadows'],
			home: 'Meadows'
		};
	}

	test('0, 1, 2 within the three periods', () => {
		const w = makeWeather(10);
		expect(periodIndex(w, 10 * 666)).toBe(0);
		expect(periodIndex(w, 11 * 666 + 1)).toBe(1);
		expect(periodIndex(w, 12 * 666 + 665)).toBe(2);
	});
	test('-1 past the third period', () => {
		const w = makeWeather(10);
		expect(periodIndex(w, 13 * 666)).toBe(-1);
	});
	test('-1 before the first period', () => {
		const w = makeWeather(10);
		expect(periodIndex(w, 9 * 666)).toBe(-1);
	});
});

describe('tintOf', () => {
	test('night and evening tint; morning and day do not', () => {
		expect(tintOf('night')).toBe('night');
		expect(tintOf('evening')).toBe('evening');
		expect(tintOf('morning')).toBeUndefined();
		expect(tintOf('day')).toBeUndefined();
	});
});
