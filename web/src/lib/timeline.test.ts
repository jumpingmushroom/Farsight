import { describe, expect, test } from 'vitest';
import {
	chipCounts,
	collapseAutosaves,
	eventIcon,
	eventText,
	eventTone,
	groupByDay,
	leftAfter,
	loadedDays,
	passes,
	place,
	sourceText,
	todayRowLabel,
	todayRows
} from './timeline';
import type { Activity, TodaySessions } from './types';

const CATEGORY: Record<string, Activity['category']> = {
	player_join: 'session',
	player_leave: 'session',
	event_raid: 'event',
	player_death: 'death',
	world_tombstone: 'death',
	world_portal: 'portal',
	world_portal_paired: 'portal',
	world_tame: 'tame',
	world_base_new: 'build',
	world_base_grew: 'build',
	world_boss: 'boss'
};

function ev(type: string, at: string, extra: Partial<Activity> = {}): Activity {
	const save = type.startsWith('world_') && type !== 'world_saved';
	return { id: `${type}@${at}`, type, category: CATEGORY[type] ?? 'server', source: save ? 'save' : 'log', at, who: [], ...extra };
}

describe('eventText', () => {
	test('server log events', () => {
		expect(eventText(ev('player_join', 'x', { name: 'Ragnar' }))).toBe('Ragnar joined');
		expect(eventText(ev('player_leave', 'x', { name: 'Bjørn', seconds: 3 * 3600 + 5 * 60 }))).toBe('Bjørn left after 3h 05m');
		expect(eventText(ev('player_leave', 'x', { name: 'Bjørn' }))).toBe('Bjørn left');
		expect(eventText(ev('server_starting', 'x'))).toBe('Server starting');
		expect(eventText(ev('server_ready', 'x'))).toBe('Server is up');
		expect(eventText(ev('world_saved', 'x'))).toBe('Autosave finished · map updated');
		expect(eventText(ev('join_code', 'x', { code: '252289' }))).toBe('New join code 252 289');
		expect(eventText(ev('event_raid', 'x', { raid: 'army_theelder' }))).toBe('Raid: The forest is moving');
		expect(eventText(ev('event_raid', 'x', { raid: 'army_gjall' }))).toBe('Raid: army_gjall');
		expect(eventText(ev('player_death', 'x', { name: 'Ragnar' }))).toBe('Ragnar died');
	});
	test('world save events', () => {
		expect(eventText(ev('world_tombstone', 'x', { owner: 'Ragnar', near: 'a sunken crypt', biome: 'Swamp' }))).toBe(
			'New tombstone: Ragnar, near a sunken crypt in the Swamp'
		);
		expect(eventText(ev('world_tombstone', 'x', { owner: 'Alina', biome: 'Mistlands' }))).toBe('New tombstone: Alina, in the Mistlands');
		expect(eventText(ev('world_portal', 'x', { tag: 'copper' }))).toBe('New portal “copper”, not paired with anything yet');
		expect(eventText(ev('world_portal', 'x', { tag: 'copper', paired: true }))).toBe('New portal “copper”, paired with “copper”');
		expect(eventText(ev('world_portal_paired', 'x', { tag: 'copper' }))).toBe('Portal “copper” now paired with “copper”');
		expect(eventText(ev('world_tame', 'x', { name: 'Big Mama', species: 'Lox' }))).toBe('New tame: Big Mama (Lox)');
		expect(eventText(ev('world_base_new', 'x', { name: 'Lox Ranch', biome: 'Plains' }))).toBe('New base: Lox Ranch (Plains)');
		expect(eventText(ev('world_base_grew', 'x', { name: 'Lox Ranch', grew: 120 }))).toBe('Lox Ranch grew by 120 pieces');
		expect(eventText(ev('world_boss', 'x', { boss: 'Moder' }))).toBe('Moder defeated');
	});
	test('helpers', () => {
		expect(leftAfter(40 * 60)).toBe('40m');
		expect(place({})).toBe('');
		expect(place({ near: 'Haldor' })).toBe('near Haldor');
	});
});

describe('icons, tones and sources', () => {
	test('per event', () => {
		expect(eventIcon(ev('player_leave', 'x'))).toBe('log-out');
		expect(eventIcon(ev('world_saved', 'x'))).toBe('map');
		expect(eventIcon(ev('world_tame', 'x'))).toBe('paw-print');
		expect(eventIcon(ev('event_raid', 'x'))).toBe('alert');
		expect(eventTone(ev('player_join', 'x'))).toBe('ember');
		expect(eventTone(ev('player_leave', 'x'))).toBe('neutral');
		expect(eventTone(ev('world_boss', 'x'))).toBe('sage');
		expect(eventTone(ev('world_portal', 'x'))).toBe('cold');
		expect(sourceText(ev('world_boss', '2026-10-05T12:20:00Z'), 'Europe/Oslo')).toBe('World save · 14:20');
		expect(sourceText(ev('player_join', '2026-10-05T12:20:00Z'), 'Europe/Oslo')).toBe('Server log');
	});
});

describe('filters', () => {
	const join = ev('player_join', 'x', { who: ['111'] });
	const tomb = ev('world_tombstone', 'x', { who: ['222'] });
	const save = ev('world_saved', 'x');
	test('categories off hide; chosen people keep their events and the server’s', () => {
		expect(passes(join, ['session'], [])).toBe(false);
		expect(passes(join, [], ['222'])).toBe(false);
		expect(passes(tomb, [], ['222'])).toBe(true);
		expect(passes(save, [], ['222'])).toBe(true);
		expect(passes(save, ['server'], [])).toBe(false);
	});
	test('chip counts follow the people filter, not the chips', () => {
		const counts = chipCounts([join, tomb, save], ['111']);
		expect(counts.session).toBe(1);
		expect(counts.death).toBe(0);
		expect(counts.server).toBe(1);
		expect(Object.keys(counts)).toEqual(['session', 'death', 'boss', 'build', 'portal', 'tame', 'event', 'server']);
	});
});

describe('collapseAutosaves (server review: page-seam collapsing)', () => {
	test('collapses a run across a page seam, keeping the newest', () => {
		const events = [
			ev('player_join', 'a'),
			ev('world_saved', 'b'), // newest of page 1's run
			ev('world_saved', 'c'), // page 2's newest: adjacent to b at the seam
			ev('player_leave', 'd'),
			ev('world_saved', 'e'),
			ev('world_saved', 'f')
		];
		const out = collapseAutosaves(events);
		expect(out.map((e) => e.at)).toEqual(['a', 'b', 'd', 'e']);
	});
	test('leaves an already-collapsed list untouched', () => {
		const events = [ev('world_saved', 'a'), ev('player_join', 'b'), ev('world_saved', 'c')];
		expect(collapseAutosaves(events).map((e) => e.at)).toEqual(['a', 'b', 'c']);
	});
});

describe('groupByDay', () => {
	test('today, yesterday and older, in the server’s zone', () => {
		const now = new Date('2026-09-29T12:44:00Z'); // 14:44 in Oslo
		const groups = groupByDay(
			[
				ev('player_join', '2026-09-29T12:39:00Z'),
				ev('player_leave', '2026-09-28T22:30:00Z'), // 00:30 on the 29th in Oslo
				ev('world_boss', '2026-09-28T20:50:00Z'),
				ev('server_ready', '2026-09-27T14:20:00Z')
			],
			'Europe/Oslo',
			now,
			214
		);
		expect(groups.map((g) => [g.label, g.sub, g.items.length])).toEqual([
			['Today', 'Tue 29 Sep · in-game day 214', 2],
			['Yesterday', 'Mon 28 Sep', 1],
			['Sun 27 Sep', '', 1]
		]);
	});
});

describe('todayRows', () => {
	const t: TodaySessions = {
		timeZone: 'Europe/Oslo',
		dayStart: '2026-09-28T22:00:00Z',
		dayEnd: '2026-09-29T22:00:00Z',
		now: '2026-09-29T12:44:00Z',
		players: [
			{ id: '1', name: 'Johnny', online: true, spans: [{ since: '2026-09-29T06:10:00Z', until: '2026-09-29T07:40:00Z' }, { since: '2026-09-29T11:32:00Z' }] },
			{ id: '2', name: 'Alina', online: false, spans: [{ since: '2026-09-28T22:00:00Z', until: '2026-09-28T23:30:00Z' }] }
		]
	};
	test('bars on the 00–24 axis; the open one runs to now', () => {
		const { rows, nowPct, nowClock } = todayRows(t, new Date('2026-09-29T12:44:00Z'));
		expect(nowClock).toBe('14:44');
		expect(nowPct).toBeCloseTo((14 + 44 / 60) / 24 * 100, 5);
		const [first, open] = rows[0].bars;
		expect(first.left).toBeCloseTo((8 + 10 / 60) / 24 * 100, 5);
		expect(first.width).toBeCloseTo(1.5 / 24 * 100, 5);
		expect(first.live).toBe(false);
		expect(first.title).toBe('Johnny · 08:10–09:40');
		expect(open.live).toBe(true);
		expect(open.title).toBe('Johnny · 13:32–now');
		expect(open.left + open.width).toBeCloseTo(nowPct, 5);
		expect(rows[1].bars[0].left).toBe(0);
	});
	test('the people filter applies', () => {
		expect(todayRows(t, new Date('2026-09-29T12:44:00Z'), ['2']).rows.map((r) => r.name)).toEqual(['Alina']);
	});
});

describe('todayRowLabel (review: a real text equivalent for "who was on today")', () => {
	test('a closed session, and an open one naming the current clock rather than the bare word "now"', () => {
		const t: TodaySessions = {
			timeZone: 'Europe/Oslo',
			dayStart: '2026-09-28T22:00:00Z',
			dayEnd: '2026-09-29T22:00:00Z',
			now: '2026-09-29T12:44:00Z',
			players: [
				{ id: '1', name: 'Ragnar', online: false, spans: [{ since: '2026-09-29T11:05:00Z', until: '2026-09-29T12:40:00Z' }] },
				{
					id: '2',
					name: 'Alina',
					online: true,
					spans: [
						{ since: '2026-09-28T23:00:00Z', until: '2026-09-29T01:30:00Z' },
						{ since: '2026-09-29T11:32:00Z' }
					]
				}
			]
		};
		const { rows } = todayRows(t, new Date('2026-09-29T12:44:00Z'));
		expect(todayRowLabel(rows[0])).toBe('Ragnar: 13:05–14:40');
		expect(todayRowLabel(rows[1])).toBe('Alina: 01:00–03:30, 13:32–14:44 (online now)');
	});
});

describe('loadedDays', () => {
	const page = (from: string, timeZone = 'UTC') => ({ from, timeZone });
	test('the default window: three days, today included', () => {
		expect(loadedDays([page('2026-09-27T00:00:00Z')], new Date('2026-09-29T10:00:00Z'))).toBe(3);
	});
	test('counts from the oldest page, however wide a refresh made page 0', () => {
		// Page 0 pinned at 27 Sep and grown to 6 days; an earlier page from 24 Sep.
		const pages = [page('2026-09-27T00:00:00Z'), page('2026-09-24T00:00:00Z')];
		expect(loadedDays(pages, new Date('2026-10-02T10:00:00Z'))).toBe(9);
	});
	test("days are the server zone's: 27 Sep 00:00 in Oslo is 26 Sep 22:00 UTC", () => {
		expect(loadedDays([page('2026-09-26T22:00:00Z', 'Europe/Oslo')], new Date('2026-09-29T21:30:00Z'))).toBe(3);
	});
	test('nothing loaded yet: the default three', () => {
		expect(loadedDays([], new Date('2026-09-29T10:00:00Z'))).toBe(3);
	});
});
