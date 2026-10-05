import { describe, expect, test } from 'vitest';
import { dayKey, daysBetween, fullDayLabel, prevDayKey, weekdayInitial, zClock, zDate, zDayRef, zWeekday, zoned } from './zoned';

describe('zoned', () => {
	test('wall clock in the server zone, not the viewer’s', () => {
		// 22:30 UTC is already 00:30 the next day in Oslo (CEST).
		expect(zoned('2026-10-05T22:30:00Z', 'Europe/Oslo')).toEqual({ y: 2026, m: 10, d: 6, hh: 0, mm: 30, wd: 2 });
		expect(dayKey('2026-10-05T22:30:00Z', 'Europe/Oslo')).toBe('2026-10-06');
		expect(dayKey('2026-10-05T22:30:00Z', 'UTC')).toBe('2026-10-05');
		expect(zClock('2026-10-05T22:30:00Z', 'Europe/Oslo')).toBe('00:30');
	});
	test('an unknown zone falls back to UTC', () => {
		expect(zClock('2026-10-05T22:30:00Z', 'Mars/Olympus')).toBe('22:30');
	});
	test('labels', () => {
		expect(zDate('2026-09-30T08:00:00Z', 'UTC')).toBe('30 Sep 2026');
		expect(zWeekday('2026-09-29T08:00:00Z', 'UTC')).toBe('Tue 29 Sep');
		expect(prevDayKey('2026-10-01')).toBe('2026-09-30');
		expect(weekdayInitial('2026-10-05')).toBe('M');
		expect(fullDayLabel('2026-09-29')).toBe('Tuesday 29 Sep');
	});
	test('zDayRef', () => {
		const now = new Date('2026-10-05T12:00:00Z');
		expect(zDayRef('2026-10-05T03:12:00Z', 'UTC', now)).toBe('today 03:12');
		expect(zDayRef('2026-10-04T23:50:00Z', 'UTC', now)).toBe('yesterday 23:50');
		expect(zDayRef('2026-10-04T23:50:00Z', 'Europe/Oslo', now)).toBe('today 01:50');
		expect(zDayRef('2026-09-28T03:12:00Z', 'UTC', now)).toBe('28 Sep 03:12');
	});
	test('daysBetween (fix round 2): pure calendar arithmetic, including across a DST transition', () => {
		expect(daysBetween('2026-09-27', '2026-09-30')).toBe(3);
		expect(daysBetween('2026-09-30', '2026-09-27')).toBe(-3);
		expect(daysBetween('2026-09-27', '2026-09-27')).toBe(0);
		// Europe/Oslo's autumn DST transition (25 Oct 2026) sits in between;
		// the calendar-day count is still exact.
		expect(daysBetween('2026-10-20', '2026-10-27')).toBe(7);
	});
});
