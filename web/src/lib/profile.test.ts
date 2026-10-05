import { describe, expect, test } from 'vitest';
import { dayBars, fmtPlaytime, hoursLabel, profileView } from './profile';
import type { Profile } from './types';

const NOW = new Date('2026-10-05T12:00:00Z');

function makeProfile(over: Partial<Profile> = {}): Profile {
	return {
		id: '111',
		name: 'astrid',
		platform: 'Steam',
		timeZone: 'Europe/Oslo',
		online: true,
		since: '2026-10-05T10:48:00Z',
		firstSeen: '2026-09-30T18:00:00Z',
		trackedSince: '2026-09-30T06:00:00Z',
		weekSeconds: 51600,
		allSeconds: 763200,
		sessions: 96,
		days: [
			{ date: '2026-09-29', seconds: 9000 },
			{ date: '2026-09-30', seconds: 0 },
			{ date: '2026-10-01', seconds: 10800 },
			{ date: '2026-10-02', seconds: 16200 },
			{ date: '2026-10-03', seconds: 3600 },
			{ date: '2026-10-04', seconds: 60 },
			{ date: '2026-10-05', seconds: 9720 }
		],
		beds: { count: 2, near: ['Longhouse', 'Plains'] },
		bases: [{ id: 'base-1', name: 'Longhouse', pieces: 2184, biome: 'Meadows', x: 1, z: 2 }],
		portals: [{ id: 'portal-1', tag: 'home', paired: true, x: 1, z: 2 }],
		tames: [],
		deaths: {
			spotted: 41,
			week: 3,
			tombstones: [{ id: 'tombstone-1', biome: 'Swamp', firstSeen: '2026-10-05T12:20:00Z', x: 3, z: 4 }]
		},
		...over
	};
}

describe('fmtPlaytime / hoursLabel', () => {
	test('formats', () => {
		expect(fmtPlaytime(0)).toBe('0m');
		expect(fmtPlaytime(38 * 60)).toBe('38m');
		expect(fmtPlaytime(9 * 3600 + 5 * 60)).toBe('9h 05m');
		expect(fmtPlaytime(14 * 3600 + 20 * 60)).toBe('14h 20m');
		expect(fmtPlaytime(212 * 3600 + 59 * 60, true)).toBe('212h');
		expect(fmtPlaytime(6 * 3600 + 10 * 60, true)).toBe('6h 10m');
		expect(hoursLabel(0)).toBe('');
		expect(hoursLabel(60)).toBe('<0.1');
		expect(hoursLabel(9000)).toBe('2.5');
		expect(hoursLabel(10800)).toBe('3');
	});
});

describe('dayBars', () => {
	test('weekday initials, today last, the busiest day at 78%', () => {
		const bars = dayBars(makeProfile().days);
		expect(bars.map((b) => b.d).join('')).toBe('TWTFSSM');
		expect(bars[3].pct).toBe(78);
		expect(bars[1]).toEqual({ d: 'W', label: '', pct: 0, today: false });
		expect(bars[6].today).toBe(true);
		expect(bars[6].label).toBe('2.7');
	});
	test('a quiet week stays low (max is at least an hour)', () => {
		const bars = dayBars([{ date: '2026-10-05', seconds: 1800 }]);
		expect(bars[0].pct).toBe(39);
	});
});

describe('profileView', () => {
	test('online', () => {
		const v = profileView(makeProfile(), NOW);
		expect(v.initial).toBe('A');
		expect(v.status).toBe('Online now · 1h 12m');
		expect(v.stats).toEqual([
			{ k: 'This week', v: '14h 20m' },
			{ k: 'All time', v: '212h' },
			{ k: 'Sessions', v: '96' }
		]);
		expect(v.first).toBe('30 Sep 2026');
		expect(v.last).toBe('Online now');
		expect(v.beds).toBe('2 · Longhouse, Plains');
		expect(v.tracked).toBe('All time and first seen count from 30 Sep 2026, when tracking began.');
		expect(v.portalCount).toBe('1 portal');
		expect(v.tameCount).toBe('');
		expect(v.deathLine).toBe('41 spotted · 3 this week');
		expect(v.tombs[0].text).toBe('Tombstone in the Swamp · since save 14:20');
		expect(v.bases[0].sub).toBe('2,184 pieces · Meadows');
	});
	test('offline', () => {
		const v = profileView(
			makeProfile({ online: false, since: undefined, lastSeen: '2026-10-05T10:00:00Z', beds: { count: 0, near: [] } }),
			NOW
		);
		expect(v.status).toBe('Last seen 2 h ago');
		expect(v.last).toBe('today 12:00');
		expect(v.beds).toBe('None placed');
	});
	test('an older tombstone names its day', () => {
		const p = makeProfile();
		p.deaths.tombstones[0].firstSeen = '2026-10-03T12:20:00Z';
		expect(profileView(p, NOW).tombs[0].text).toBe('Tombstone in the Swamp · since save 3 Oct 14:20');
	});
	test('bases are listed by piece count, largest first, regardless of server order', () => {
		const p = makeProfile({
			bases: [
				{ id: 'base-1', name: 'Longhouse', pieces: 400, biome: 'Meadows', x: 1, z: 2 },
				{ id: 'base-2', name: 'Watchtower', pieces: 2184, biome: 'Black Forest', x: 3, z: 4 },
				{ id: 'base-3', name: 'Outpost', pieces: 950, biome: 'Plains', x: 5, z: 6 }
			]
		});
		expect(profileView(p, NOW).bases.map((b) => b.id)).toEqual(['base-2', 'base-3', 'base-1']);
	});
});
