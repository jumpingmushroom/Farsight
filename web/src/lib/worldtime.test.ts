// `@types/node` isn't an allowed dependency, so `process` is declared
// ambiently rather than typed via node's globals.
declare const process: { env: Record<string, string | undefined> };
process.env.TZ = 'UTC';

import { describe, expect, test } from 'vitest';
import type { Card, Clock, Weather } from './types';
import {
	clockFraction,
	clockText,
	dayFraction,
	dayOf,
	netTimeNow,
	nextText,
	periodIndex,
	phase,
	timeView,
	type TimeView,
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

describe('timeView', () => {
	const at = '2026-10-06T18:00:00Z';
	// Period 741 starts at 493506 (= 741 * 666); 493506 mod 1800 = 306 (raw 0.17, morning).
	const weather: Weather = {
		periodSec: 666,
		periods: [
			{ start: 741 * 666, byBiome: { Meadows: 'Light rain', 'Black Forest': 'Forest mist', Swamp: 'Rain' } },
			{ start: 742 * 666, byBiome: { Meadows: 'Clear', 'Black Forest': 'Clear', Swamp: 'Rain' } },
			{ start: 743 * 666, byBiome: { Meadows: 'Fog', 'Black Forest': 'Fog', Swamp: 'Fog' } }
		],
		biomes: ['Meadows', 'Black Forest', 'Swamp'],
		home: 'Meadows'
	};
	// 100 s into period 741.
	const clock = (partial: Partial<Clock> = {}) => makeClock({ netTime: 741 * 666 + 100, at, ...partial });
	const now = new Date(at);
	const tv = (card: Pick<Card, 'clock' | 'weather'>, n: Date): TimeView => {
		const v = timeView(card, n);
		if (!v) throw new Error('no view');
		return v;
	};

	test('no clock, no pill', () => {
		expect(timeView({ weather }, now)).toBeUndefined();
	});

	test('title, line and chip from the home biome', () => {
		const v = tv({ clock: clock(), weather }, now);
		expect(v.label).toBe('Morning');
		expect(v.clock).toBe(clockText(741 * 666 + 100));
		expect(v.line).toBe('Meadows: light rain');
		expect(v.chip).toBe(`${v.clock} · Light rain`);
		expect(v.night).toBe(false);
		expect(v.day).toBe(dayOf(741 * 666 + 100));
		expect(v.pct).toBeCloseTo(dayFraction(741 * 666 + 100) * 100);
	});

	test('rows are the explored biomes with legend swatches and the three periods', () => {
		const v = tv({ clock: clock(), weather }, now);
		expect(v.rows).toEqual([
			{ name: 'Meadows', hex: '#A3B25C', now: 'Light rain', next: 'Clear', then: 'Fog' },
			{ name: 'Black Forest', hex: '#3E5834', now: 'Forest mist', next: 'Clear', then: 'Fog' },
			{ name: 'Swamp', hex: '#786246', now: 'Rain', next: 'Rain', then: 'Fog' }
		]);
	});

	test('running: Next and Then headed with their start in local wall time', () => {
		// Next starts in 566 s (18:09:26), Then in 1232 s (18:20:32); TZ is UTC.
		const v = tv({ clock: clock(), weather }, now);
		expect(v.heads).toEqual({ next: 'from 18:09', then: 'from 18:20' });
		expect(v.pausedText).toBeUndefined();
	});

	test('the clock ticks forward while running', () => {
		const v = tv({ clock: clock(), weather }, new Date(Date.parse(at) + 600_000));
		// 700 s into the period: now is the second period.
		expect(v.rows[0]).toMatchObject({ now: 'Clear', next: 'Fog', then: '—' });
		expect(v.line).toBe('Meadows: clear');
	});

	test('paused: frozen clock, " · paused" line, "after" headers and the paused text', () => {
		const v = tv({ clock: clock({ running: false }), weather }, new Date(Date.parse(at) + 3_600_000));
		expect(v.clock).toBe(clockText(741 * 666 + 100));
		expect(v.line).toBe('Meadows: light rain · paused');
		expect(v.heads).toEqual({ next: 'after Now', then: 'after Next' });
		expect(v.pausedText).toBe('Time is paused while nobody is online.');
	});

	test('past the three periods every cell shows a dash', () => {
		const v = tv({ clock: clock(), weather }, new Date(Date.parse(at) + 3 * 666_000));
		expect(v.rows[0]).toMatchObject({ now: '—', next: '—', then: '—' });
		expect(v.line).toBe('Meadows: —');
	});

	test('night uses the moon disc', () => {
		const v = tv({ clock: clock({ netTime: 742 * 1800 + 60 }), weather }, now);
		expect(v.label).toBe('Night');
		expect(v.night).toBe(true);
	});

	test('footer mentions the last sleep only when the anchor is a sleep', () => {
		const tail =
			' The world clock only runs while someone is online. Weather follows the game’s own schedule; raids, dungeons and the Dark Meadows have their own.';
		expect(tv({ clock: clock(), weather }, now).footer).toBe(`Estimated from the world clock in the last save.${tail}`);
		expect(tv({ clock: clock({ source: 'sleep' }), weather }, now).footer).toBe(
			`Estimated from the world clock in the last save and the last sleep.${tail}`
		);
	});

	test('without weather: no rows and a dash line', () => {
		const v = tv({ clock: clock(), weather: undefined }, now);
		expect(v.rows).toEqual([]);
		expect(v.line).toBe('—');
	});
});
