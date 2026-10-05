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

export interface Activity {
	type: string;
	at: string;
	name?: string;
	platform?: string;
	code?: string;
	players?: number;
	seconds?: number;
	version?: string;
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
	online: OnlinePlayer[];
	recent: RecentSession[];
	activity: Activity[];
	world?: WorldCard;
	tiles: Tiles;
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
