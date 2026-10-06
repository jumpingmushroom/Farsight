// The activity timeline's model (Plan 7, design `:837-908`): categories and
// their chips, each event's text, icon, tone and source line, the people
// and category filters, day groups in the server's zone, and the "who was
// on today" bars. The side panel's short list (derive.ts activityRows)
// uses the same text, icons and filters.

import { fmtCode } from './format';
import type { Activity, Category, TodaySessions } from './types';
import { dayKey, prevDayKey, zClock, zWeekday } from './zoned';

export type ActivityIcon = 'log-in' | 'log-out' | 'power' | 'map' | 'skull' | 'flame' | 'home' | 'portal' | 'paw-print' | 'alert';
export type Tone = 'ember' | 'neutral' | 'sage' | 'cold';

/** The chips, in the design's order. */
export const CATEGORIES: { key: Category; label: string; icon: ActivityIcon }[] = [
	{ key: 'session', label: 'Joins & leaves', icon: 'log-in' },
	{ key: 'death', label: 'Deaths', icon: 'skull' },
	{ key: 'boss', label: 'Bosses', icon: 'flame' },
	{ key: 'build', label: 'Building', icon: 'home' },
	{ key: 'portal', label: 'Portals', icon: 'portal' },
	{ key: 'tame', label: 'Tames', icon: 'paw-print' },
	{ key: 'event', label: 'Raids & events', icon: 'alert' },
	{ key: 'server', label: 'Server', icon: 'power' }
];

/**
 * The game's start message for a random event, by its name in the log
 * ("Random event set:<name>"). Names not listed show as they are.
 */
export const RAID_MESSAGES: Record<string, string> = {
	army_eikthyr: 'Eikthyr rallies the creatures of the forest',
	army_theelder: 'The forest is moving',
	army_bonemass: 'A foul smell from the swamp',
	army_moder: 'A cold wind blows from the mountains',
	army_goblin: 'The horde is attacking',
	foresttrolls: 'The ground is shaking',
	skeletons: 'Skeleton surprise',
	wolves: 'You are being hunted',
	surtlings: 'There’s a smell of sulfur in the air',
	bats: 'You stirred the cauldron'
};

const pad2 = (n: number) => String(n).padStart(2, '0');

/** A session length for "left after …": "3h 05m", "40m". */
export function leftAfter(sec: number): string {
	const h = Math.floor(sec / 3600);
	const m = Math.floor((sec % 3600) / 60);
	return h > 0 ? `${h}h ${pad2(m)}m` : `${m}m`;
}

/** "near a sunken crypt in the Swamp", "in the Swamp", or "". */
export function place(e: Pick<Activity, 'near' | 'biome'>): string {
	if (!e.biome) return e.near ? `near ${e.near}` : '';
	return e.near ? `near ${e.near} in the ${e.biome}` : `in the ${e.biome}`;
}

export function eventText(e: Activity): string {
	switch (e.type) {
		case 'player_join':
			return `${e.name} joined`;
		case 'player_leave':
			return e.seconds ? `${e.name} left after ${leftAfter(e.seconds)}` : `${e.name} left`;
		case 'server_ready':
			return e.version ? `Server is up · version ${e.version}` : 'Server is up';
		case 'server_stopped':
			return 'Server stopped';
		case 'server_starting':
		case 'server_boot':
			return 'Server starting';
		case 'world_saved':
			return 'Autosave finished · map updated';
		case 'join_code':
			return `New join code ${e.code ? fmtCode(e.code) : ''}`;
		case 'player_death':
			return `${e.name} died`;
		case 'event_raid':
			return `Raid: ${RAID_MESSAGES[e.raid ?? ''] ?? e.raid ?? 'unknown event'}`;
		case 'world_tombstone': {
			const where = place(e);
			return `New tombstone: ${e.owner || 'someone'}${where ? `, ${where}` : ''}`;
		}
		case 'world_portal':
			return e.paired ? `New portal “${e.tag}”, paired with “${e.tag}”` : `New portal “${e.tag}”, not paired with anything yet`;
		case 'world_portal_paired':
			return `Portal “${e.tag}” now paired with “${e.tag}”`;
		case 'world_tame':
			return `New tame: ${e.name} (${e.species})`;
		case 'world_base_new':
			return e.biome ? `New base: ${e.name} (${e.biome})` : `New base: ${e.name}`;
		case 'world_base_grew':
			return `${e.name} grew by ${e.grew} pieces`;
		case 'world_boss':
			return `${e.boss} defeated`;
		default:
			return e.type;
	}
}

export function eventIcon(e: Activity): ActivityIcon {
	if (e.type === 'player_join') return 'log-in';
	if (e.type === 'player_leave') return 'log-out';
	if (e.type === 'world_saved') return 'map';
	return CATEGORIES.find((c) => c.key === e.category)?.icon ?? 'power';
}

/** The icon disc's tone (design `tone`): joins and deaths and raids ember, bosses and tames sage, portals cold. */
export function eventTone(e: Activity): Tone {
	switch (e.category) {
		case 'session':
			return e.type === 'player_join' ? 'ember' : 'neutral';
		case 'death':
		case 'event':
			return 'ember';
		case 'boss':
		case 'tame':
			return 'sage';
		case 'portal':
			return 'cold';
		default:
			return 'neutral';
	}
}

/** "Server log", or "World save · 14:20" (the save's time in the server's zone). */
export function sourceText(e: Activity, tz: string): string {
	return e.source === 'save' ? `World save · ${zClock(e.at, tz)}` : 'Server log';
}

/** Whether `e` passes the filters: its category is on, and it concerns a chosen player (or nobody in particular). */
export function passes(e: Activity, off: readonly Category[], people: readonly string[]): boolean {
	if (off.includes(e.category)) return false;
	if (people.length === 0 || e.who.length === 0) return true;
	return e.who.some((id) => people.includes(id));
}

/** Per-chip counts: the events passing the people filter, by category. */
export function chipCounts(events: readonly Activity[], people: readonly string[]): Record<Category, number> {
	const out = Object.fromEntries(CATEGORIES.map((c) => [c.key, 0])) as Record<Category, number>;
	for (const e of events) if (passes(e, [], people)) out[e.category]++;
	return out;
}

/**
 * Collapses a run of consecutive `world_saved` entries (newest-first) to
 * its newest, the same rule the server applies within one page — needed
 * here only at a page seam: the server only collapses within a page, so
 * the oldest `world_saved` of one page and the newest of the next (loaded
 * by "Show earlier") can still sit next to each other uncollapsed.
 */
export function collapseAutosaves(events: readonly Activity[]): Activity[] {
	const out: Activity[] = [];
	for (const e of events) {
		const prev = out[out.length - 1];
		if (e.type === 'world_saved' && prev?.type === 'world_saved') continue;
		out.push(e);
	}
	return out;
}

export interface DayGroup {
	key: string;
	label: string;
	sub: string;
	items: Activity[];
}

/**
 * Groups events (newest first) by local day in `tz`: "Today · Tue 29 Sep ·
 * in-game day 214" (the day only when `gameDay` is known), "Yesterday ·
 * Mon 28 Sep", then "Sun 27 Sep".
 */
export function groupByDay(events: readonly Activity[], tz: string, now: Date, gameDay?: number): DayGroup[] {
	const today = dayKey(now, tz);
	const yesterday = prevDayKey(today);
	const groups: DayGroup[] = [];
	for (const e of events) {
		const key = dayKey(e.at, tz);
		let g = groups[groups.length - 1];
		if (!g || g.key !== key) {
			const date = zWeekday(e.at, tz);
			if (key === today) g = { key, label: 'Today', sub: gameDay ? `${date} · in-game day ${gameDay}` : date, items: [] };
			else if (key === yesterday) g = { key, label: 'Yesterday', sub: date, items: [] };
			else g = { key, label: date, sub: '', items: [] };
			groups.push(g);
		}
		g.items.push(e);
	}
	return groups;
}

export interface TodayBar {
	/** Left edge and width, % of the 00–24 axis. */
	left: number;
	width: number;
	live: boolean;
	title: string;
	/** "08:10–09:40", or "13:32–14:44" (the current clock) for a live bar: the accessible range, without the player's name. */
	range: string;
}

export interface TodayRow {
	id: string;
	name: string;
	bars: TodayBar[];
}

/** "Who was on today": a row per player, a bar per session on the day's axis; open sessions run to `now`. */
export function todayRows(t: TodaySessions, now: Date, people: readonly string[] = []): { rows: TodayRow[]; nowPct: number; nowClock: string } {
	const d0 = new Date(t.dayStart).getTime();
	const span = new Date(t.dayEnd).getTime() - d0;
	const pct = (ms: number) => Math.max(0, Math.min(100, ((ms - d0) / span) * 100));
	const nowMs = Math.min(now.getTime(), new Date(t.dayEnd).getTime());
	const nowClock = zClock(new Date(nowMs), t.timeZone);
	const rows = t.players
		.filter((p) => people.length === 0 || people.includes(p.id))
		.map((p) => ({
			id: p.id,
			name: p.name,
			bars: p.spans.map((s) => {
				const a = new Date(s.since).getTime();
				const b = s.until ? new Date(s.until).getTime() : nowMs;
				const sinceClock = zClock(s.since, t.timeZone);
				const untilClock = s.until ? zClock(s.until, t.timeZone) : nowClock;
				return {
					left: pct(a),
					width: Math.max(0, pct(b) - pct(a)),
					live: !s.until,
					title: `${p.name} · ${sinceClock}–${s.until ? untilClock : 'now'}`,
					range: `${sinceClock}–${untilClock}`
				};
			})
		}));
	return { rows, nowPct: pct(nowMs), nowClock };
}

/** An accessible summary of one "who was on today" row: "Ragnar: 13:05–14:40 (online now)". */
export function todayRowLabel(r: TodayRow): string {
	return `${r.name}: ${r.bars.map((b) => (b.live ? `${b.range} (online now)` : b.range)).join(', ')}`;
}
