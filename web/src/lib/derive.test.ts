// `@types/node` isn't an allowed dependency, so `process` is declared
// ambiently rather than typed via node's globals.
declare const process: { env: Record<string, string | undefined> };
process.env.TZ = 'UTC';

import { describe, expect, test } from 'vitest';
import type { Activity, Card, Marker, OnlinePlayer, RecentSession, ServerSummary, WorldCard } from './types';
import {
	activityRows,
	countText,
	isOfflineLike,
	joinCodeView,
	mapPill,
	mapFilter,
	mapState,
	mapView,
	offlineTitle,
	nearestAltar,
	playersEmpty,
	nextBoss,
	nextEtaMin,
	recentList,
	statusView,
	switcherSub,
	worldRules
} from './derive';

const NOW = new Date('2026-09-30T12:00:00Z');

function iso(offsetFromNowSec: number): string {
	return new Date(NOW.getTime() + offsetFromNowSec * 1000).toISOString();
}

function makeCard(overrides: Partial<Card> = {}): Card {
	return {
		id: 'example',
		name: 'Example Vikings',
		crossplay: true,
		maxPlayers: 10,
		status: 'online',
		players: 0,
		online: [],
		recent: [],
		activity: [],
		tiles: { state: 'complete', done: 1365, total: 1365, key: 'abc' },
		...overrides
	};
}

function makeWorld(overrides: Partial<WorldCard> = {}): WorldCard {
	return {
		name: 'Example',
		seedName: 'Example',
		day: 214,
		bosses: [
			{ key: 'eikthyr', name: 'Eikthyr', defeated: true },
			{ key: 'elder', name: 'The Elder', defeated: true },
			{ key: 'bonemass', name: 'Bonemass', defeated: true },
			{ key: 'moder', name: 'Moder', defeated: true },
			{ key: 'yagluth', name: 'Yagluth', defeated: false },
			{ key: 'queen', name: 'The Queen', defeated: false },
			{ key: 'fader', name: 'Fader', defeated: false }
		],
		modifiers: {},
		flags: [],
		exploredPct: 12.4,
		savedAt: iso(0),
		readAt: iso(0),
		...overrides
	};
}

describe('statusView', () => {
	test('online', () => {
		const v = statusView('online');
		expect(v.label).toBe('Online');
		expect(v.tone).toBe('accent');
		expect(v.ring).toBe(true);
	});
	test('starting', () => {
		const v = statusView('starting');
		expect(v.label).toBe('Starting');
		expect(v.tone).toBe('cold');
		expect(v.ring).toBe(false);
	});
	test('restarting', () => {
		const v = statusView('restarting');
		expect(v.label).toBe('Restarting');
		expect(v.tone).toBe('cold');
	});
	test('offline', () => {
		const v = statusView('offline');
		expect(v.label).toBe('Offline');
		expect(v.tone).toBe('muted');
		expect(v.ring).toBe(false);
	});
	test('unknown', () => {
		const v = statusView('unknown');
		expect(v.label).toBe('Unknown');
		expect(v.tone).toBe('muted');
	});
});

describe('recentList', () => {
	test('dedupes by name (newest until), excludes online, caps at 5, newest first', () => {
		const online: OnlinePlayer[] = [{ name: 'Halvor', platform: 'steam', platformId: '1', since: iso(-100) }];
		const recent: RecentSession[] = [
			{ name: 'Alina', platform: 'steam', platformId: '2', since: iso(-1000), until: iso(-500), seconds: 500 },
			{ name: 'Alina', platform: 'steam', platformId: '2', since: iso(-3000), until: iso(-2500), seconds: 500 },
			{ name: 'Halvor', platform: 'steam', platformId: '1', since: iso(-4000), until: iso(-3500), seconds: 500 },
			{ name: 'Bjorn', platform: 'steam', platformId: '3', since: iso(-600), until: iso(-400), seconds: 200 },
			{ name: 'Johnny', platform: 'steam', platformId: '4', since: iso(-900), until: iso(-800), seconds: 100 },
			{ name: 'Frøya', platform: 'steam', platformId: '5', since: iso(-1200), until: iso(-1100), seconds: 100 },
			{ name: 'Sten', platform: 'steam', platformId: '6', since: iso(-1500), until: iso(-1400), seconds: 100 }
		];
		const card = makeCard({ online, recent });
		const list = recentList(card);
		expect(list.length).toBe(5);
		expect(list.find((r) => r.name === 'Halvor')).toBeUndefined();
		const alina = list.find((r) => r.name === 'Alina');
		expect(alina?.until).toBe(iso(-500));
		for (let i = 1; i < list.length; i++) {
			expect(new Date(list[i - 1].until).getTime()).toBeGreaterThanOrEqual(new Date(list[i].until).getTime());
		}
	});
});

describe('activityRows', () => {
	test('keeps only the newest world_saved, maps copy, applies the limit', () => {
		const activity: Activity[] = [
			{ type: 'player_join', at: iso(-10), name: 'Halvor' },
			{ type: 'world_saved', at: iso(-20) },
			{ type: 'world_saved', at: iso(-40) },
			{ type: 'player_leave', at: iso(-60), name: 'Bjorn' },
			{ type: 'server_ready', at: iso(-80), version: '1.0.16' },
			{ type: 'server_stopped', at: iso(-100) },
			{ type: 'server_starting', at: iso(-120) },
			{ type: 'join_code', at: iso(-140), code: '318742' }
		];
		const card = makeCard({ activity });
		const rows = activityRows(card, 8);
		expect(rows.filter((r) => r.text.includes('map updated')).length).toBe(1);
		expect(rows.find((r) => r.text === 'Halvor joined')?.icon).toBe('log-in');
		expect(rows.find((r) => r.text === 'Halvor joined')?.tone).toBe('ember');
		expect(rows.find((r) => r.text === 'Bjorn left')?.icon).toBe('log-out');
		expect(rows.find((r) => r.text === 'Server is up · version 1.0.16')).toBeTruthy();
		expect(rows.find((r) => r.text === 'Server stopped')).toBeTruthy();
		expect(rows.find((r) => r.text === 'Server starting')).toBeTruthy();
		expect(rows.find((r) => r.text === 'New join code 318 742')).toBeTruthy();
	});

	test('applies the limit', () => {
		const activity: Activity[] = Array.from({ length: 10 }, (_, i) => ({
			type: 'player_join',
			at: iso(-i * 10),
			name: `P${i}`
		}));
		const card = makeCard({ activity });
		expect(activityRows(card, 3).length).toBe(3);
	});

	test('server_ready without a version', () => {
		const card = makeCard({ activity: [{ type: 'server_ready', at: iso(-5) }] });
		expect(activityRows(card, 8)[0].text).toBe('Server is up');
	});
});

describe('mapPill', () => {
	test('interval 1200s, saved 12 min ago', () => {
		const world = makeWorld({ savedAt: iso(-720), saveIntervalSec: 1200 });
		const pill = mapPill(world, NOW);
		expect(pill.age).toBe('12 min ago');
		expect(pill.next).toBe('~8 min');
		expect(pill.progress).toBeCloseTo(0.6);
	});
	test('saved 25 min ago clamps progress to 1 and shows "any moment"', () => {
		const world = makeWorld({ savedAt: iso(-1500), saveIntervalSec: 1200 });
		const pill = mapPill(world, NOW);
		expect(pill.next).toBe('any moment');
		expect(pill.progress).toBe(1);
	});
	test('no interval: no next, no progress', () => {
		const world = makeWorld({ savedAt: iso(-720), saveIntervalSec: undefined });
		const pill = mapPill(world, NOW);
		expect(pill.next).toBeUndefined();
		expect(pill.progress).toBeUndefined();
	});
});

describe('mapState', () => {
	test('no world -> waiting', () => {
		const card = makeCard({ world: undefined, tiles: { state: 'none', done: 0, total: 0 } });
		expect(mapState(card, NOW)).toEqual({ kind: 'waiting' });
	});
	test('tiles rendering 389/1365 -> charting pct 28', () => {
		const card = makeCard({
			world: makeWorld(),
			tiles: { state: 'rendering', done: 389, total: 1365 }
		});
		const st = mapState(card, NOW);
		expect(st.kind).toBe('charting');
		if (st.kind === 'charting') {
			expect(st.pct).toBe(28);
			expect(st.done).toBe(389);
			expect(st.total).toBe(1365);
		}
	});
	test('refused -> refused', () => {
		const card = makeCard({ world: makeWorld(), tiles: { state: 'refused', done: 0, total: 1365 } });
		expect(mapState(card, NOW)).toEqual({ kind: 'refused' });
	});
	test('complete and fresh -> ready not stale', () => {
		const card = makeCard({
			world: makeWorld({ savedAt: iso(-60), saveIntervalSec: 1200 }),
			tiles: { state: 'complete', done: 1365, total: 1365, key: 'k' }
		});
		expect(mapState(card, NOW)).toEqual({ kind: 'ready', stale: false });
	});
	test('complete and 3h41m old with a 1200s interval -> stale', () => {
		const card = makeCard({
			world: makeWorld({ savedAt: iso(-(3 * 3600 + 41 * 60)), saveIntervalSec: 1200 }),
			tiles: { state: 'complete', done: 1365, total: 1365, key: 'k' }
		});
		const st = mapState(card, NOW);
		expect(st).toMatchObject({ kind: 'ready', stale: true, ageText: '3 h 41 min', usualMin: 20 });
	});
	test('no interval, 2.5h old -> stale, no usualMin', () => {
		const card = makeCard({
			world: makeWorld({ savedAt: iso(-2.5 * 3600), saveIntervalSec: undefined }),
			tiles: { state: 'complete', done: 1365, total: 1365, key: 'k' }
		});
		const st = mapState(card, NOW);
		expect(st.kind).toBe('ready');
		if (st.kind === 'ready' && st.stale) {
			expect(st.usualMin).toBeUndefined();
		} else {
			throw new Error('expected stale');
		}
	});
	test('tiles none with a world present -> refused', () => {
		const card = makeCard({ world: makeWorld(), tiles: { state: 'none', done: 0, total: 0 } });
		expect(mapState(card, NOW)).toEqual({ kind: 'refused' });
	});
});

describe('nextEtaMin', () => {
	test('two samples 60s apart, delta-done 100, 865 remaining -> 9', () => {
		const samples = [
			{ at: 0, done: 100 },
			{ at: 60_000, done: 200 }
		];
		expect(nextEtaMin(samples, 200 + 865)).toBe(9);
	});
	test('fewer than 2 samples -> undefined', () => {
		expect(nextEtaMin([{ at: 0, done: 1 }], 100)).toBeUndefined();
	});
});

describe('isOfflineLike', () => {
	test.each([
		['offline', true],
		['unknown', true],
		['online', false],
		['starting', false],
		['restarting', false]
	] as const)('%s -> %s', (s, want) => {
		expect(isOfflineLike(s)).toBe(want);
	});
});

describe('joinCodeView', () => {
	test('online with a code -> live', () => {
		const card = makeCard({ status: 'online', joinCode: '318742', joinCodeAt: '2026-09-30T06:00:00Z' });
		const view = joinCodeView(card, NOW);
		expect(view.state).toBe('live');
		expect(view.code).toBe('318742');
		expect(view.note).toBe(
			'Issued at today’s 06:00 restart. A new code is issued every time the server restarts, and this page updates by itself.'
		);
	});
	test('restarting -> restarting', () => {
		const card = makeCard({ status: 'restarting', joinCode: undefined });
		const view = joinCodeView(card, NOW);
		expect(view.state).toBe('restarting');
	});
	test('offline -> offline, "Last seen 03:12"', () => {
		const card = makeCard({ status: 'offline', lastHeartbeat: '2026-09-30T03:12:00Z' });
		const view = joinCodeView(card, NOW);
		expect(view.state).toBe('offline');
		expect(view.status).toBe('Last seen 03:12');
	});
	test('offline with no lastHeartbeat -> status "Offline"', () => {
		const card = makeCard({ status: 'offline', lastHeartbeat: undefined });
		const view = joinCodeView(card, NOW);
		expect(view.status).toBe('Offline');
	});
	test('online without a code -> restarting look, status "Waiting for code"', () => {
		const card = makeCard({ status: 'online', joinCode: undefined });
		const view = joinCodeView(card, NOW);
		expect(view.state).toBe('restarting');
		expect(view.status).toBe('Waiting for code');
	});
	test('crossplay:false -> state none', () => {
		const card = makeCard({ crossplay: false, status: 'online', joinCode: '318742' });
		const view = joinCodeView(card, NOW);
		expect(view.state).toBe('none');
		expect(view.note).toBe('Steam server: join by address.');
	});
});

describe('worldRules', () => {
	test('5 changed modifiers with the §3.10 labels; preset Custom', () => {
		const world = makeWorld({
			modifiers: { combat: 'hard', deathpenalty: 'casual', resources: 'more', raids: 'less', portals: 'casual' },
			flags: []
		});
		const { preset, tiles } = worldRules(world);
		expect(preset).toBe('Custom · based on Normal');
		expect(tiles).toHaveLength(8);
		const byKey = Object.fromEntries(tiles.map((t) => [t.key, t]));
		expect(byKey['Combat']).toMatchObject({ value: 'Hard', changed: true });
		expect(byKey['Death penalty']).toMatchObject({ value: 'Casual · keep gear', changed: true });
		expect(byKey['Resources']).toMatchObject({ value: '1.5×', changed: true });
		expect(byKey['Raids']).toMatchObject({ value: 'Less often', changed: true });
		expect(byKey['Portals']).toMatchObject({ value: 'Ores & metals allowed', changed: true });
		expect(byKey['Map']).toMatchObject({ value: 'Enabled', changed: false });
		expect(byKey['Passive enemies']).toMatchObject({ value: 'Off', changed: false });
		expect(byKey['No build cost']).toMatchObject({ value: 'Off', changed: false });
	});

	test('empty modifiers and flags -> all baseline, preset Normal', () => {
		const world = makeWorld({ modifiers: {}, flags: [] });
		const { preset, tiles } = worldRules(world);
		expect(preset).toBe('Normal');
		expect(tiles.every((t) => !t.changed)).toBe(true);
	});

	test('flags:["nomap"] -> Map "Disabled", changed', () => {
		const world = makeWorld({ modifiers: {}, flags: ['nomap'] });
		const { tiles } = worldRules(world);
		const map = tiles.find((t) => t.key === 'Map');
		expect(map).toMatchObject({ value: 'Disabled', changed: true });
	});

	test('unknown modifier value is title-cased and counts as changed', () => {
		const world = makeWorld({ modifiers: { combat: 'superhard' }, flags: [] });
		const { tiles } = worldRules(world);
		const combat = tiles.find((t) => t.key === 'Combat');
		expect(combat).toMatchObject({ value: 'Superhard', changed: true });
	});

	test('a present key carrying its Normal value does not count as changed', () => {
		const world = makeWorld({ modifiers: { combat: 'normal' }, flags: [] });
		const { preset, tiles } = worldRules(world);
		const combat = tiles.find((t) => t.key === 'Combat');
		expect(combat).toMatchObject({ value: 'Normal', changed: false });
		expect(preset).toBe('Normal');
	});

	test('resources carrying its Normal value ("1×") does not count as changed', () => {
		const world = makeWorld({ modifiers: { resources: 'normal' }, flags: [] });
		const { tiles } = worldRules(world);
		const resources = tiles.find((t) => t.key === 'Resources');
		expect(resources).toMatchObject({ value: '1×', changed: false });
	});
});

describe('nextBoss', () => {
	test('returns the first undefeated boss with its biome', () => {
		const world = makeWorld();
		expect(nextBoss(world)).toEqual({ name: 'Yagluth', biome: 'Plains' });
	});
	test('undefined when all defeated', () => {
		const world = makeWorld({ bosses: makeWorld().bosses.map((b) => ({ ...b, defeated: true })) });
		expect(nextBoss(world)).toBeUndefined();
	});
});

describe('switcherSub', () => {
	test('current server with a card', () => {
		const s: ServerSummary = { id: 'example', name: 'Example Vikings', status: 'online', players: 4, maxPlayers: 10 };
		const card = makeCard({ id: 'example', world: makeWorld({ day: 214 }) });
		expect(switcherSub(s, card)).toBe('Day 214 · 4 of 7 bosses');
	});
	test('another server -> status word', () => {
		const s: ServerSummary = { id: 'ashen', name: 'Ashen Crew', status: 'offline', players: 0, maxPlayers: 10 };
		expect(switcherSub(s)).toBe('Offline');
	});
});

describe('countText', () => {
	test('offline -> "—/10"', () => {
		expect(countText(0, 10, 'offline')).toBe('—/10');
	});
	test('online shows the actual count', () => {
		expect(countText(4, 10, 'online')).toBe('4/10');
	});
	test('mobile pill separator', () => {
		expect(countText(4, 10, 'online', ' / ')).toBe('4 / 10');
	});
});

describe('nearestAltar', () => {
	const altar = (id: string, label: string, x: number, z: number): Marker => ({ id, kind: 'boss_altar', label, x, y: 0, z });
	test('matches the label and picks the one nearest the centre', () => {
		const locs: Marker[] = [
			altar('a', 'Yagluth', 3000, 0),
			{ id: 't', kind: 'trader', label: 'Yagluth', x: 1, y: 0, z: 1 },
			altar('b', 'Yagluth', -100, 200),
			altar('c', 'Moder', 0, 10)
		];
		expect(nearestAltar(locs, 'Yagluth')?.id).toBe('b');
	});
	test('none', () => {
		expect(nearestAltar([altar('c', 'Moder', 0, 10)], 'Yagluth')).toBeUndefined();
	});
});

describe('playersEmpty', () => {
	const leave = (name: string, untilSec: number): RecentSession => ({
		name,
		platform: 'steam',
		platformId: '1',
		since: iso(untilSec - 600),
		until: iso(untilSec),
		seconds: 600
	});
	test('offline with a world names the final save', () => {
		const e = playersEmpty(makeCard({ status: 'offline', world: makeWorld({ savedAt: '2026-09-30T03:10:00Z' }) }), NOW);
		expect(e).toEqual({
			title: 'Server is resting',
			body: 'Nobody can join until it’s back. The map shows the final save before shutdown (03:10).'
		});
	});
	test('unknown without a world', () => {
		expect(playersEmpty(makeCard({ status: 'unknown' }), NOW)?.body).toBe('Nobody can join until it’s back.');
	});
	test('someone online: no empty state', () => {
		const online: OnlinePlayer[] = [{ name: 'Astrid', platform: 'steam', platformId: '1', since: iso(-60) }];
		expect(playersEmpty(makeCard({ online, players: 1 }), NOW)).toBeUndefined();
	});
	test('quiet with a recent leave', () => {
		const e = playersEmpty(makeCard({ recent: [leave('Ulf', -7200), leave('Bjorn', -9000)] }), NOW);
		expect(e).toEqual({
			title: 'The longhouse is quiet',
			body: 'Nobody’s online right now. Ulf was the last to leave, 2 h ago. The fire’s still warm.'
		});
	});
	test('quiet, last leave long ago drops the warm fire', () => {
		const e = playersEmpty(makeCard({ recent: [leave('Ulf', -8 * 3600)] }), NOW);
		expect(e?.body).toBe('Nobody’s online right now. Ulf was the last to leave, 8 h ago.');
	});
	test('quiet with minutes and no recent', () => {
		expect(playersEmpty(makeCard({ recent: [leave('Ulf', -300)] }), NOW)?.body).toBe(
			'Nobody’s online right now. Ulf was the last to leave, 5 min ago. The fire’s still warm.'
		);
		expect(playersEmpty(makeCard(), NOW)?.body).toBe('Nobody’s online right now.');
	});
});

describe('fmt stale age under an hour (mapState ageText)', () => {
	test('5 min interval, 12 min old -> "12 min" (no "0 h")', () => {
		const card = makeCard({
			world: makeWorld({ savedAt: iso(-12 * 60), saveIntervalSec: 300 }),
			tiles: { state: 'complete', done: 1365, total: 1365, key: 'k' }
		});
		expect(mapState(card, NOW)).toMatchObject({ kind: 'ready', stale: true, ageText: '12 min', usualMin: 5 });
	});
});

describe('mapFilter', () => {
	test('nothing -> empty string', () => {
		expect(mapFilter(true, undefined)).toBe('');
	});
	test('biomes off -> greyscale', () => {
		expect(mapFilter(false, undefined)).toBe('grayscale(1) contrast(.85) brightness(1.08)');
	});
	test('offline and stale tones', () => {
		expect(mapFilter(true, 'offline')).toBe('grayscale(.55) brightness(.8)');
		expect(mapFilter(true, 'stale')).toBe('sepia(.35) brightness(.9)');
	});
	test('biomes off combines by concatenation', () => {
		expect(mapFilter(false, 'offline')).toBe('grayscale(1) contrast(.85) brightness(1.08) grayscale(.55) brightness(.8)');
		expect(mapFilter(false, 'stale')).toBe('grayscale(1) contrast(.85) brightness(1.08) sepia(.35) brightness(.9)');
	});
});

describe('offlineTitle', () => {
	test('with a heartbeat today -> day ref and ago', () => {
		const card = makeCard({ status: 'offline', lastHeartbeat: '2026-09-30T01:00:00Z' });
		expect(offlineTitle(card, NOW)).toBe('Server offline · last seen online today 01:00 (11 h ago)');
	});
	test('with a heartbeat yesterday', () => {
		const card = makeCard({ status: 'offline', lastHeartbeat: '2026-09-29T20:30:00Z' });
		expect(offlineTitle(card, NOW)).toBe('Server offline · last seen online yesterday 20:30 (15 h ago)');
	});
	test('without a heartbeat -> plain', () => {
		expect(offlineTitle(makeCard({ status: 'offline' }), NOW)).toBe('Server offline');
	});
});

describe('mapView (state precedence, ruling 2)', () => {
	const fresh = () => makeWorld({ savedAt: iso(-5 * 60), saveIntervalSec: 1200 });
	const old = () => makeWorld({ savedAt: iso(-4 * 3600), saveIntervalSec: 1200 });
	const complete = { state: 'complete' as const, done: 1365, total: 1365, key: 'k' };

	test('no world -> waiting, markers off, no filter', () => {
		const v = mapView(makeCard({ world: undefined, status: 'offline', tiles: { state: 'none', done: 0, total: 0 } }), NOW, [], true);
		expect(v).toEqual({ overlay: { kind: 'waiting' }, markersOn: false, filter: '', pinClass: '' });
	});
	test('no world with tiles complete -> waiting', () => {
		const v = mapView(makeCard({ world: undefined, tiles: complete }), NOW, [], true);
		expect(v).toEqual({ overlay: { kind: 'waiting' }, markersOn: false, filter: '', pinClass: '' });
	});
	test('charting beats offline and stale; markers off, no filter even with biomes off', () => {
		const card = makeCard({ status: 'offline', world: old(), tiles: { state: 'rendering', done: 389, total: 1365 } });
		const samples = [
			{ at: 0, done: 189 },
			{ at: 60_000, done: 389 }
		];
		const v = mapView(card, NOW, samples, false);
		expect(v).toEqual({
			overlay: { kind: 'charting', pct: 28, done: 389, total: 1365, etaMin: 5 },
			markersOn: false,
			filter: '',
			pinClass: ''
		});
	});
	test('queued also charts, eta undefined with one sample', () => {
		const card = makeCard({ world: fresh(), tiles: { state: 'queued', done: 0, total: 1365 } });
		const v = mapView(card, NOW, [{ at: 0, done: 0 }], true);
		expect(v.overlay).toEqual({ kind: 'charting', pct: 0, done: 0, total: 1365, etaMin: undefined });
		expect(v.markersOn).toBe(false);
	});
	test('refused beats offline for the banner; markers stay on; offline treatment still applies', () => {
		const card = makeCard({ status: 'offline', world: fresh(), tiles: { state: 'refused', done: 0, total: 1365 } });
		const v = mapView(card, NOW, [], true);
		expect(v.overlay).toEqual({
			kind: 'banner',
			tone: 'refused',
			title: 'Can’t draw this world’s map yet',
			body: 'This world was made by a newer game version than Farsight knows. Markers and the online list still work.'
		});
		expect(v.markersOn).toBe(true);
		expect(v.filter).toBe('grayscale(.55) brightness(.8)');
		expect(v.pinClass).toBe('fs-pins-offline');
	});
	test('tiles none with a world -> refused banner', () => {
		const v = mapView(makeCard({ world: fresh(), tiles: { state: 'none', done: 0, total: 0 } }), NOW, [], true);
		expect(v.overlay).toMatchObject({ kind: 'banner', tone: 'refused' });
	});
	test('offline -> offline banner, filter and pin class', () => {
		const card = makeCard({ status: 'offline', lastHeartbeat: '2026-09-30T01:00:00Z', world: fresh(), tiles: complete });
		expect(mapView(card, NOW, [], true)).toEqual({
			overlay: {
				kind: 'banner',
				tone: 'offline',
				title: 'Server offline · last seen online today 01:00 (11 h ago)',
				body: 'The map is still here to browse. We’ll switch back to live as soon as it answers.'
			},
			markersOn: true,
			filter: 'grayscale(.55) brightness(.8)',
			pinClass: 'fs-pins-offline'
		});
	});
	test('offline and stale -> offline wins banner, filter and pins', () => {
		const card = makeCard({ status: 'offline', world: old(), tiles: complete });
		const v = mapView(card, NOW, [], true);
		expect(v.overlay).toMatchObject({ kind: 'banner', tone: 'offline' });
		expect(v.filter).toBe('grayscale(.55) brightness(.8)');
		expect(v.pinClass).toBe('fs-pins-offline');
	});
	test('unknown status is not the offline banner', () => {
		const v = mapView(makeCard({ status: 'unknown', world: fresh(), tiles: complete }), NOW, [], true);
		expect(v.overlay).toEqual({ kind: 'pill' });
	});
	test('stale -> stale banner with the usual-interval clause', () => {
		const v = mapView(makeCard({ world: old(), tiles: complete }), NOW, [], true);
		expect(v).toEqual({
			overlay: {
				kind: 'banner',
				tone: 'stale',
				title: 'Map last updated 4 h 0 min ago · saves usually every ~20 min',
				body: 'Autosave may be failing, or the world file isn’t being read. Markers may be out of date; the online list is still live.'
			},
			markersOn: true,
			filter: 'sepia(.35) brightness(.9)',
			pinClass: 'fs-pins-stale'
		});
	});
	test('stale without an interval drops the clause', () => {
		const card = makeCard({ world: makeWorld({ savedAt: iso(-2.5 * 3600), saveIntervalSec: undefined }), tiles: complete });
		expect(mapView(card, NOW, [], true).overlay).toMatchObject({ title: 'Map last updated 2 h 30 min ago' });
	});
	test('fresh and online -> pill; biomes off greys the map', () => {
		const card = makeCard({ world: fresh(), tiles: complete });
		expect(mapView(card, NOW, [], true)).toEqual({ overlay: { kind: 'pill' }, markersOn: true, filter: '', pinClass: '' });
		expect(mapView(card, NOW, [], false).filter).toBe('grayscale(1) contrast(.85) brightness(1.08)');
	});
	test('stale with biomes off concatenates', () => {
		const v = mapView(makeCard({ world: old(), tiles: complete }), NOW, [], false);
		expect(v.filter).toBe('grayscale(1) contrast(.85) brightness(1.08) sepia(.35) brightness(.9)');
	});
});
