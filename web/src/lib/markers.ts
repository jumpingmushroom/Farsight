// The map-marker model (DESIGN-NOTES §4, §8): every snapshot marker,
// location and base becomes a MapMarker with its design type, layer, pin
// style (§4.1), card content (§4.2 with the MVP adaptations and plan rulings)
// and search terms (§5.2). Plus the visibility, count and portal-pair helpers
// the map, layers panel and search share, and the pin/cluster HTML strings
// for Leaflet divIcons.

import { BOSS_BIOME } from './derive';
import { isExplored } from './explored';
import { fmtInt, fmtKm, fmtN } from './format';
import { dir8, distance } from './geo';
import { iconSvg, type IconName } from './icons/paths';
import type { Base, Marker, SnapshotView, WorldCard } from './types';

export type { IconName } from './icons/paths';

export type LayerKey = 'biomes' | 'structures' | 'portals' | 'beds' | 'tombstones' | 'tames' | 'signs' | 'locations';

/** §3.13, minus vehicles and wards, in order. */
export const LAYERS: { key: LayerKey; label: string; short: string; icon: IconName; default: boolean }[] = [
	{ key: 'biomes', label: 'Biomes & terrain', short: 'Terrain', icon: 'mountain', default: true },
	{ key: 'structures', label: 'Structures & bases', short: 'Bases', icon: 'home', default: true },
	{ key: 'portals', label: 'Portals', short: 'Portals', icon: 'portal', default: true },
	{ key: 'beds', label: 'Beds', short: 'Beds', icon: 'bed', default: false },
	{ key: 'tombstones', label: 'Tombstones', short: 'Tombstones', icon: 'skull', default: true },
	{ key: 'tames', label: 'Tamed creatures', short: 'Tames', icon: 'paw-print', default: true },
	{ key: 'signs', label: 'Signs', short: 'Signs', icon: 'signpost', default: false },
	{ key: 'locations', label: 'Dungeons & locations', short: 'Locations', icon: 'arch', default: true }
];

export function defaultLayers(): Record<LayerKey, boolean> {
	return Object.fromEntries(LAYERS.map((l) => [l.key, l.default])) as Record<LayerKey, boolean>;
}

/** The Layers badge (§3.13, §5.3): how many of the main layers are on (portal links excluded). */
export function enabledLayerCount(layers: Record<LayerKey, boolean>): number {
	return LAYERS.filter((l) => layers[l.key]).length;
}

/**
 * The layers-panel legend (§3.13): the tile renderer's palette
 * (internal/tiles/tiles.go), in the design's order, plus Unexplored.
 */
export const BIOME_LEGEND: { name: string; hex: string }[] = [
	{ name: 'Ocean', hex: '#2E5478' },
	{ name: 'Meadows', hex: '#A3B25C' },
	{ name: 'Black Forest', hex: '#3E5834' },
	{ name: 'Swamp', hex: '#786246' },
	{ name: 'Mountains', hex: '#E2E6EC' },
	{ name: 'Plains', hex: '#D6BE6E' },
	{ name: 'Mistlands', hex: '#6E6478' },
	{ name: 'Ashlands', hex: '#963C28' },
	{ name: 'Deep North', hex: '#C8D7E6' },
	{ name: 'Unexplored', hex: '#cfbe9c' }
];

export type PinType = 'base' | 'portal' | 'tomb' | 'altar' | 'bed' | 'tame' | 'sign' | 'trader' | 'dungeon';

export interface Pin {
	size: number;
	bg: string;
	border: string;
	icon: IconName;
	iconColor: string;
	iconPx: number;
	ring?: boolean;
}

export type BadgeTone = 'cold' | 'ember' | 'sage' | 'neutral';

export interface CardModel {
	kicker: string;
	title: string;
	icon: IconName;
	/** The icon colour on the disc (the pin's icon colour). */
	iconColor: string;
	discBg: string;
	badges: { text: string; tone: BadgeTone }[];
	facts: { k: string; v: string }[];
	note?: string;
	partnerId?: string;
	where: string;
}

export interface MapMarker {
	id: string;
	type: PinType;
	layer: LayerKey;
	x: number;
	z: number;
	title: string;
	tooltip: string;
	pin: Pin;
	card: CardModel;
	/** Lower-cased search terms; empty for kinds search never returns. */
	terms: string[];
	/** Search kind order within a rank: portal, base, tame, sign, altar, trader. */
	kindOrder: number;
	/** Base name label under the disc. */
	label?: string;
	/** Hidden below this Leaflet zoom (dungeons: 3). */
	minZoom?: number;
}

// Theme-independent marker colours (§2.3).
export const INK = '#26231f';
export const CREAM = '#f5ead8';
export const COLD = '#3d7eab';
export const EMBER = '#c67139';
export const SAGE = '#7a8a5e';

const KICK: Record<PinType, string> = {
	base: 'Base',
	portal: 'Portal',
	tomb: 'Tombstone',
	altar: 'Boss altar',
	bed: 'Bed',
	tame: 'Tamed creature',
	sign: 'Sign',
	trader: 'Trader',
	dungeon: 'Dungeon'
};

const LAYER_OF: Record<PinType, LayerKey> = {
	base: 'structures',
	portal: 'portals',
	tomb: 'tombstones',
	altar: 'locations',
	bed: 'beds',
	tame: 'tames',
	sign: 'signs',
	trader: 'locations',
	dungeon: 'locations'
};

/** Array order (bases, portals, altars, traders, dungeons, tombstones, tames, beds, signs). */
const SORT: Record<PinType, number> = {
	base: 0,
	portal: 1,
	altar: 2,
	trader: 3,
	dungeon: 4,
	tomb: 5,
	tame: 6,
	bed: 7,
	sign: 8
};

/** Search order within a rank (§5.2 ruling); unsearchable kinds last. */
const KIND_ORDER: Record<PinType, number> = {
	portal: 0,
	base: 1,
	tame: 2,
	sign: 3,
	altar: 4,
	trader: 5,
	dungeon: 6,
	tomb: 7,
	bed: 8
};

const SELLS: Record<string, string> = {
	Haldor: 'Gear, upgrades, pocket expansions',
	Hildir: 'Clothing & cosmetics',
	'Bog Witch': 'Ingredients & trinkets'
};

const CAVE_TYPES = new Set(['MountainCave02', 'TrollCave02', 'BearCave', 'Hildir_cave']);

const DUNGEON_NOTE = 'Shown because someone has explored this area. Locations in unexplored areas stay hidden.';
const NEAR_BASE_M = 300;
const DUNGEON_MIN_ZOOM = 3;

function pinFor(type: PinType, icon: IconName, opts: { paired?: boolean; defeated?: boolean } = {}): Pin {
	const make = (size: number, bg: string, border: string, iconColor: string, ring?: boolean): Pin => {
		const p: Pin = { size, bg, border, icon, iconColor, iconPx: Math.round(size * 0.52) };
		if (ring) p.ring = true;
		return p;
	};
	switch (type) {
		case 'base':
			return make(36, CREAM, INK, INK);
		case 'portal':
			return make(28, COLD, CREAM, CREAM, !opts.paired);
		case 'tomb':
			return make(28, EMBER, CREAM, CREAM);
		case 'altar':
			return make(30, opts.defeated ? SAGE : INK, CREAM, CREAM);
		default:
			return make(26, INK, CREAM, CREAM);
	}
}

function lc(s: string): string {
	// A typographic apostrophe searches like a straight one.
	return s.toLowerCase().replace(/[’‘]/g, "'");
}

interface Draft {
	id: string;
	type: PinType;
	x: number;
	z: number;
	title: string;
	icon: IconName;
	pinOpts?: { paired?: boolean; defeated?: boolean };
	badges?: CardModel['badges'];
	facts?: CardModel['facts'];
	note?: string;
	partnerId?: string;
	terms?: string[];
	tooltipTitle?: string;
	label?: string;
	minZoom?: number;
}

function finish(d: Draft): MapMarker {
	const pin = pinFor(d.type, d.icon, d.pinOpts);
	const kicker = KICK[d.type];
	const m: MapMarker = {
		id: d.id,
		type: d.type,
		layer: LAYER_OF[d.type],
		x: d.x,
		z: d.z,
		title: d.title,
		tooltip: `${kicker} · ${d.tooltipTitle ?? d.title}`,
		pin,
		card: {
			kicker,
			title: d.title,
			icon: d.icon,
			iconColor: pin.iconColor,
			discBg: pin.bg,
			badges: d.badges ?? [],
			facts: d.facts ?? [],
			where: `X ${fmtN(d.x)} · Z ${fmtN(d.z)}`
		},
		terms: (d.terms ?? []).filter((t) => t !== ''),
		kindOrder: KIND_ORDER[d.type]
	};
	if (d.note !== undefined) m.card.note = d.note;
	if (d.partnerId !== undefined) m.card.partnerId = d.partnerId;
	if (d.label !== undefined) m.label = d.label;
	if (d.minZoom !== undefined) m.minZoom = d.minZoom;
	return m;
}

function baseTitle(b: Base): string {
	if (b.name) return b.name;
	const top = b.builders[0]?.name;
	return top ? `${top}'s base` : 'Base';
}

function baseMarker(b: Base): MapMarker {
	const title = baseTitle(b);
	const builder = b.builders[0]?.name || 'Unknown builder';
	const others = [...new Set(b.builders.slice(1).map((x) => x.name || 'Unknown builder'))];
	const named = b.builders.map((x) => x.name).filter((n): n is string => !!n);
	const words = [...named, title].join(' ');
	return finish({
		id: b.id,
		type: 'base',
		x: b.x,
		z: b.z,
		title,
		icon: 'home',
		facts: [
			{ k: 'Builder', v: builder },
			{ k: 'Pieces', v: fmtInt(b.pieces) },
			{ k: 'Also built by', v: others.length ? others.join(', ') : '—' }
		],
		note: 'Builder = whoever placed the most pieces here.',
		terms: [...new Set([...named.map(lc), lc(title), ...lc(words).split(/[ ,]+/)])],
		label: title
	});
}

function portalMarker(m: Marker, byId: Map<string, Marker>, tagCount: Map<string, number>): MapMarker {
	const tag = m.label ?? '';
	const facts: CardModel['facts'] = [];
	if (tag) facts.push({ k: 'Tag', v: `“${tag}”` });
	const partner = m.pair ? byId.get(m.pair) : undefined;
	const d: Draft = {
		id: m.id,
		type: 'portal',
		x: m.x,
		z: m.z,
		title: tag || 'Untagged portal',
		icon: 'portal',
		pinOpts: { paired: !!m.pair },
		facts,
		terms: tag ? [lc(tag)] : []
	};
	if (m.pair) {
		d.badges = [{ text: 'Paired', tone: 'cold' }];
		if (partner) {
			const where = `${fmtKm(distance(m, partner))} ${dir8(partner.x - m.x, partner.z - m.z)}`;
			facts.push({ k: 'Partner', v: where });
			d.note = `Leads to the “${tag}” portal ${where}.`;
			d.partnerId = partner.id;
		}
	} else {
		d.badges = [{ text: 'Unpaired', tone: 'ember' }];
		const n = tagCount.get(tag) ?? 1;
		if (!tag) d.note = 'This portal has no tag, so it leads nowhere.';
		else if (n <= 1)
			d.note = `No other portal carries the tag “${tag}”, so this one leads nowhere. Build a partner with the same tag, or retag it.`;
		else d.note = `${n} portals share the tag “${tag}”, so the game can’t pair them.`;
	}
	return finish(d);
}

function nearestBase(x: number, z: number, bases: { x: number; z: number; title: string }[]): string | undefined {
	let best: string | undefined;
	let bestD = NEAR_BASE_M;
	for (const b of bases) {
		const d = Math.hypot(b.x - x, b.z - z);
		if (d <= bestD) {
			bestD = d;
			best = b.title;
		}
	}
	return best;
}

function markerFor(
	m: Marker,
	ctx: {
		byId: Map<string, Marker>;
		tagCount: Map<string, number>;
		bases: { x: number; z: number; title: string }[];
		defeated: Set<string>;
	}
): MapMarker | undefined {
	switch (m.kind) {
		case 'portal':
			return portalMarker(m, ctx.byId, ctx.tagCount);
		case 'bed': {
			const owner = m.owner ?? '';
			return finish({
				id: m.id,
				type: 'bed',
				x: m.x,
				z: m.z,
				title: owner ? `${owner}'s bed` : 'Bed',
				icon: 'bed',
				facts: [{ k: 'Owner', v: owner || 'Unknown' }],
				note: `Spawn point for ${owner || 'someone'}.`
			});
		}
		case 'tombstone': {
			const owner = m.owner ?? '';
			return finish({
				id: m.id,
				type: 'tomb',
				x: m.x,
				z: m.z,
				title: owner || 'Unknown',
				icon: 'skull',
				facts: [{ k: 'Player', v: owner || 'Unknown' }],
				note: `Where ${owner || 'someone'} died. It stays until they recover their items.`
			});
		}
		case 'tame': {
			const name = m.label ?? '';
			const species = m.species ?? '';
			const facts: CardModel['facts'] = [];
			if (name) facts.push({ k: 'Name', v: name });
			facts.push({ k: 'Species', v: species || 'Unknown' });
			const near = nearestBase(m.x, m.z, ctx.bases);
			if (near) facts.push({ k: 'Near', v: near });
			return finish({
				id: m.id,
				type: 'tame',
				x: m.x,
				z: m.z,
				title: name || species || 'Tamed creature',
				icon: 'paw-print',
				badges: [{ text: 'Tamed', tone: 'sage' }],
				facts,
				terms: [lc(name), lc(species)]
			});
		}
		case 'sign': {
			const text = m.label ?? '';
			return finish({
				id: m.id,
				type: 'sign',
				x: m.x,
				z: m.z,
				title: text || 'Blank sign',
				tooltipTitle: text ? `“${text}”` : 'Blank sign',
				icon: 'signpost',
				note: 'Sign text as of the last save.',
				terms: [lc(text)]
			});
		}
		case 'boss_altar': {
			const boss = m.label || m.type || 'Boss';
			const defeated = ctx.defeated.has(boss);
			const facts: CardModel['facts'] = [{ k: 'Forsaken', v: boss }];
			const biome = BOSS_BIOME[boss];
			if (biome) facts.push({ k: 'Biome', v: biome });
			return finish({
				id: m.id,
				type: 'altar',
				x: m.x,
				z: m.z,
				title: boss,
				icon: 'flame',
				pinOpts: { defeated },
				badges: [defeated ? { text: 'Defeated', tone: 'sage' } : { text: 'Not yet defeated', tone: 'neutral' }],
				facts,
				note: defeated ? 'The world remembers this victory.' : 'Still waiting for a brave crew.',
				terms: [lc(boss)]
			});
		}
		case 'trader': {
			const name = m.label || m.type || 'Trader';
			const sells = SELLS[name];
			return finish({
				id: m.id,
				type: 'trader',
				x: m.x,
				z: m.z,
				title: name,
				icon: 'coins',
				facts: sells ? [{ k: 'Sells', v: sells }] : [],
				note: 'Trader camp. Shown because someone has been here.',
				terms: ['trader', lc(name)]
			});
		}
		case 'dungeon':
			return finish({
				id: m.id,
				type: 'dungeon',
				x: m.x,
				z: m.z,
				title: m.label || m.type || 'Dungeon',
				icon: m.type && CAVE_TYPES.has(m.type) ? 'mountain' : 'arch',
				note: DUNGEON_NOTE,
				minZoom: DUNGEON_MIN_ZOOM
			});
		default:
			return undefined;
	}
}

/**
 * The marker model's identity: `buildMarkers` only depends on the snapshot
 * (one per save) and the defeated bosses, so the shell rebuilds only when
 * this key changes, not on every card poll.
 */
export function markersKey(serverId: string, snap: SnapshotView, world?: WorldCard): string {
	const defeated = (world?.bosses ?? [])
		.filter((b) => b.defeated)
		.map((b) => b.name)
		.sort();
	return `${serverId}|${snap.savedAt}|${world ? 'w' : '-'}|${defeated.join(',')}`;
}

/** Every marker of a snapshot, in the stable kind order used by clustering and search. */
export function buildMarkers(snap: SnapshotView, world?: WorldCard): MapMarker[] {
	const byId = new Map<string, Marker>();
	const tagCount = new Map<string, number>();
	for (const m of snap.markers) {
		byId.set(m.id, m);
		if (m.kind === 'portal') tagCount.set(m.label ?? '', (tagCount.get(m.label ?? '') ?? 0) + 1);
	}
	const bases = snap.bases.map(baseMarker);
	const ctx = {
		byId,
		tagCount,
		bases: bases.map((b) => ({ x: b.x, z: b.z, title: b.title })),
		defeated: new Set((world?.bosses ?? []).filter((b) => b.defeated).map((b) => b.name))
	};
	const out: MapMarker[] = [...bases];
	for (const m of [...snap.markers, ...snap.locations]) {
		const mm = markerFor(m, ctx);
		if (mm) out.push(mm);
	}
	// Array.prototype.sort is stable: input order is kept within a kind.
	return out.sort((a, b) => SORT[a.type] - SORT[b.type]);
}

function fogVisible(m: MapMarker, mask: Uint8Array | undefined, fog: boolean): boolean {
	return !fog || !mask || isExplored(mask, m.x, m.z);
}

/** Markers whose layer is on, that are explored (when fog is on), and not zoom-gated. */
export function visibleMarkers(
	all: MapMarker[],
	layers: Record<LayerKey, boolean>,
	mask: Uint8Array | undefined,
	fog: boolean,
	zoom: number
): MapMarker[] {
	return all.filter(
		(m) => layers[m.layer] && (m.minZoom === undefined || zoom >= m.minZoom) && fogVisible(m, mask, fog)
	);
}

/** Layer-row counts (§8) of what survives fog filtering; biomes is the constant 9. */
export function layerCounts(
	all: MapMarker[],
	mask: Uint8Array | undefined,
	fog: boolean
): Record<LayerKey, number> & { unpaired: number; pairs: number } {
	const c = {
		biomes: 9,
		structures: 0,
		portals: 0,
		beds: 0,
		tombstones: 0,
		tames: 0,
		signs: 0,
		locations: 0,
		unpaired: 0,
		pairs: 0
	};
	const vis = all.filter((m) => fogVisible(m, mask, fog));
	for (const m of vis) {
		c[m.layer]++;
		if (m.type === 'portal' && m.pin.ring) c.unpaired++;
	}
	c.pairs = portalPairs(vis).length;
	return c;
}

/** Portal pairs whose ends are both in `visible`, each pair once (in the first end's order). */
export function portalPairs(visible: MapMarker[]): [MapMarker, MapMarker][] {
	const byId = new Map(visible.map((m) => [m.id, m]));
	const seen = new Set<string>();
	const out: [MapMarker, MapMarker][] = [];
	for (const m of visible) {
		const pid = m.type === 'portal' ? m.card.partnerId : undefined;
		if (!pid || seen.has(m.id)) continue;
		const p = byId.get(pid);
		if (!p) continue;
		seen.add(m.id);
		seen.add(p.id);
		out.push([m, p]);
	}
	return out;
}

// ---- Pin HTML (Leaflet divIcons take strings) ----

const SHADOW = '0 2px 6px rgba(0,0,0,.35)';
const RING = `0 0 0 3px ${EMBER}, 0 0 0 7px rgba(198,113,57,.35)`;
const SELECTED = '0 0 0 4px #7fb2d6, 0 0 0 8px rgba(127,178,214,.35), 0 4px 12px rgba(0,0,0,.4)';

export function escapeHtml(s: string): string {
	return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!);
}

/** A single pin (§4.1): the disc with its icon, and the base label when shown. */
export function pinHtml(m: MapMarker, opts: { selected: boolean; showLabel: boolean }): string {
	const p = m.pin;
	const shadow = opts.selected ? SELECTED : p.ring ? RING : SHADOW;
	const label =
		opts.showLabel && m.label !== undefined ? `<span class="fs-pin-label">${escapeHtml(m.label)}</span>` : '';
	return (
		`<div class="fs-pin-disc" style="width:${p.size}px;height:${p.size}px;background:${p.bg};` +
		`border:2px solid ${p.border};box-shadow:${shadow}">${iconSvg(p.icon, p.iconPx, p.iconColor)}</div>${label}`
	);
}

export function clusterSize(n: number): number {
	return 34 + Math.min(12, n);
}

/** A cluster bubble (§5.1). */
export function clusterHtml(n: number): string {
	const s = clusterSize(n);
	return `<div class="fs-cluster" style="width:${s}px;height:${s}px">${n}</div>`;
}

/** "{n} markers · {up to 3 distinct kickers}" */
export function clusterTooltip(ms: MapMarker[]): string {
	const kinds = [...new Set(ms.map((m) => m.card.kicker))].slice(0, 3).join(', ');
	return `${ms.length} markers · ${kinds}`;
}
