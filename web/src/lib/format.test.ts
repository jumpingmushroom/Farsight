// Deterministic clock formatting: TZ is pinned to UTC via vite.config.ts's
// `test.env`, and pinned again here per the task-2 ruling on timezone in
// tests. `@types/node` isn't an allowed dependency, so `process` is declared
// ambiently rather than typed via node's globals.
declare const process: { env: Record<string, string | undefined> };
process.env.TZ = 'UTC';

import { describe, expect, test } from 'vitest';
import {
	fmtActivityTime,
	fmtClock,
	fmtCode,
	fmtDayRef,
	fmtInt,
	fmtKm,
	fmtLastSeen,
	fmtMapAge,
	fmtN,
	fmtPct,
	fmtSession,
	fmtUptime,
	roman
} from './format';

const NOW = new Date('2026-09-30T12:00:00Z');

function isoMinutesAgo(min: number): string {
	return new Date(NOW.getTime() - min * 60_000).toISOString();
}

function isoDaysAgo(days: number): string {
	return new Date(NOW.getTime() - days * 86_400_000).toISOString();
}

describe('fmtN', () => {
	test.each([
		[-1234.4, '−1,234'],
		[3050, '3,050'],
		[0, '0'],
		[-0.4, '0']
	])('fmtN(%s) = %s', (n, want) => {
		expect(fmtN(n)).toBe(want);
	});
});

describe('fmtInt', () => {
	test('formats with en-US grouping', () => {
		expect(fmtInt(2184)).toBe('2,184');
	});
});

describe('fmtCode', () => {
	test('groups 3+3 with a space', () => {
		expect(fmtCode('318742')).toBe('318 742');
	});
});

describe('fmtClock', () => {
	test('renders 24h HH:MM in local time (UTC in tests)', () => {
		expect(fmtClock('2026-09-30T14:32:00Z')).toBe('14:32');
	});
	test('pads single-digit hours and minutes', () => {
		expect(fmtClock('2026-09-30T03:05:00Z')).toBe('03:05');
	});
});

describe('fmtSession', () => {
	test.each([
		[4320, '1h 12m'],
		[2280, '38m'],
		[97200, '1d 3h']
	])('fmtSession(%d) = %s', (sec, want) => {
		expect(fmtSession(sec)).toBe(want);
	});
});

describe('fmtUptime', () => {
	test('3d 6h', () => {
		expect(fmtUptime(3 * 86400 + 6 * 3600)).toBe('3d 6h');
	});
	test('under a day: h and m', () => {
		expect(fmtUptime(5 * 3600 + 12 * 60)).toBe('5h 12m');
	});
	test('under an hour: m only', () => {
		expect(fmtUptime(12 * 60)).toBe('12m');
	});
});

describe('fmtActivityTime', () => {
	test('5 min ago -> "5 min"', () => {
		expect(fmtActivityTime(isoMinutesAgo(5), NOW)).toBe('5 min');
	});
	test('72 min ago -> "1 h 12 m"', () => {
		expect(fmtActivityTime(isoMinutesAgo(72), NOW)).toBe('1 h 12 m');
	});
	test('3 days ago -> "27 Sep"', () => {
		expect(fmtActivityTime(isoDaysAgo(3), NOW)).toBe('27 Sep');
	});
	test('clamps under a minute to "1 min"', () => {
		expect(fmtActivityTime(isoMinutesAgo(0.2), NOW)).toBe('1 min');
	});
	test('exact hour has no minutes suffix', () => {
		expect(fmtActivityTime(isoMinutesAgo(120), NOW)).toBe('2 h');
	});
});

describe('fmtLastSeen', () => {
	test('24 min ago', () => {
		expect(fmtLastSeen(isoMinutesAgo(24), NOW)).toBe('last seen 24 min ago');
	});
	test('3 h ago (same calendar day)', () => {
		expect(fmtLastSeen(isoMinutesAgo(180), NOW)).toBe('last seen 3 h ago');
	});
	test('previous calendar day -> "yesterday", even under 24h', () => {
		// 2026-09-29T20:00:00Z is 16h before NOW but on the previous UTC day.
		expect(fmtLastSeen('2026-09-29T20:00:00Z', NOW)).toBe('last seen yesterday');
	});
	test('older than yesterday -> "D Mon"', () => {
		expect(fmtLastSeen('2026-09-25T09:00:00Z', NOW)).toBe('last seen 25 Sep');
	});
	test('midnight crossing: 20 min ago but the previous calendar day -> "yesterday"', () => {
		const midnight = new Date('2026-09-30T00:10:00Z');
		expect(fmtLastSeen('2026-09-29T23:50:00Z', midnight)).toBe('last seen yesterday');
	});
});

describe('fmtMapAge', () => {
	test.each([
		[0, 'just now'],
		[0.5, 'just now'],
		[12, '12 min ago'],
		[221, '3 h 41 min ago']
	])('fmtMapAge(%s) = %s', (min, want) => {
		expect(fmtMapAge(min)).toBe(want);
	});
});

describe('fmtDayRef', () => {
	test('today', () => {
		expect(fmtDayRef('2026-09-30T03:12:00Z', NOW)).toBe('today 03:12');
	});
	test('yesterday', () => {
		expect(fmtDayRef('2026-09-29T03:12:00Z', NOW)).toBe('yesterday 03:12');
	});
	test('older', () => {
		expect(fmtDayRef('2026-09-27T03:12:00Z', NOW)).toBe('27 Sep 03:12');
	});
});

describe('fmtPct', () => {
	test('default 1 decimal', () => {
		expect(fmtPct(12.4)).toBe('12.4%');
	});
	test('0 decimals', () => {
		expect(fmtPct(12.4, 0)).toBe('12%');
	});
});

describe('fmtKm', () => {
	test('fmtKm(2450) = "2.5 km"', () => {
		expect(fmtKm(2450)).toBe('2.5 km');
	});
});

describe('roman', () => {
	test.each([
		[1, 'I'],
		[4, 'IV'],
		[8, 'VIII']
	])('roman(%d) = %s', (n, want) => {
		expect(roman(n)).toBe(want);
	});
});
