import './testing/leaflet-node';
import { describe, expect, test } from 'vitest';
import {
	BIOME_LEGEND,
	LAYERS,
	defaultLayers,
	enabledLayerCount,
	markersKey,
	buildMarkers,
	clusterHtml,
	layerCounts,
	pinHtml,
	portalPairs,
	visibleMarkers,
	type LayerKey,
	type MapMarker
} from './markers';
import { fixtureSnapshot, fixtureWorld } from './testing/markers-fixture';

const INK = '#26231f';
const CREAM = '#f5ead8';
const COLD = '#3d7eab';
const EMBER = '#c67139';
const SAGE = '#7a8a5e';

const ALL_ON: Record<LayerKey, boolean> = {
	biomes: true,
	structures: true,
	portals: true,
	beds: true,
	tombstones: true,
	tames: true,
	signs: true,
	locations: true
};

function build(): MapMarker[] {
	return buildMarkers(fixtureSnapshot(), fixtureWorld());
}
function byId(all: MapMarker[], id: string): MapMarker {
	const m = all.find((x) => x.id === id);
	if (!m) throw new Error(`no marker ${id}`);
	return m;
}
function fact(m: MapMarker, k: string): string | undefined {
	return m.card.facts.find((f) => f.k === k)?.v;
}

describe('LAYERS', () => {
	test('the §3.13 table minus vehicles and wards, in order, with defaults', () => {
		expect(LAYERS.map((l) => [l.key, l.label, l.short, l.icon, l.default])).toEqual([
			['biomes', 'Biomes & terrain', 'Terrain', 'mountain', true],
			['structures', 'Structures & bases', 'Bases', 'home', true],
			['portals', 'Portals', 'Portals', 'portal', true],
			['beds', 'Beds', 'Beds', 'bed', false],
			['tombstones', 'Tombstones', 'Tombstones', 'skull', true],
			['tames', 'Tamed creatures', 'Tames', 'paw-print', true],
			['signs', 'Signs', 'Signs', 'signpost', false],
			['locations', 'Dungeons & locations', 'Locations', 'arch', true]
		]);
	});
});

describe('buildMarkers: kinds, types, layers and pins (§4.1, §8)', () => {
	test('one marker per input, sorted bases, portals, altars, traders, dungeons, tombstones, tames, beds, signs', () => {
		const all = build();
		expect(all.map((m) => m.id)).toEqual([
			'base-1',
			'portal-1',
			'portal-2',
			'portal-3',
			'portal-4',
			'portal-5',
			'portal-6',
			'portal-7',
			'loc-1',
			'loc-2',
			'loc-3',
			'loc-5',
			'loc-4',
			'tombstone-1',
			'tame-1',
			'tame-2',
			'bed-1',
			'sign-1',
			'sign-2'
		]);
	});

	test.each([
		['base-1', 'base', 'structures', { size: 36, bg: CREAM, border: INK, icon: 'home', iconColor: INK, iconPx: 19 }],
		['portal-1', 'portal', 'portals', { size: 28, bg: COLD, border: CREAM, icon: 'portal', iconColor: CREAM, iconPx: 15 }],
		[
			'portal-3',
			'portal',
			'portals',
			{ size: 28, bg: COLD, border: CREAM, icon: 'portal', iconColor: CREAM, iconPx: 15, ring: true }
		],
		['tombstone-1', 'tomb', 'tombstones', { size: 28, bg: EMBER, border: CREAM, icon: 'skull', iconColor: CREAM, iconPx: 15 }],
		['loc-1', 'altar', 'locations', { size: 30, bg: SAGE, border: CREAM, icon: 'flame', iconColor: CREAM, iconPx: 16 }],
		['loc-2', 'altar', 'locations', { size: 30, bg: INK, border: CREAM, icon: 'flame', iconColor: CREAM, iconPx: 16 }],
		['bed-1', 'bed', 'beds', { size: 26, bg: INK, border: CREAM, icon: 'bed', iconColor: CREAM, iconPx: 14 }],
		['tame-1', 'tame', 'tames', { size: 26, bg: INK, border: CREAM, icon: 'paw-print', iconColor: CREAM, iconPx: 14 }],
		['sign-1', 'sign', 'signs', { size: 26, bg: INK, border: CREAM, icon: 'signpost', iconColor: CREAM, iconPx: 14 }],
		['loc-3', 'trader', 'locations', { size: 26, bg: INK, border: CREAM, icon: 'coins', iconColor: CREAM, iconPx: 14 }],
		['loc-4', 'dungeon', 'locations', { size: 26, bg: INK, border: CREAM, icon: 'arch', iconColor: CREAM, iconPx: 14 }],
		['loc-5', 'dungeon', 'locations', { size: 26, bg: INK, border: CREAM, icon: 'mountain', iconColor: CREAM, iconPx: 14 }]
	])('%s → %s on %s', (id, type, layer, pin) => {
		const m = byId(build(), id);
		expect(m.type).toBe(type);
		expect(m.layer).toBe(layer);
		expect(m.pin).toEqual(pin);
		expect(m.card.icon).toBe(pin.icon);
		expect(m.card.discBg).toBe(pin.bg);
	});

	test('positions and the where line (U+2212 minus)', () => {
		const m = byId(build(), 'portal-3');
		expect([m.x, m.z]).toEqual([-1234.4, 3050]);
		expect(m.card.where).toBe('X −1,234 · Z 3,050');
	});

	test('tooltips are "{Kicker} · {title}"', () => {
		const all = build();
		expect(byId(all, 'portal-1').tooltip).toBe('Portal · home');
		expect(byId(all, 'sign-2').tooltip).toBe('Sign · “Welcome home”');
		expect(byId(all, 'loc-4').tooltip).toBe('Dungeon · Sunken crypt');
	});
});

describe('buildMarkers: card content (§4.2 with MVP adaptations)', () => {
	test('paired portal', () => {
		const m = byId(build(), 'portal-1');
		expect(m.title).toBe('home');
		expect(m.card.kicker).toBe('Portal');
		expect(m.card.badges).toEqual([{ text: 'Paired', tone: 'cold' }]);
		expect(m.card.facts).toEqual([
			{ k: 'Tag', v: '“home”' },
			{ k: 'Partner', v: '3.0 km east' }
		]);
		expect(m.card.partnerId).toBe('portal-2');
		expect(m.card.note).toBe('Leads to the “home” portal 3.0 km east.');
		expect(byId(build(), 'portal-2').card.facts[1]).toEqual({ k: 'Partner', v: '3.0 km west' });
	});

	test('unpaired portal with a unique tag', () => {
		const m = byId(build(), 'portal-3');
		expect(m.card.badges).toEqual([{ text: 'Unpaired', tone: 'ember' }]);
		expect(m.card.facts).toEqual([{ k: 'Tag', v: '“copper”' }]);
		expect(m.card.partnerId).toBeUndefined();
		expect(m.card.note).toBe('No explored portal carries the tag “copper”, so it leads nowhere yet.');
	});

	// Fix round 1, item 4: the server blanks `pair` both when a portal
	// genuinely has no partner and when its partner was filtered out as
	// unexplored, so the client can't tell those apart — the note must stay
	// accurate (and give no "build one"/"retag it" advice) either way.
	test('unpaired portal whose partner is simply unexplored gets the same honest note', () => {
		const snap = fixtureSnapshot();
		// portal-3's tag ("copper") is unique in the fixture, so this is
		// indistinguishable, from the client's view, from a hidden partner
		// sharing the same tag: both are "no EXPLORED portal carries it".
		const m = byId(buildMarkers(snap), 'portal-3');
		expect(m.pin.ring).toBe(true);
		expect(m.card.note).not.toMatch(/build a partner|retag/i);
		expect(m.card.note).not.toMatch(/no other portal/i);
		expect(m.card.note).toBe('No explored portal carries the tag “copper”, so it leads nowhere yet.');
	});

	test('three portals sharing a tag', () => {
		for (const id of ['portal-4', 'portal-5', 'portal-6']) {
			const m = byId(build(), id);
			expect(m.card.badges).toEqual([{ text: 'Unpaired', tone: 'ember' }]);
			expect(m.card.note).toBe('3 portals share the tag “hub”, so the game can’t pair them.');
		}
	});

	test('untagged portal', () => {
		const m = byId(build(), 'portal-7');
		expect(m.title).toBe('Untagged portal');
		expect(m.card.title).toBe('Untagged portal');
		expect(m.card.badges).toEqual([{ text: 'Unpaired', tone: 'ember' }]);
		expect(m.card.note).toBe('This portal has no tag, so it leads nowhere.');
		expect(m.pin.ring).toBe(true);
	});

	test('bed with an empty owner', () => {
		const m = byId(build(), 'bed-1');
		expect(m.card.kicker).toBe('Bed');
		expect(m.title).toBe('Bed');
		expect(m.card.facts).toEqual([{ k: 'Owner', v: 'Unknown' }]);
		expect(m.card.note).toBe('Spawn point for someone.');
	});

	test('tombstone', () => {
		const m = byId(build(), 'tombstone-1');
		expect(m.card.kicker).toBe('Tombstone');
		expect(m.title).toBe('Bjorn');
		expect(m.card.facts).toEqual([{ k: 'Player', v: 'Bjorn' }]);
		expect(m.card.badges).toEqual([]);
		expect(m.card.note).toBe('Where Bjorn died. It stays until they recover their items.');
	});

	test('named tame near a base', () => {
		const m = byId(build(), 'tame-1');
		expect(m.title).toBe('Big Mama');
		expect(m.card.kicker).toBe('Tamed creature');
		expect(m.card.badges).toEqual([{ text: 'Tamed', tone: 'sage' }]);
		expect(m.card.facts).toEqual([
			{ k: 'Name', v: 'Big Mama' },
			{ k: 'Species', v: 'Lox' },
			{ k: 'Near', v: "Halvor's base" }
		]);
	});

	test('unnamed Hen, far from any base', () => {
		const m = byId(build(), 'tame-2');
		expect(m.title).toBe('Hen');
		expect(fact(m, 'Name')).toBeUndefined();
		expect(fact(m, 'Species')).toBe('Hen');
		expect(fact(m, 'Near')).toBeUndefined();
	});

	test('blank sign and a sign with text', () => {
		const all = build();
		const blank = byId(all, 'sign-1');
		expect(blank.title).toBe('Blank sign');
		expect(blank.card.facts).toEqual([]);
		expect(blank.card.note).toBe('Sign text as of the last save.');
		expect(byId(all, 'sign-2').title).toBe('Welcome home');
	});

	test('base with three builders, one unnamed', () => {
		const m = byId(build(), 'base-1');
		expect(m.title).toBe("Halvor's base");
		expect(m.label).toBe("Halvor's base");
		expect(m.card.kicker).toBe('Base');
		expect(m.card.facts).toEqual([
			{ k: 'Builder', v: 'Halvor' },
			{ k: 'Pieces', v: '2,184' },
			{ k: 'Also built by', v: 'Frøya, Unknown builder' }
		]);
		expect(m.card.note).toBe('Builder = whoever placed the most pieces here.');
	});

	test('base naming fallbacks', () => {
		const snap = fixtureSnapshot();
		snap.bases = [
			{ id: 'b1', name: 'Longhouse', x: 0, z: 0, radius: 10, pieces: 5, builders: [{ id: 1, name: 'Ulf', pieces: 5 }] },
			{ id: 'b2', name: '', x: 900, z: 0, radius: 10, pieces: 5, builders: [{ id: 1, pieces: 5 }] },
			{ id: 'b3', name: '', x: 1900, z: 0, radius: 10, pieces: 5, builders: [] }
		];
		const all = buildMarkers(snap);
		expect(byId(all, 'b1').title).toBe('Longhouse');
		expect(fact(byId(all, 'b1'), 'Also built by')).toBe('—');
		expect(byId(all, 'b2').title).toBe('Base');
		expect(fact(byId(all, 'b2'), 'Builder')).toBe('Unknown builder');
		expect(byId(all, 'b3').title).toBe('Base');
	});

	test('boss altars join defeated from world.bosses', () => {
		const all = build();
		const eik = byId(all, 'loc-1');
		expect(eik.card.kicker).toBe('Boss altar');
		expect(eik.title).toBe('Eikthyr');
		expect(eik.card.badges).toEqual([{ text: 'Defeated', tone: 'sage' }]);
		expect(fact(eik, 'Forsaken')).toBe('Eikthyr');
		expect(eik.card.note).toBe('The world remembers this victory.');
		const yag = byId(all, 'loc-2');
		expect(yag.card.badges).toEqual([{ text: 'Not yet defeated', tone: 'neutral' }]);
		expect(yag.card.note).toBe('Still waiting for a brave crew.');
	});

	test('altars are not defeated without a world', () => {
		const eik = byId(buildMarkers(fixtureSnapshot()), 'loc-1');
		expect(eik.card.badges).toEqual([{ text: 'Not yet defeated', tone: 'neutral' }]);
		expect(eik.pin.bg).toBe(INK);
	});

	test('trader', () => {
		const m = byId(build(), 'loc-3');
		expect(m.card.kicker).toBe('Trader');
		expect(m.title).toBe('Haldor');
		expect(m.card.facts).toEqual([{ k: 'Sells', v: 'Gear, upgrades, pocket expansions' }]);
		expect(m.card.note).toBe('Trader camp. Shown because someone has been here.');
	});

	test('dungeons', () => {
		const all = build();
		for (const id of ['loc-4', 'loc-5']) {
			const m = byId(all, id);
			expect(m.card.kicker).toBe('Dungeon');
			expect(m.minZoom).toBe(3);
			expect(m.card.note).toBe(
				'Shown because someone has explored this area. Locations in unexplored areas stay hidden.'
			);
		}
		expect(byId(all, 'loc-4').title).toBe('Sunken crypt');
		expect(byId(all, 'loc-4').pin.icon).toBe('arch');
		expect(byId(all, 'loc-5').pin.icon).toBe('mountain');
		// Only dungeons are zoom-gated.
		expect(all.filter((m) => m.minZoom !== undefined).map((m) => m.id)).toEqual(['loc-5', 'loc-4']);
	});

	test('only bases carry a label', () => {
		expect(
			build()
				.filter((m) => m.label !== undefined)
				.map((m) => m.id)
		).toEqual(['base-1']);
	});
});

describe('visibleMarkers', () => {
	test('every marker shows with all layers on (the server already dropped unexplored ones)', () => {
		const all = build();
		expect(visibleMarkers(all, ALL_ON, 4)).toEqual(all);
	});

	test('a layer that is off is hidden', () => {
		const ids = visibleMarkers(build(), { ...ALL_ON, portals: false }, 4).map((m) => m.id);
		expect(ids.some((id) => id.startsWith('portal'))).toBe(false);
		expect(ids).toContain('base-1');
	});

	test('dungeons are hidden below zoom 3; altars and traders are not', () => {
		const below = visibleMarkers(build(), ALL_ON, 2.75).map((m) => m.id);
		expect(below).not.toContain('loc-4');
		expect(below).not.toContain('loc-5');
		expect(below).toContain('loc-1');
		expect(below).toContain('loc-3');
		const at = visibleMarkers(build(), ALL_ON, 3).map((m) => m.id);
		expect(at).toContain('loc-4');
	});
});

describe('layerCounts', () => {
	test('counts every marker per layer, with pairs and unpaired', () => {
		const c = layerCounts(build());
		expect(c).toEqual({
			biomes: 9,
			structures: 1,
			portals: 7,
			beds: 1,
			tombstones: 1,
			tames: 2,
			signs: 2,
			locations: 5,
			unpaired: 5,
			pairs: 1
		});
	});
});

describe('portalPairs', () => {
	test('each pair once, when both ends are visible', () => {
		const all = build();
		const pairs = portalPairs(all);
		expect(pairs.map(([a, b]) => [a.id, b.id])).toEqual([['portal-1', 'portal-2']]);
	});

	test('no pair when an end is hidden', () => {
		const all = build();
		const vis = visibleMarkers(all.filter((m) => m.id !== 'portal-2'), ALL_ON, 4);
		expect(portalPairs(vis)).toEqual([]);
		expect(portalPairs(visibleMarkers(all, { ...ALL_ON, portals: false }, 4))).toEqual([]);
	});
});

describe('pin HTML', () => {
	test('a single pin: disc, icon, ring and selection', () => {
		const all = build();
		const html = pinHtml(byId(all, 'portal-3'), { selected: false, showLabel: false });
		expect(html).toContain('width:28px');
		expect(html).toContain('background:#3d7eab');
		expect(html).toContain('border:2px solid #f5ead8');
		expect(html).toContain('0 0 0 3px #c67139, 0 0 0 7px rgba(198,113,57,.35)');
		expect(html).toContain('<svg');
		const sel = pinHtml(byId(all, 'portal-3'), { selected: true, showLabel: false });
		expect(sel).toContain('0 0 0 4px #7fb2d6, 0 0 0 8px rgba(127,178,214,.35), 0 4px 12px rgba(0,0,0,.4)');
		expect(pinHtml(byId(all, 'portal-1'), { selected: false, showLabel: false })).toContain(
			'0 2px 6px rgba(0,0,0,.35)'
		);
	});

	test('base labels are escaped and optional', () => {
		const snap = fixtureSnapshot();
		snap.bases[0].name = '<b>&"Hall"';
		const base = byId(buildMarkers(snap), 'base-1');
		const html = pinHtml(base, { selected: false, showLabel: true });
		expect(html).toContain('&lt;b&gt;&amp;&quot;Hall&quot;');
		expect(html).not.toContain('<b>');
		expect(pinHtml(base, { selected: false, showLabel: false })).not.toContain('fs-pin-label');
	});

	test('cluster bubble: size 34 + min(12, n)', () => {
		expect(clusterHtml(3)).toContain('width:37px');
		expect(clusterHtml(30)).toContain('width:46px');
		expect(clusterHtml(3)).toContain('>3<');
	});
});

describe('enabledLayerCount (the Layers badge)', () => {
	test('defaults: 6 of the 8 main layers', () => {
		expect(enabledLayerCount(defaultLayers())).toBe(6);
	});
	test('all on -> 8; biomes off -> 7', () => {
		expect(enabledLayerCount(ALL_ON)).toBe(8);
		expect(enabledLayerCount({ ...ALL_ON, biomes: false })).toBe(7);
	});
});

describe('BIOME_LEGEND', () => {
	test('the tile palette in the design order, plus Unexplored', () => {
		expect(BIOME_LEGEND).toEqual([
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
		]);
	});
});

describe('markersKey (memoising the marker model)', () => {
	test('stable across identical cards and snapshot refetches', () => {
		const a = markersKey('demo', fixtureSnapshot(), fixtureWorld());
		const b = markersKey('demo', fixtureSnapshot(), { ...fixtureWorld(), day: 999, exploredPct: 50 });
		expect(a).toBe(b);
	});
	test('independent of boss order', () => {
		const w = fixtureWorld();
		const rev = { ...w, bosses: [...w.bosses].reverse() };
		expect(markersKey('demo', fixtureSnapshot(), rev)).toBe(markersKey('demo', fixtureSnapshot(), w));
	});
	test('changes with the server, the save and the defeated set', () => {
		const snap = fixtureSnapshot();
		const w = fixtureWorld();
		const base = markersKey('demo', snap, w);
		expect(markersKey('other', snap, w)).not.toBe(base);
		expect(markersKey('demo', { ...snap, savedAt: '2026-09-30T11:00:00Z' }, w)).not.toBe(base);
		const flipped = { ...w, bosses: w.bosses.map((b, i) => (i === w.bosses.length - 1 ? { ...b, defeated: !b.defeated } : b)) };
		expect(markersKey('demo', snap, flipped)).not.toBe(base);
		expect(markersKey('demo', snap, undefined)).not.toBe(base);
	});
});
