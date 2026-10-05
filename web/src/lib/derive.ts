// Derived view state: status chips, recent/activity lists, the map-updated
// pill and charting/stale states, join-code states and world rules. Built
// against DESIGN-NOTES §3.3, §3.5, §3.7, §3.8, §3.9, §3.10, §3.11, §3.19,
// §3.22 and the plan's rulings on design ambiguities.

import { fmtClock, fmtDayRef, fmtMapAge } from './format';
import { eventIcon, eventText, eventTone, passes, type ActivityIcon, type Tone } from './timeline';
import type { Activity, Card, Category, Marker, ServerSummary, Status, WorldCard } from './types';

const MONTH_ABBR = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

function sameLocalDay(a: Date, b: Date): boolean {
	return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}

function isPreviousLocalDay(a: Date, b: Date): boolean {
	const prev = new Date(b.getFullYear(), b.getMonth(), b.getDate() - 1);
	return a.getFullYear() === prev.getFullYear() && a.getMonth() === prev.getMonth() && a.getDate() === prev.getDate();
}

// --- Status (§3.3) --------------------------------------------------------

export interface StatusView {
	label: string;
	dot: string;
	ring: boolean;
	tone: 'accent' | 'cold' | 'muted';
}

const STATUS_VIEWS: Record<Status, StatusView> = {
	online: { label: 'Online', dot: 'var(--color-accent)', ring: true, tone: 'accent' },
	starting: { label: 'Starting', dot: 'var(--cold)', ring: false, tone: 'cold' },
	restarting: { label: 'Restarting', dot: 'var(--cold)', ring: false, tone: 'cold' },
	offline: { label: 'Offline', dot: 'var(--color-neutral-500)', ring: false, tone: 'muted' },
	unknown: { label: 'Unknown', dot: 'var(--color-neutral-500)', ring: false, tone: 'muted' }
};

export function statusView(s: Status): StatusView {
	return STATUS_VIEWS[s];
}

export function sessionSeconds(since: string, now: Date): number {
	return Math.max(0, (now.getTime() - new Date(since).getTime()) / 1000);
}

export function isOfflineLike(s: Status): boolean {
	return s === 'offline' || s === 'unknown';
}

// --- Players (§3.7) --------------------------------------------------------

export function recentList(card: Card): { name: string; until: string; platformId: string }[] {
	const onlineNames = new Set(card.online.map((p) => p.name));
	const newestByName = new Map<string, { until: string; platformId: string }>();
	for (const r of card.recent) {
		if (onlineNames.has(r.name)) continue;
		const prev = newestByName.get(r.name);
		if (!prev || new Date(r.until).getTime() > new Date(prev.until).getTime()) {
			newestByName.set(r.name, { until: r.until, platformId: r.platformId });
		}
	}
	return Array.from(newestByName, ([name, r]) => ({ name, ...r }))
		.sort((a, b) => new Date(b.until).getTime() - new Date(a.until).getTime())
		.slice(0, 5);
}

// --- Activity (§3.8; Plan 7) ------------------------------------------------

export interface ActivityRow {
	icon: ActivityIcon;
	tone: Tone;
	text: string;
	at: string;
	source: Activity['source'];
}

/**
 * The side panel's short list: the card's activity (newest first) through
 * the timeline's filters, only the newest `world_saved` row kept, capped at
 * `limit`. Text, icon and tone are the timeline's.
 */
export function activityRows(
	card: Card,
	limit: number,
	off: readonly Category[] = [],
	people: readonly string[] = []
): ActivityRow[] {
	const rows: ActivityRow[] = [];
	let sawWorldSaved = false;
	for (const a of card.activity) {
		if (rows.length >= limit) break;
		if (!passes(a, off, people)) continue;
		if (a.type === 'world_saved') {
			if (sawWorldSaved) continue;
			sawWorldSaved = true;
		}
		rows.push({ icon: eventIcon(a), tone: eventTone(a), text: eventText(a), at: a.at, source: a.source });
	}
	return rows;
}

// --- Map-updated pill (§3.11) ----------------------------------------------

export interface MapPill {
	age: string;
	next?: string;
	progress?: number;
	savedClock: string;
}

export function mapPill(world: WorldCard, now: Date): MapPill {
	const savedAt = new Date(world.savedAt);
	const elapsedSec = Math.max(0, (now.getTime() - savedAt.getTime()) / 1000);
	const age = fmtMapAge(elapsedSec / 60);
	const savedClock = fmtClock(world.savedAt);
	if (world.saveIntervalSec === undefined) {
		return { age, savedClock };
	}
	const remainingSec = world.saveIntervalSec - elapsedSec;
	const m = Math.ceil(remainingSec / 60);
	const next = m <= 0 ? 'any moment' : `~${m} min`;
	const progress = Math.min(1, elapsedSec / world.saveIntervalSec);
	return { age, next, progress, savedClock };
}

// --- Map / charting / stale state (§3.22) -----------------------------------

export type MapState =
	| { kind: 'waiting' }
	| { kind: 'charting'; pct: number; done: number; total: number; etaMin?: number }
	| { kind: 'refused' }
	| { kind: 'ready'; stale: false }
	| { kind: 'ready'; stale: true; ageText: string; usualMin?: number };

export interface TileSample {
	at: number;
	done: number;
}

function fmtDurationHM(sec: number): string {
	const totalMin = Math.floor(sec / 60);
	const h = Math.floor(totalMin / 60);
	const m = totalMin % 60;
	return h === 0 ? `${m} min` : `${h} h ${m} min`;
}

const DEFAULT_STALE_THRESHOLD_SEC = 2 * 3600;

export function mapState(card: Card, now: Date, samples: TileSample[] = []): MapState {
	if (!card.world) {
		return { kind: 'waiting' };
	}
	if (card.tiles.state === 'refused' || card.tiles.state === 'none') {
		return { kind: 'refused' };
	}
	if (card.tiles.state === 'queued' || card.tiles.state === 'rendering') {
		const pct = card.tiles.total > 0 ? Math.round((card.tiles.done / card.tiles.total) * 100) : 0;
		const etaMin = nextEtaMin(samples, card.tiles.total);
		return { kind: 'charting', pct, done: card.tiles.done, total: card.tiles.total, etaMin };
	}
	const world = card.world;
	const elapsedSec = Math.max(0, (now.getTime() - new Date(world.savedAt).getTime()) / 1000);
	const thresholdSec = world.saveIntervalSec !== undefined ? world.saveIntervalSec * 2 : DEFAULT_STALE_THRESHOLD_SEC;
	if (elapsedSec <= thresholdSec) {
		return { kind: 'ready', stale: false };
	}
	const usualMin = world.saveIntervalSec !== undefined ? Math.round(world.saveIntervalSec / 60) : undefined;
	return { kind: 'ready', stale: true, ageText: fmtDurationHM(elapsedSec), usualMin };
}

export function nextEtaMin(samples: TileSample[], total: number): number | undefined {
	if (samples.length < 2) return undefined;
	const first = samples[0];
	const last = samples[samples.length - 1];
	const dtSec = (last.at - first.at) / 1000;
	const ddone = last.done - first.done;
	if (dtSec <= 0 || ddone <= 0) return undefined;
	const rate = ddone / dtSec; // tiles per second
	const remaining = total - last.done;
	if (remaining <= 0) return 0;
	return Math.round(remaining / rate / 60);
}

// --- Map overlays, filters and pin treatment (§3.13, §3.22; ruling 2) --------

export type BannerTone = 'offline' | 'stale' | 'refused';

export type Overlay =
	| { kind: 'waiting' }
	| { kind: 'charting'; pct: number; done: number; total: number; etaMin?: number }
	| { kind: 'banner'; tone: BannerTone; title: string; body: string }
	| { kind: 'pill' };

export interface MapView {
	/** What sits in the map-updated pill slot (or, for waiting/charting, centred in the map area). */
	overlay: Overlay;
	/** Markers (and search) are hidden while waiting for a save and while charting. */
	markersOn: boolean;
	/** The tile-pane CSS filter; '' for none. */
	filter: string;
	/** A class for Leaflet's marker pane that sets the pin opacity. */
	pinClass: '' | 'fs-pins-offline' | 'fs-pins-stale';
}

const BIOMES_OFF_FILTER = 'grayscale(1) contrast(.85) brightness(1.08)';
const TONE_FILTER: Record<'offline' | 'stale', string> = {
	offline: 'grayscale(.55) brightness(.8)',
	stale: 'sepia(.35) brightness(.9)'
};

/** The tile-pane filter: the biomes-off greyscale, then the offline or stale treatment. */
export function mapFilter(biomes: boolean, tone: 'offline' | 'stale' | undefined): string {
	return [biomes ? '' : BIOMES_OFF_FILTER, tone ? TONE_FILTER[tone] : ''].filter(Boolean).join(' ');
}

/** "Server offline · last seen online today 03:12 (11 h ago)", or "Server offline" without a heartbeat. */
export function offlineTitle(card: Card, now: Date): string {
	if (!card.lastHeartbeat) return 'Server offline';
	const ago = agoShort(sessionSeconds(card.lastHeartbeat, now));
	return `Server offline · last seen online ${fmtDayRef(card.lastHeartbeat, now)} (${ago})`;
}

const OFFLINE_BODY = 'The map is still here to browse. We’ll switch back to live as soon as it answers.';
const STALE_BODY =
	'Autosave may be failing, or the world file isn’t being read. Markers may be out of date; the online list is still live.';
const REFUSED_TITLE = 'Can’t draw this world’s map yet';
const REFUSED_BODY =
	'This world was made by a newer game version than Farsight knows. Markers and the online list still work.';

/**
 * The map's state treatment, highest precedence first: waiting (no save),
 * charting, refused/none banner, offline banner, stale banner, else the
 * map-updated pill. Offline beats stale for the banner, filter and pins;
 * the refused banner keeps the offline treatment when the server is down.
 */
export function mapView(card: Card, now: Date, samples: TileSample[], biomes: boolean): MapView {
	const st = mapState(card, now, samples);
	if (st.kind === 'waiting' || st.kind === 'charting') {
		return { overlay: st, markersOn: false, filter: '', pinClass: '' };
	}
	const offline = card.status === 'offline';
	const stale = st.kind === 'ready' && st.stale;
	const tone = offline ? 'offline' : stale ? 'stale' : undefined;
	const base = {
		markersOn: true,
		filter: mapFilter(biomes, tone),
		pinClass: tone ? (`fs-pins-${tone}` as const) : ('' as const)
	};
	if (st.kind === 'refused') {
		return { overlay: { kind: 'banner', tone: 'refused', title: REFUSED_TITLE, body: REFUSED_BODY }, ...base };
	}
	if (offline) {
		return { overlay: { kind: 'banner', tone: 'offline', title: offlineTitle(card, now), body: OFFLINE_BODY }, ...base };
	}
	if (st.stale) {
		const usual = st.usualMin !== undefined ? ` · saves usually every ~${st.usualMin} min` : '';
		const title = `Map last updated ${st.ageText} ago${usual}`;
		return { overlay: { kind: 'banner', tone: 'stale', title, body: STALE_BODY }, ...base };
	}
	return { overlay: { kind: 'pill' }, ...base };
}

// --- Join code (§3.19) -------------------------------------------------------

export interface JoinCodeView {
	state: 'live' | 'restarting' | 'offline' | 'none';
	code?: string;
	note: string;
	status: string;
}

const RESTARTING_NOTE =
	'The old code is hidden as soon as the log shows a shutdown, so nobody copies a dead one. A new code usually appears within a minute.';
const OFFLINE_NOTE =
	'There’s no active session to join. The address stays listed for PC players and will work once the server is back.';

function dayPossessive(iso: string, now: Date): string {
	const d = new Date(iso);
	if (sameLocalDay(d, now)) return 'today’s';
	if (isPreviousLocalDay(d, now)) return 'yesterday’s';
	return `${d.getDate()} ${MONTH_ABBR[d.getMonth()]}`;
}

export function joinCodeView(card: Card, now: Date): JoinCodeView {
	if (!card.crossplay) {
		return { state: 'none', note: 'Steam server: join by address.', status: '' };
	}
	if (card.status === 'online' && card.joinCode) {
		const note =
			`Issued at ${dayPossessive(card.joinCodeAt ?? now.toISOString(), now)} ${fmtClock(card.joinCodeAt ?? now.toISOString())} restart. ` +
			'A new code is issued every time the server restarts, and this page updates by itself.';
		return { state: 'live', code: card.joinCode, note, status: 'From server log' };
	}
	if (card.status === 'online' && !card.joinCode) {
		return { state: 'restarting', note: RESTARTING_NOTE, status: 'Waiting for code' };
	}
	if (card.status === 'starting' || card.status === 'restarting') {
		return { state: 'restarting', note: RESTARTING_NOTE, status: 'Waiting for new code' };
	}
	const status = card.lastHeartbeat ? `Last seen ${fmtClock(card.lastHeartbeat)}` : 'Offline';
	return { state: 'offline', note: OFFLINE_NOTE, status };
}

// --- World rules (§3.10) -----------------------------------------------------

export interface RuleTile {
	key: string;
	value: string;
	changed: boolean;
}

function titleCase(v: string): string {
	return v.charAt(0).toUpperCase() + v.slice(1).toLowerCase();
}

function modTile(label: string, raw: string | undefined, labels: Record<string, string>, normal: string): RuleTile {
	if (raw === undefined) return { key: label, value: normal, changed: false };
	// The literal raw token "normal" (present because a world explicitly
	// carries its default value rather than omitting the key) always maps to
	// this tile's own baseline label, so it correctly compares as unchanged
	// below — it isn't looked up in `labels`, which only holds non-default
	// values.
	const value = raw === 'normal' ? normal : (labels[raw] ?? titleCase(raw));
	return { key: label, value, changed: value !== normal };
}

function flagTile(label: string, flagName: string, flags: string[], normal: string, changedValue: string): RuleTile {
	const present = flags.includes(flagName);
	return { key: label, value: present ? changedValue : normal, changed: present };
}

const COMBAT_LABELS: Record<string, string> = {
	veryeasy: 'Very easy',
	easy: 'Easy',
	hard: 'Hard',
	veryhard: 'Very hard'
};
// veryeasy/easy/hard have no explicit §3.10 copy for death penalty (only
// casual and hardcore do), so they fall back to generic title-casing below.
const DEATHPENALTY_LABELS: Record<string, string> = {
	casual: 'Casual · keep gear',
	hardcore: 'Hardcore'
};
const RESOURCES_LABELS: Record<string, string> = {
	muchless: '0.5×',
	less: '0.75×',
	more: '1.5×',
	muchmore: '2×',
	most: '3×'
};
const RAIDS_LABELS: Record<string, string> = {
	none: 'None',
	muchless: 'Less often',
	less: 'Less often',
	more: 'More often',
	muchmore: 'More often'
};
const PORTALS_LABELS: Record<string, string> = {
	casual: 'Ores & metals allowed',
	hard: 'No boss portals',
	veryhard: 'No portals'
};

export function worldRules(world: WorldCard): { preset: string; tiles: RuleTile[] } {
	const m = world.modifiers ?? {};
	const f = world.flags ?? [];
	const tiles: RuleTile[] = [
		modTile('Combat', m.combat, COMBAT_LABELS, 'Normal'),
		modTile('Death penalty', m.deathpenalty, DEATHPENALTY_LABELS, 'Normal'),
		modTile('Resources', m.resources, RESOURCES_LABELS, '1×'),
		modTile('Raids', m.raids, RAIDS_LABELS, 'Normal'),
		modTile('Portals', m.portals, PORTALS_LABELS, 'Normal'),
		flagTile('Map', 'nomap', f, 'Enabled', 'Disabled'),
		flagTile('Passive enemies', 'passivemobs', f, 'Off', 'On'),
		flagTile('No build cost', 'nobuildcost', f, 'Off', 'On')
	];
	const preset = tiles.some((t) => t.changed) ? 'Custom · based on Normal' : 'Normal';
	return { preset, tiles };
}

// --- Bosses (§3.9) -----------------------------------------------------------

export const BOSS_BIOME: Record<string, string> = {
	Eikthyr: 'Meadows',
	'The Elder': 'Black Forest',
	Bonemass: 'Swamp',
	Moder: 'Mountains',
	Yagluth: 'Plains',
	'The Queen': 'Mistlands',
	Fader: 'Ashlands',
	'Kall Fimbulbringer': 'Deep North'
};

export function nextBoss(world: WorldCard): { name: string; biome: string } | undefined {
	const b = world.bosses.find((x) => !x.defeated);
	if (!b) return undefined;
	return { name: b.name, biome: BOSS_BIOME[b.name] ?? '' };
}

const BOSS_SHORT_NAMES: Record<string, string> = {
	'The Elder': 'Elder',
	'The Queen': 'Queen',
	'Kall Fimbulbringer': 'Kall'
};

export function bossShort(name: string): string {
	return BOSS_SHORT_NAMES[name] ?? name;
}

// --- Switcher / counts (§3.5) -------------------------------------------------

export function switcherSub(s: ServerSummary, card?: Card): string {
	if (card?.world) {
		const defeated = card.world.bosses.filter((b) => b.defeated).length;
		return `Day ${card.world.day} · ${defeated} of ${card.world.bosses.length} bosses`;
	}
	return statusView(s.status).label;
}

export function countText(players: number, max: number, status: Status, sep: '/' | ' / ' = '/'): string {
	const p = isOfflineLike(status) ? '—' : String(players);
	return `${p}${sep}${max}`;
}

// --- Next-up altar (§3.9) -----------------------------------------------------

/**
 * The `boss_altar` location for `boss` (matched on its label), picking the one
 * nearest the world centre when there are several (plan ruling "Next up").
 */
export function nearestAltar(locations: Marker[], boss: string): Marker | undefined {
	let best: Marker | undefined;
	let bestD = Infinity;
	for (const m of locations) {
		if (m.kind !== 'boss_altar' || m.label !== boss) continue;
		const d = m.x * m.x + m.z * m.z;
		if (d < bestD) {
			best = m;
			bestD = d;
		}
	}
	return best;
}

// --- Players-tab empty states (§3.22, §6.4) ---------------------------------

export interface EmptyState {
	title: string;
	body: string;
}

const WARM_FIRE_SEC = 6 * 3600;

/** "just now" | "12 min ago" | "2 h ago" | "3 d ago" */
function agoShort(sec: number): string {
	const min = Math.floor(sec / 60);
	if (min < 1) return 'just now';
	if (min < 60) return `${min} min ago`;
	const h = Math.floor(min / 60);
	if (h < 24) return `${h} h ago`;
	return `${Math.floor(h / 24)} d ago`;
}

/**
 * The Players-tab empty state: "Server is resting" when offline (or unknown),
 * "The longhouse is quiet" when nobody is online, otherwise undefined.
 */
export function playersEmpty(card: Card, now: Date): EmptyState | undefined {
	if (isOfflineLike(card.status)) {
		const save = card.world ? ` The map shows the final save before shutdown (${fmtClock(card.world.savedAt)}).` : '';
		return { title: 'Server is resting', body: `Nobody can join until it’s back.${save}` };
	}
	if (card.online.length > 0) return undefined;
	let body = 'Nobody’s online right now.';
	const last = recentList(card)[0];
	if (last) {
		const sec = sessionSeconds(last.until, now);
		body += ` ${last.name} was the last to leave, ${agoShort(sec)}.`;
		if (sec <= WARM_FIRE_SEC) body += ' The fire’s still warm.';
	}
	return { title: 'The longhouse is quiet', body };
}
