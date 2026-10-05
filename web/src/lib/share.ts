// Share-link and view hash parsing: `/#s=<id>&k=<passphrase>`, plus the
// open view: `&p=<playerId>` (a player's profile) or `&activity` (the
// activity timeline). Per the plan's global constraints, the passphrase is
// only ever read from the hash and is never stored or logged; the caller
// replaces the URL with `hashFor(server)` once it has been used.

/** A view over the map that browser back closes (Plan 7). */
export type View = { kind: 'profile'; player: string } | { kind: 'activity' };

export interface ShareLink {
	server?: string;
	key?: string;
	view?: View;
}

export function parseHash(hash: string): ShareLink {
	if (!hash || hash === '#') return {};
	const raw = hash.startsWith('#') ? hash.slice(1) : hash;
	const params = new URLSearchParams(raw);
	const result: ShareLink = {};
	const server = params.get('s');
	const key = params.get('k');
	const player = params.get('p');
	if (server) result.server = server;
	if (key) result.key = key;
	if (player) result.view = { kind: 'profile', player };
	else if (params.has('activity')) result.view = { kind: 'activity' };
	return result;
}

export function hashFor(server: string, view?: View): string {
	const base = `#s=${encodeURIComponent(server)}`;
	if (view?.kind === 'profile') return `${base}&p=${encodeURIComponent(view.player)}`;
	if (view?.kind === 'activity') return `${base}&activity`;
	return base;
}

export function sameView(a: View | undefined, b: View | undefined): boolean {
	if (!a || !b) return a === b;
	if (a.kind === 'profile' && b.kind === 'profile') return a.player === b.player;
	return a.kind === b.kind;
}
