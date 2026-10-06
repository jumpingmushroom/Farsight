// API contract types — mirrors docs/superpowers/specs/2026-09-29-farsight-atlas-mvp-design.md
// (search "#### API contract") exactly. All times are RFC 3339 UTC strings;
// absent optional fields are omitted by the server.

export type Status = 'online' | 'starting' | 'restarting' | 'offline' | 'unknown';
export type TileState = 'none' | 'queued' | 'rendering' | 'complete' | 'refused';

export interface ServerSummary {
	id: string;
	name: string;
	status: Status;
	players: number;
	maxPlayers: number;
}

export interface OnlinePlayer {
	name: string;
	platform: string;
	platformId: string;
	since: string;
}

export interface RecentSession {
	name: string;
	platform: string;
	platformId: string;
	since: string;
	until: string;
	seconds: number;
}

/** An activity category: the timeline's chips (Plan 7). */
export type Category = 'session' | 'death' | 'boss' | 'build' | 'portal' | 'tame' | 'event' | 'server';

/**
 * One activity entry, in the card's `activity` and the timeline alike.
 * Log events carry their exact time, world-save events (`source: 'save'`)
 * the save's time. Only the fields its type uses are set.
 */
export interface Activity {
	id: string;
	type: string;
	category: Category;
	source: 'log' | 'save';
	at: string;
	/** Platform IDs of the players it concerns (the people filter). */
	who: string[];
	/** The player (log events), or the tame or base (world events). */
	name?: string;
	platform?: string;
	platformId?: string;
	code?: string;
	players?: number;
	seconds?: number;
	version?: string;
	/** event_raid: the game's event name, e.g. "army_theelder". */
	raid?: string;
	owner?: string;
	tag?: string;
	paired?: boolean;
	species?: string;
	pieces?: number;
	grew?: number;
	boss?: string;
	biome?: string;
	/** A known location near the place: "a sunken crypt". */
	near?: string;
	/** Set only for a place in explored ground ("Show on map →"). */
	x?: number;
	z?: number;
}

export interface Boss {
	key: string;
	name: string;
	defeated: boolean;
}

export interface WorldCard {
	name: string;
	seedName: string;
	day: number;
	bosses: Boss[];
	modifiers: Record<string, string>;
	flags: string[];
	exploredPct: number;
	savedAt: string;
	readAt: string;
	saveIntervalSec?: number;
}

export interface Tiles {
	state: TileState;
	done: number;
	total: number;
	key?: string;
}

/** `Card.clock`: the server's estimate of the game's world clock now (Plan 9). */
export interface Clock {
	/** The estimate at `at`. */
	netTime: number;
	/** The server's time of the estimate; the client ticks from the card's receipt (`AppState.cardAt`) instead. */
	at: string;
	/** The client ticks `netTime` forward 1 s/s while true. */
	running: boolean;
	/** The anchor the estimate was built from. */
	source: 'save' | 'sleep';
}

/** One of `Weather.periods`: the weather by biome for the 666 s period starting at `start` (a netTime). */
export interface WeatherPeriod {
	start: number;
	byBiome: Record<string, string>;
}

/** `Card.weather`: the current weather period and the next two, by explored biome (Plan 9). */
export interface Weather {
	periodSec: number;
	/** The current period and the next two. */
	periods: WeatherPeriod[];
	/** Explored biomes, in legend order. */
	biomes: string[];
	/** The pill's biome: under the base with the most pieces, else Meadows. */
	home: string;
}

export interface Card {
	id: string;
	name: string;
	crossplay: boolean;
	address?: string;
	discordHint?: string;
	maxPlayers: number;
	status: Status;
	version?: string;
	networkVersion?: number;
	upSince?: string;
	lastHeartbeat?: string;
	players: number;
	joinCode?: string;
	joinCodeAt?: string;
	/** The server's log time zone (IANA): local days and clock times in profiles and the timeline. */
	timeZone?: string;
	online: OnlinePlayer[];
	recent: RecentSession[];
	activity: Activity[];
	world?: WorldCard;
	tiles: Tiles;
	clock?: Clock;
	weather?: Weather;
}

export interface Marker {
	id: string;
	kind: string;
	x: number;
	y: number;
	z: number;
	label?: string;
	owner?: string;
	species?: string;
	type?: string;
	pair?: string;
	/** A tame's namer: their platform user ID ("Steam_…"). */
	namer?: string;
	/** A classified location's layer (boss_altar/trader/dungeon/landmark only); absent on older snapshots. */
	group?: 'landmarks' | 'dungeons' | 'minor';
	/** A location the game has planned but not yet built; the server already drops unique sites' unplaced candidates. */
	unplaced?: boolean;
}

export interface Builder {
	id: number;
	name?: string;
	pieces: number;
}

export interface Base {
	id: string;
	name: string;
	x: number;
	z: number;
	radius: number;
	pieces: number;
	builders: Builder[];
}

/** The explored mask as the API sends it (see explored.ts). */
export interface Explored {
	source: 'tables' | 'zones';
	cell: number;
	size: number;
	/** base64 of gzip of the bitset. */
	bits: string;
}

export interface SnapshotView {
	savedAt: string;
	/** Names the fog tiles drawn from this snapshot's mask. */
	fogKey: string;
	explored: Explored;
	/**
	 * `explored`, decoded by getSnapshot (client-side only; absent if it
	 * can't be). Only the cursor readout uses it: the server has already
	 * filtered markers, locations and bases to it.
	 */
	mask?: Uint8Array;
	markers: Marker[];
	locations: Marker[];
	bases: Base[];
	players: { id: number; name: string }[];
}

/** GET /api/servers/{id}/players/{player} (Plan 7). Times are RFC 3339 UTC. */
export interface Profile {
	/** The platform ID (the sessions' platformId). */
	id: string;
	name: string;
	platform: string;
	timeZone: string;
	online: boolean;
	/** The open session's start, when online. */
	since?: string;
	/** The latest session's end, when offline. */
	lastSeen?: string;
	firstSeen: string;
	trackedSince: string;
	weekSeconds: number;
	allSeconds: number;
	sessions: number;
	/** Seven local days, oldest first, today last. */
	days: { date: string; seconds: number }[];
	beds: { count: number; near: string[] };
	bases: { id: string; name: string; pieces: number; biome: string; x: number; z: number }[];
	portals: { id: string; tag: string; paired: boolean; x: number; z: number }[];
	tames: { id: string; name: string; species: string; x: number; z: number }[];
	deaths: {
		spotted: number;
		week: number;
		tombstones: { id: string; biome: string; firstSeen: string; x: number; z: number }[];
	};
}

/** GET /api/servers/{id}/activity: events in [from, until), newest first. */
export interface ActivityPage {
	timeZone: string;
	from: string;
	until: string;
	/** When tracking began; absent before any event. */
	earliest?: string;
	events: Activity[];
	counts: Record<Category, number>;
	/** Every player seen: online first, then most recently seen. */
	people: { id: string; name: string; online: boolean }[];
}

/** GET /api/servers/{id}/sessions/today: today's sessions in the server's zone. */
export interface TodaySessions {
	timeZone: string;
	dayStart: string;
	dayEnd: string;
	now: string;
	players: { id: string; name: string; online: boolean; spans: { since: string; until?: string }[] }[];
}
