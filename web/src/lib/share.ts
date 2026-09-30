// Share-link hash parsing: `/#s=<id>&k=<passphrase>`. Per the plan's global
// constraints, the passphrase is only ever read from the hash and is never
// stored or logged; the caller replaces the URL with `hashFor(server)` once
// it has been used.

export interface ShareLink {
	server?: string;
	key?: string;
}

export function parseHash(hash: string): ShareLink {
	if (!hash || hash === '#') return {};
	const raw = hash.startsWith('#') ? hash.slice(1) : hash;
	const params = new URLSearchParams(raw);
	const result: ShareLink = {};
	const server = params.get('s');
	const key = params.get('k');
	if (server) result.server = server;
	if (key) result.key = key;
	return result;
}

export function hashFor(server: string): string {
	return `#s=${encodeURIComponent(server)}`;
}
