// Marker search (DESIGN-NOTES §5.2 with the plan ruling): a case-insensitive
// substring match on each searchable marker's terms, ranked exact, then
// prefix, then substring; within a rank by kind (portal, base, tame, sign,
// altar, trader, landmark), then marker order. Beds, tombstones and dungeons
// are never returned. Unexplored markers never get here: the server filters
// the snapshot to the explored mask.

import type { IconName, MapMarker, Pin, PinType } from './markers';

export interface SearchResult {
	id: string;
	title: string;
	sub: string;
	pin: Pin;
	icon: IconName;
}

const SEARCHABLE = new Set<PinType>(['portal', 'base', 'tame', 'sign', 'altar', 'trader', 'landmark']);

function normalise(q: string): string {
	return q.toLowerCase().trim().replace(/[’‘]/g, "'");
}

/** 0 exact, 1 prefix, 2 substring, undefined no match. */
function rank(terms: string[], q: string): number | undefined {
	let best: number | undefined;
	for (const t of terms) {
		const r = t === q ? 0 : t.startsWith(q) ? 1 : t.includes(q) ? 2 : undefined;
		if (r !== undefined && (best === undefined || r < best)) best = r;
		if (best === 0) break;
	}
	return best;
}

function fact(m: MapMarker, k: string): string | undefined {
	return m.card.facts.find((f) => f.k === k)?.v;
}

function subLine(m: MapMarker): string {
	switch (m.type) {
		case 'portal':
			return m.pin.ring ? 'Portal · unpaired' : 'Portal · paired';
		case 'base':
			return `Base by ${fact(m, 'Builder')} · ${fact(m, 'Pieces')} pieces`;
		case 'tame': {
			const species = fact(m, 'Species') ?? '';
			const near = fact(m, 'Near');
			return near ? `${species} · near ${near}` : species;
		}
		case 'altar':
			return `Boss altar · ${m.card.badges[0]?.text ?? ''}`;
		default:
			return m.card.kicker;
	}
}

export function search(all: MapMarker[], q: string, limit = 8): SearchResult[] {
	const query = normalise(q);
	if (!query) return [];
	const hits: { m: MapMarker; r: number; i: number }[] = [];
	all.forEach((m, i) => {
		if (!SEARCHABLE.has(m.type)) return;
		const r = rank(m.terms, query);
		if (r !== undefined) hits.push({ m, r, i });
	});
	hits.sort((a, b) => a.r - b.r || a.m.kindOrder - b.m.kindOrder || a.i - b.i);
	return hits.slice(0, limit).map(({ m }) => ({
		id: m.id,
		title: m.title,
		sub: subLine(m),
		pin: m.pin,
		icon: m.pin.icon
	}));
}

const SIGN_HINT_CHARS = 20;

/**
 * Up to 5 hints derived from the data (plan ruling): the first portal tag,
 * named tame, base builder, boss-altar label and sign text (≤ 20 chars),
 * skipping empty values and duplicates.
 */
export function hints(all: MapMarker[]): string[] {
	const first = (type: PinType, pick: (m: MapMarker) => string | undefined): string | undefined => {
		for (const m of all) {
			if (m.type !== type) continue;
			const v = pick(m)?.trim();
			if (v) return v;
		}
		return undefined;
	};
	const named = (m: MapMarker) => fact(m, 'Name');
	const builder = (m: MapMarker) => {
		const b = fact(m, 'Builder');
		return b === 'Unknown builder' ? undefined : b;
	};
	const tag = (m: MapMarker) => (m.terms.length ? m.title : undefined);
	const sign = (m: MapMarker) => (m.terms.length ? m.title.slice(0, SIGN_HINT_CHARS).trim() : undefined);
	const out: string[] = [];
	for (const h of [
		first('portal', tag),
		first('tame', named),
		first('base', builder),
		first('altar', (m) => m.title),
		first('sign', sign)
	]) {
		if (h && !out.includes(h)) out.push(h);
	}
	return out.slice(0, 5);
}
