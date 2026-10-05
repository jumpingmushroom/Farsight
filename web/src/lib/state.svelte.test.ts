import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { filters } from './filters.svelte';
import { AppState, CARD_EVERY_MS, unlockMessage, type Visibility } from './state.svelte';
import type { Activity, ActivityPage, Card, Profile, ServerSummary, SnapshotView, TodaySessions, WorldCard } from './types';

// --- fixtures (invented names only) ----------------------------------------

const SERVERS: ServerSummary[] = [
	{ id: 'a', name: 'Alderholm', status: 'online', players: 2, maxPlayers: 10 },
	{ id: 'b', name: 'Birchmoor', status: 'offline', players: 0, maxPlayers: 10 }
];

function makeWorld(savedAt: string): WorldCard {
	return {
		name: 'Testworld',
		seedName: 'seedy',
		day: 12,
		bosses: [],
		modifiers: {},
		flags: [],
		exploredPct: 3.2,
		savedAt,
		readAt: savedAt
	};
}

function makeCard(id: string, overrides: Partial<Card> = {}): Card {
	return {
		id,
		name: id === 'a' ? 'Alderholm' : 'Birchmoor',
		crossplay: false,
		maxPlayers: 10,
		status: 'online',
		players: 0,
		online: [],
		recent: [],
		activity: [],
		world: makeWorld('2026-09-30T10:00:00Z'),
		tiles: { state: 'complete', done: 10, total: 10, key: 'k1' },
		...overrides
	};
}

function makeProfile(over: Partial<Profile> = {}): Profile {
	return {
		id: '1',
		name: 'Astrid',
		platform: 'Steam',
		timeZone: 'UTC',
		online: true,
		since: '2026-09-30T10:00:00Z',
		firstSeen: '2026-09-29T10:00:00Z',
		trackedSince: '2026-09-29T10:00:00Z',
		weekSeconds: 100,
		allSeconds: 100,
		sessions: 1,
		days: [],
		beds: { count: 0, near: [] },
		bases: [],
		portals: [],
		tames: [],
		deaths: { spotted: 0, week: 0, tombstones: [] },
		...over
	};
}

function act(type: string, at: string, extra: Partial<Activity> = {}): Activity {
	return { id: `${type}@${at}`, type, category: 'session', source: 'log', at, who: [], ...extra };
}

function makeActivityPage(events: Activity[] = [], over: Partial<ActivityPage> = {}): ActivityPage {
	return {
		timeZone: 'UTC',
		from: '2026-09-27T00:00:00Z',
		until: '2026-09-30T00:00:00Z',
		events,
		counts: { session: 0, death: 0, boss: 0, build: 0, portal: 0, tame: 0, event: 0, server: 0 },
		people: [],
		...over
	};
}

function makeToday(players: TodaySessions['players'] = []): TodaySessions {
	return {
		timeZone: 'UTC',
		dayStart: '2026-09-30T00:00:00Z',
		dayEnd: '2026-10-01T00:00:00Z',
		now: '2026-09-30T10:05:00Z',
		players
	};
}

const SNAPSHOT: SnapshotView = {
	savedAt: '2026-09-30T10:00:00Z',
	fogKey: '0123456789abcdef',
	explored: { source: 'tables', cell: 12, size: 2048, bits: '' },
	markers: [],
	locations: [],
	bases: [],
	players: []
};

// --- fakes -------------------------------------------------------------------

interface Call {
	method: string;
	path: string;
	body?: unknown;
}

function json(body: unknown, status = 200): Response {
	return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

type Route = (call: Call) => Response | Promise<Response>;

class FakeServer {
	calls: Call[] = [];
	servers: ServerSummary[] = [...SERVERS];
	cards: Record<string, Card | number> = { a: makeCard('a'), b: makeCard('b') };
	/** Keyed "server/player"; a number is a status to fail with, undefined -> 404. */
	profiles: Record<string, Profile | number> = {};
	/** Keyed by server id; a number is a status to fail with, undefined -> 404. */
	activity: Record<string, ActivityPage | number> = {};
	today: Record<string, TodaySessions | number> = {};
	unlockStatus = 204;
	unlockAdds: ServerSummary | undefined = undefined;
	fail = false;
	override: Route | undefined;

	fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
		const path = String(input);
		const method = init?.method ?? 'GET';
		const body = typeof init?.body === 'string' ? JSON.parse(init.body) : undefined;
		const call = { method, path, body };
		this.calls.push(call);
		if (this.fail) throw new TypeError('Failed to fetch');
		if (this.override) return this.override(call);
		return this.route(call);
	}) as typeof fetch;

	route(call: Call): Response {
		const path = call.path;
		if (path === '/api/unlock') {
			if (this.unlockStatus === 204 && this.unlockAdds) this.servers = [...this.servers, this.unlockAdds];
			return new Response(this.unlockStatus === 204 ? null : 'x', { status: this.unlockStatus });
		}
		if (path === '/api/servers') return json({ servers: this.servers });
		const snap = path.match(/^\/api\/servers\/([^/]+)\/snapshot$/);
		if (snap) return json(SNAPSHOT);
		const profile = path.match(/^\/api\/servers\/([^/]+)\/players\/([^/]+)$/);
		if (profile) {
			const key = `${decodeURIComponent(profile[1])}/${decodeURIComponent(profile[2])}`;
			const p = this.profiles[key];
			if (p === undefined) return new Response('not found', { status: 404 });
			if (typeof p === 'number') return new Response('err', { status: p });
			return json(p);
		}
		const today = path.match(/^\/api\/servers\/([^/]+)\/sessions\/today$/);
		if (today) {
			const t = this.today[decodeURIComponent(today[1])];
			if (t === undefined) return new Response('not found', { status: 404 });
			if (typeof t === 'number') return new Response('err', { status: t });
			return json(t);
		}
		const activity = path.match(/^\/api\/servers\/([^/]+)\/activity(\?.*)?$/);
		if (activity) {
			const a = this.activity[decodeURIComponent(activity[1])];
			if (a === undefined) return new Response('not found', { status: 404 });
			if (typeof a === 'number') return new Response('err', { status: a });
			return json(a);
		}
		const card = path.match(/^\/api\/servers\/([^/]+)$/);
		if (card) {
			const c = this.cards[decodeURIComponent(card[1])];
			if (c === undefined) return new Response('not found', { status: 404 });
			if (typeof c === 'number') return new Response('err', { status: c });
			return json(c);
		}
		return new Response('not found', { status: 404 });
	}

	count(path: string, method = 'GET'): number {
		return this.calls.filter((c) => c.path === path && c.method === method).length;
	}
}

class MemStorage implements Storage {
	private m = new Map<string, string>();
	get length() {
		return this.m.size;
	}
	clear() {
		this.m.clear();
	}
	getItem(k: string) {
		return this.m.has(k) ? this.m.get(k)! : null;
	}
	key(i: number) {
		return [...this.m.keys()][i] ?? null;
	}
	removeItem(k: string) {
		this.m.delete(k);
	}
	setItem(k: string, v: string) {
		this.m.set(k, v);
	}
}

class ThrowingStorage extends MemStorage {
	getItem(): string | null {
		throw new Error('SecurityError');
	}
	setItem(): void {
		throw new Error('QuotaExceededError');
	}
}

class FakeVisibility implements Visibility {
	isVisible = true;
	private cbs: (() => void)[] = [];
	visible() {
		return this.isVisible;
	}
	onVisible(cb: () => void) {
		this.cbs.push(cb);
		return () => {
			this.cbs = this.cbs.filter((c) => c !== cb);
		};
	}
	show() {
		this.isVisible = true;
		for (const cb of this.cbs) cb();
	}
}

/** Stands in for SvelteKit's history.state (its history/navigation index). */
const KIT_STATE = { 'sveltekit:history': 3, 'sveltekit:navigation': 3 };

function setup(hash = '', opts: { storage?: Storage | null } = {}) {
	const server = new FakeServer();
	const location = { hash };
	const replaced: { url: unknown; fetchesBefore: number }[] = [];
	const replaceState = vi.fn((_data: unknown, _unused: string, url?: string | URL | null) => {
		replaced.push({ url, fetchesBefore: server.calls.length });
		if (typeof url === 'string' && url.startsWith('#')) location.hash = url;
	});
	// SvelteKit keeps its navigation index in history.state; hash updates keep it.
	const back = vi.fn();
	const history = { replaceState, state: KIT_STATE, back };
	let hashChanged: () => void = () => {};
	const onHashChange = (cb: () => void) => {
		hashChanged = cb;
		return () => (hashChanged = () => {});
	};
	const storage = opts.storage === undefined ? new MemStorage() : opts.storage;
	const visibility = new FakeVisibility();
	const root = { dataset: {} as Record<string, string | undefined> };
	const log = vi.fn();
	const app = new AppState({ fetch: server.fetch, storage, location, history, visibility, root, log, onHashChange });
	return { app, server, location, replaceState, replaced, storage, visibility, root, log, back, hashChanged: () => hashChanged() };
}

// Flush pending promise chains (fake fetch resolves on microtasks).
async function flush() {
	for (let i = 0; i < 20; i++) await Promise.resolve();
	await vi.advanceTimersByTimeAsync(0);
}

beforeEach(() => {
	vi.useFakeTimers();
	vi.setSystemTime(new Date('2026-09-30T10:05:00Z'));
});

afterEach(() => {
	vi.useRealTimers();
});

// --- tests -------------------------------------------------------------------

describe('start()', () => {
	test('share link #s=b&k=pw unlocks, strips the key and selects b', async () => {
		const { app, server, replaceState } = setup('#s=b&k=pw');
		const stop = app.start();
		await flush();
		const unlocks = server.calls.filter((c) => c.path === '/api/unlock');
		expect(unlocks).toHaveLength(1);
		expect(unlocks[0].method).toBe('POST');
		expect(unlocks[0].body).toEqual({ server: 'b', passphrase: 'pw' });
		expect(replaceState).toHaveBeenCalledWith(KIT_STATE, '', '#s=b');
		expect(replaceState.mock.calls.every((c) => !String(c[2]).includes('k='))).toBe(true);
		expect(app.currentId).toBe('b');
		expect(app.loaded).toBe(true);
		expect(app.card?.id).toBe('b');
		expect(app.unlockPrompt).toBeUndefined();
		// Unlock happens before the servers list is loaded.
		const iUnlock = server.calls.findIndex((c) => c.path === '/api/unlock');
		const iServers = server.calls.findIndex((c) => c.path === '/api/servers');
		expect(iUnlock).toBeLessThan(iServers);
		stop();
	});

	test('share link: the key leaves the URL before the unlock request is sent', async () => {
		const { app, server, replaced } = setup('#s=b&k=pw');
		let release!: () => void;
		const gate = new Promise<void>((r) => (release = r));
		server.override = async (call) => {
			if (call.path === '/api/unlock') await gate; // a hanging unlock
			return server.route(call);
		};
		const stop = app.start();
		await flush();
		expect(server.count('/api/unlock', 'POST')).toBe(1);
		expect(replaced[0]).toEqual({ url: '#s=b', fetchesBefore: 0 });
		expect(replaced.every((r) => !String(r.url).includes('k='))).toBe(true);
		release();
		await flush();
		expect(app.currentId).toBe('b');
		stop();
	});

	test('calling start() twice does not double the timers or listeners', async () => {
		const { app, server, visibility } = setup('');
		const stop1 = app.start();
		const stop2 = app.start();
		await flush();
		expect(server.count('/api/servers')).toBe(1);
		const before = server.count('/api/servers/a');
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a')).toBe(before + 1);
		visibility.show();
		await flush();
		expect(server.count('/api/servers/a')).toBe(before + 2);
		stop2();
		stop1();
		const n = server.calls.length;
		await vi.advanceTimersByTimeAsync(60_000);
		expect(server.calls.length).toBe(n);
	});

	test('start() works again after stop()', async () => {
		const { app, server } = setup('');
		app.start()();
		await flush();
		const stop = app.start();
		await flush();
		const before = server.count('/api/servers/a');
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a')).toBe(before + 1);
		stop();
	});

	test('share link with a wrong passphrase still strips the key and prompts', async () => {
		const { app, server, replaceState } = setup('#s=b&k=nope');
		server.unlockStatus = 401;
		const stop = app.start();
		await flush();
		expect(replaceState).toHaveBeenCalledWith(KIT_STATE, '', '#s=b');
		expect(replaceState.mock.calls.every((c) => !String(c[2]).includes('k='))).toBe(true);
		expect(app.unlockPrompt).toEqual({ server: 'b', error: 'Wrong passphrase.' });
		expect(JSON.stringify(app.unlockPrompt)).not.toContain('nope');
		stop();
	});

	test('share link for an unlisted server strips the key even when unlock throws', async () => {
		const { app, server, replaceState } = setup('#s=zzz&k=nope');
		server.unlockStatus = 500;
		const stop = app.start();
		await flush();
		expect(replaceState.mock.calls[0]).toEqual([KIT_STATE, '', '#s=zzz']);
		expect(replaceState.mock.calls.every((c) => !String(c[2]).includes('k='))).toBe(true);
		expect(app.unlockPrompt?.server).toBe('zzz');
		expect(app.unlockPrompt?.error).toMatch(/Couldn’t reach Farsight/);
		expect(app.currentId).toBe('a');
		stop();
	});

	test('no hash selects the first server', async () => {
		const { app, server } = setup('');
		const stop = app.start();
		await flush();
		expect(server.count('/api/unlock', 'POST')).toBe(0);
		expect(app.loaded).toBe(true);
		expect(app.servers.map((s) => s.id)).toEqual(['a', 'b']);
		expect(app.currentId).toBe('a');
		expect(app.card?.id).toBe('a');
		stop();
	});

	test('hash #s=b without a key selects b when it is listed', async () => {
		const { app } = setup('#s=b');
		const stop = app.start();
		await flush();
		expect(app.currentId).toBe('b');
		stop();
	});

	test('hash with an unknown server falls back to the first', async () => {
		const { app } = setup('#s=zzz');
		const stop = app.start();
		await flush();
		expect(app.currentId).toBe('a');
		stop();
	});

	test('no servers leaves currentId undefined', async () => {
		const { app, server } = setup('');
		server.servers = [];
		const stop = app.start();
		await flush();
		expect(app.loaded).toBe(true);
		expect(app.currentId).toBeUndefined();
		stop();
	});
});

describe('polling', () => {
	test('a 15 s tick refetches the card', async () => {
		const { app, server } = setup('');
		const stop = app.start();
		await flush();
		const before = server.count('/api/servers/a');
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a')).toBe(before + 1);
		stop();
	});

	test('hidden visibility skips the tick; becoming visible refreshes at once', async () => {
		const { app, server, visibility } = setup('');
		const stop = app.start();
		await flush();
		const before = server.count('/api/servers/a');
		visibility.isVisible = false;
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a')).toBe(before);
		visibility.show();
		await flush();
		expect(server.count('/api/servers/a')).toBe(before + 1);
		stop();
	});

	test('servers list refreshes every 60 s', async () => {
		const { app, server } = setup('');
		const stop = app.start();
		await flush();
		const before = server.count('/api/servers');
		await vi.advanceTimersByTimeAsync(60_000);
		expect(server.count('/api/servers')).toBe(before + 1);
		stop();
	});

	test('now ticks every 30 s', async () => {
		const { app } = setup('');
		const stop = app.start();
		await flush();
		const t0 = app.now.getTime();
		await vi.advanceTimersByTimeAsync(30_000);
		expect(app.now.getTime()).toBeGreaterThanOrEqual(t0 + 30_000);
		stop();
	});

	test('stop() halts all timers', async () => {
		const { app, server } = setup('');
		const stop = app.start();
		await flush();
		stop();
		const n = server.calls.length;
		await vi.advanceTimersByTimeAsync(120_000);
		expect(server.calls.length).toBe(n);
	});

	test('a changed world.savedAt triggers a snapshot fetch, an unchanged one does not', async () => {
		const { app, server } = setup('');
		const stop = app.start();
		await flush();
		expect(server.count('/api/servers/a/snapshot')).toBe(1);
		expect(app.snapshot).toEqual(SNAPSHOT);
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a/snapshot')).toBe(1);
		server.cards.a = makeCard('a', { world: makeWorld('2026-09-30T10:20:00Z') });
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a/snapshot')).toBe(2);
		stop();
	});

	test('a card without a world sets snapshot to null without fetching', async () => {
		const { app, server } = setup('');
		server.cards.a = makeCard('a', { world: undefined, tiles: { state: 'none', done: 0, total: 0 } });
		const stop = app.start();
		await flush();
		expect(app.snapshot).toBeNull();
		expect(server.count('/api/servers/a/snapshot')).toBe(0);
		stop();
	});

	test('a card 404 drops the server and selects the next', async () => {
		const { app, server } = setup('');
		const stop = app.start();
		await flush();
		expect(app.currentId).toBe('a');
		delete server.cards.a;
		await vi.advanceTimersByTimeAsync(15_000);
		await flush();
		expect(app.servers.map((s) => s.id)).toEqual(['b']);
		expect(app.currentId).toBe('b');
		expect(app.card?.id).toBe('b');
		stop();
	});

	test('servers refresh dropping the current server selects its same-index neighbour', async () => {
		const { app, server } = setup('#s=b');
		const c: ServerSummary = { id: 'c', name: 'Coldwater', status: 'online', players: 1, maxPlayers: 10 };
		server.servers = [...SERVERS, c];
		server.cards.c = makeCard('c');
		const stop = app.start();
		await flush();
		expect(app.currentId).toBe('b');
		server.servers = [SERVERS[0], c];
		await vi.advanceTimersByTimeAsync(60_000);
		await flush();
		expect(app.servers.map((s) => s.id)).toEqual(['a', 'c']);
		expect(app.currentId).toBe('c');
		stop();
	});

	test('overlapping refreshes: a second refresh waits for the slow one, then the newest data wins', async () => {
		const { app, server } = setup('');
		const stop = app.start();
		await flush();
		let release!: () => void;
		const gate = new Promise<void>((r) => (release = r));
		let first = true;
		server.override = async (call) => {
			if (call.path === '/api/servers/a' && first) {
				first = false;
				const older = json(makeCard('a', { players: 1, world: makeWorld('2026-09-30T10:10:00Z') }));
				await gate;
				return older;
			}
			return server.route(call);
		};
		const cards = server.count('/api/servers/a');
		const p1 = app.refresh(); // slow
		server.cards.a = makeCard('a', { players: 7, world: makeWorld('2026-09-30T10:20:00Z') });
		const p2 = app.refresh(); // joins the flight instead of racing it
		await flush();
		expect(server.count('/api/servers/a')).toBe(cards + 1);
		release();
		await Promise.all([p1, p2]);
		await flush();
		// The follow-up run fetched the newer card and its snapshot.
		expect(server.count('/api/servers/a')).toBe(cards + 2);
		expect(app.card?.players).toBe(7);
		expect(app.card?.world?.savedAt).toBe('2026-09-30T10:20:00Z');
		stop();
	});

	test('a failed snapshot fetch (5xx) is retried on the next poll', async () => {
		const { app, server } = setup('');
		let failSnap = true;
		server.override = (call) => {
			if (call.path === '/api/servers/a/snapshot' && failSnap) return new Response('boom', { status: 502 });
			return server.route(call);
		};
		const stop = app.start();
		await flush();
		expect(server.count('/api/servers/a/snapshot')).toBe(1);
		expect(app.snapshot).toBeUndefined();
		failSnap = false;
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a/snapshot')).toBe(2);
		expect(app.snapshot).toEqual(SNAPSHOT);
		stop();
	});

	test('a snapshot 404 racing the card is not recorded: the next poll refetches it', async () => {
		const { app, server } = setup('');
		let stored = false;
		server.override = (call) => {
			if (call.path === '/api/servers/a/snapshot' && !stored) return new Response('not found', { status: 404 });
			return server.route(call);
		};
		const stop = app.start();
		await flush();
		expect(server.count('/api/servers/a/snapshot')).toBe(1);
		// No snapshot was applied, so nothing may be treated as loaded.
		expect(app.snapshot).toBeUndefined();
		stored = true;
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a/snapshot')).toBe(2);
		expect(app.snapshot).toEqual(SNAPSHOT);
		// Applied: the same savedAt is not fetched again.
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a/snapshot')).toBe(2);
		stop();
	});

	test('a snapshot 404 keeps the previous snapshot and retries', async () => {
		const { app, server } = setup('');
		const stop = app.start();
		await flush();
		expect(app.snapshot).toEqual(SNAPSHOT);
		const first = app.snapshot;
		let stored = false;
		server.override = (call) => {
			if (call.path === '/api/servers/a/snapshot' && !stored) return new Response('not found', { status: 404 });
			return server.route(call);
		};
		server.cards.a = makeCard('a', { world: makeWorld('2026-09-30T10:20:00Z') });
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a/snapshot')).toBe(2);
		expect(app.snapshot).toBe(first);
		stored = true;
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a/snapshot')).toBe(3);
		stop();
	});

	test('single flight: a slow snapshot that outlasts the next tick is applied, and fetched once', async () => {
		const { app, server } = setup('');
		let release!: () => void;
		const gate = new Promise<void>((r) => (release = r));
		server.override = async (call) => {
			if (call.path === '/api/servers/a/snapshot') await gate; // a slow link
			return server.route(call);
		};
		const stop = app.start();
		await flush();
		expect(server.count('/api/servers/a/snapshot')).toBe(1);
		const cards = server.count('/api/servers/a');
		// Two ticks and a visibility change fire while the download is in flight.
		await vi.advanceTimersByTimeAsync(15_000);
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a')).toBe(cards);
		release();
		await flush();
		expect(app.snapshot).toEqual(SNAPSHOT);
		expect(server.count('/api/servers/a/snapshot')).toBe(1);
		// The ticks that were skipped are folded into one follow-up card fetch.
		expect(server.count('/api/servers/a')).toBe(cards + 1);
		stop();
	});

	test('a card 404 on the last server leaves nothing selected', async () => {
		const { app, server } = setup('');
		server.servers = [SERVERS[0]];
		const stop = app.start();
		await flush();
		delete server.cards.a;
		await vi.advanceTimersByTimeAsync(15_000);
		await flush();
		expect(app.servers).toEqual([]);
		expect(app.currentId).toBeUndefined();
		expect(app.card).toBeUndefined();
		stop();
	});

	test('a network error keeps the last data and logs once per failure streak', async () => {
		const { app, server, log } = setup('');
		const stop = app.start();
		await flush();
		const card = app.card;
		server.fail = true;
		await vi.advanceTimersByTimeAsync(15_000);
		await vi.advanceTimersByTimeAsync(15_000);
		await vi.advanceTimersByTimeAsync(15_000);
		expect(app.card).toEqual(card);
		expect(app.servers.map((s) => s.id)).toEqual(['a', 'b']);
		expect(app.toast).toBeUndefined();
		expect(log).toHaveBeenCalledTimes(1);
		server.fail = false;
		await vi.advanceTimersByTimeAsync(15_000);
		server.fail = true;
		await vi.advanceTimersByTimeAsync(15_000);
		expect(log).toHaveBeenCalledTimes(2);
		stop();
	});

	test('a hung card fetch times out, and the next poll starts a fresh request instead of wedging', async () => {
		const { app, server } = setup('');
		server.override = async (call) => {
			if (call.path === '/api/servers/a') return new Promise<Response>(() => {}); // never resolves
			return server.route(call);
		};
		const stop = app.start();
		await flush();
		// The boot fetch for 'a' is now hanging.
		expect(server.count('/api/servers/a')).toBe(1);
		// A 15 s tick while it's still in flight joins the flight: no new request yet.
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a')).toBe(1);
		// Past the card's 20 s timeout the hung request ends (ApiError(0, 'timeout')),
		// and the queued follow-up (from the 15 s tick) fires immediately.
		await vi.advanceTimersByTimeAsync(5_000);
		await flush();
		expect(server.count('/api/servers/a')).toBe(2);
		// Polling keeps making forward progress rather than wedging on the hang.
		await vi.advanceTimersByTimeAsync(20_000);
		await flush();
		expect(server.count('/api/servers/a')).toBeGreaterThanOrEqual(3);
		stop();
	});

	test('tileSamples collect while rendering (last 5) and clear on complete', async () => {
		const { app, server } = setup('');
		let done = 0;
		server.cards.a = makeCard('a', { tiles: { state: 'rendering', done, total: 100 } });
		const stop = app.start();
		await flush();
		expect(app.tileSamples).toHaveLength(1);
		for (let i = 0; i < 6; i++) {
			done += 5;
			server.cards.a = makeCard('a', { tiles: { state: 'rendering', done, total: 100 } });
			await vi.advanceTimersByTimeAsync(15_000);
		}
		expect(app.tileSamples).toHaveLength(5);
		expect(app.tileSamples[4].done).toBe(30);
		server.cards.a = makeCard('a', { tiles: { state: 'complete', done: 100, total: 100, key: 'k' } });
		await vi.advanceTimersByTimeAsync(15_000);
		expect(app.tileSamples).toEqual([]);
		stop();
	});
});

describe('select()', () => {
	test('replaces the hash, clears card and snapshot, and loads at once', async () => {
		const { app, server, replaceState } = setup('');
		const stop = app.start();
		await flush();
		replaceState.mockClear();
		app.select('b');
		expect(replaceState).toHaveBeenCalledWith(KIT_STATE, '', '#s=b');
		expect(app.currentId).toBe('b');
		expect(app.card).toBeUndefined();
		expect(app.snapshot).toBeUndefined();
		await flush();
		expect(app.card?.id).toBe('b');
		expect(server.count('/api/servers/b')).toBe(1);
		stop();
	});

	test('a slow response for the previous server is ignored', async () => {
		const { app, server } = setup('');
		const stop = app.start();
		await flush();
		let release!: () => void;
		const gate = new Promise<void>((r) => (release = r));
		server.override = async (call) => {
			if (call.path === '/api/servers/a') await gate;
			return server.route(call);
		};
		void app.refresh();
		app.select('b');
		await flush();
		release();
		await flush();
		expect(app.currentId).toBe('b');
		expect(app.card?.id).toBe('b');
		stop();
	});
});

describe('theme', () => {
	test('start() applies the stored theme', async () => {
		const storage = new MemStorage();
		storage.setItem('farsight.theme', 'light');
		const { app, root } = setup('', { storage });
		const stop = app.start();
		expect(app.theme).toBe('light');
		expect(root.dataset.theme).toBe('light');
		stop();
	});

	test('start() defaults to dark', async () => {
		const { app, root } = setup('');
		const stop = app.start();
		expect(app.theme).toBe('dark');
		expect(root.dataset.theme).toBe('dark');
		stop();
	});

	test("setTheme('light') persists and sets data-theme", () => {
		const { app, storage, root } = setup('');
		app.setTheme('light');
		expect(app.theme).toBe('light');
		expect(storage!.getItem('farsight.theme')).toBe('light');
		expect(root.dataset.theme).toBe('light');
	});

	test('a throwing storage does not break setTheme or start', () => {
		const { app, root } = setup('', { storage: new ThrowingStorage() });
		const stop = app.start();
		expect(app.theme).toBe('dark');
		expect(() => app.setTheme('light')).not.toThrow();
		expect(app.theme).toBe('light');
		expect(root.dataset.theme).toBe('light');
		stop();
	});

	test('a null storage does not break setTheme', () => {
		const { app } = setup('', { storage: null });
		expect(() => app.setTheme('light')).not.toThrow();
		expect(app.theme).toBe('light');
	});
});

describe('tryUnlock()', () => {
	test("'ok' refreshes servers and selects the id", async () => {
		const { app, server } = setup('');
		server.servers = [SERVERS[0]];
		const stop = app.start();
		await flush();
		server.unlockAdds = SERVERS[1];
		const before = server.count('/api/servers');
		const r = await app.tryUnlock('b', 'pw');
		expect(r).toBe('ok');
		expect(server.count('/api/servers')).toBe(before + 1);
		expect(app.servers.map((s) => s.id)).toEqual(['a', 'b']);
		expect(app.currentId).toBe('b');
		stop();
	});

	test("'wrong' and 'limited' leave the selection alone", async () => {
		const { app, server } = setup('');
		const stop = app.start();
		await flush();
		server.unlockStatus = 401;
		expect(await app.tryUnlock('b', 'x')).toBe('wrong');
		server.unlockStatus = 429;
		expect(await app.tryUnlock('b', 'x')).toBe('limited');
		expect(app.currentId).toBe('a');
		stop();
	});

	test('unlockMessage maps results to copy', () => {
		expect(unlockMessage('wrong')).toBe('Wrong passphrase.');
		expect(unlockMessage('limited')).toBe('Too many attempts. Try again in a few minutes.');
	});
});

describe('showToast()', () => {
	test('clears after 2600 ms', () => {
		const { app } = setup('');
		app.showToast('Link copied', 'farsight.example');
		expect(app.toast).toEqual({ title: 'Link copied', sub: 'farsight.example' });
		vi.advanceTimersByTime(2599);
		expect(app.toast).toBeDefined();
		vi.advanceTimersByTime(1);
		expect(app.toast).toBeUndefined();
	});

	test('a second toast restarts the timer', () => {
		const { app } = setup('');
		app.showToast('One');
		vi.advanceTimersByTime(2000);
		app.showToast('Two');
		vi.advanceTimersByTime(2000);
		expect(app.toast?.title).toBe('Two');
		vi.advanceTimersByTime(600);
		expect(app.toast).toBeUndefined();
	});
});

describe('views (Plan 7)', () => {
	test('openView adds a history entry; closeView goes back', async () => {
		const { app, location, back, hashChanged } = setup('#s=a');
		app.start();
		await flush();
		app.openView({ kind: 'profile', player: '111' });
		expect(app.view).toEqual({ kind: 'profile', player: '111' });
		expect(location.hash).toBe('#s=a&p=111');
		app.closeView();
		expect(app.view).toBeUndefined();
		expect(back).toHaveBeenCalledTimes(1);
		// The browser then lands back on #s=a.
		location.hash = '#s=a';
		hashChanged();
		expect(app.view).toBeUndefined();
	});

	test('browser back from an open view closes it; forward reopens it', async () => {
		const { app, location, hashChanged } = setup('#s=a');
		app.start();
		await flush();
		app.openView({ kind: 'activity' });
		location.hash = '#s=a';
		hashChanged();
		expect(app.view).toBeUndefined();
		location.hash = '#s=a&activity';
		hashChanged();
		expect(app.view).toEqual({ kind: 'activity' });
	});

	test('a linked profile opens on load and closes by replacing the hash', async () => {
		const { app, replaceState, back } = setup('#s=b&p=222');
		app.start();
		await flush();
		expect(app.currentId).toBe('b');
		expect(app.view).toEqual({ kind: 'profile', player: '222' });
		expect(replaceState).toHaveBeenLastCalledWith(KIT_STATE, '', '#s=b&p=222');
		app.closeView();
		expect(back).not.toHaveBeenCalled();
		expect(replaceState).toHaveBeenLastCalledWith(KIT_STATE, '', '#s=b');
	});

	test('opening a view over another replaces it; switching server pops the pushed entry, not merely replaces it', async () => {
		const { app, location, replaceState, back, hashChanged } = setup('#s=a');
		app.start();
		await flush();
		app.openView({ kind: 'activity' });
		app.openView({ kind: 'profile', player: '1' });
		expect(replaceState).toHaveBeenLastCalledWith(KIT_STATE, '', '#s=a&p=1');
		expect(location.hash).toBe('#s=a&p=1');
		app.select('b');
		expect(app.view).toBeUndefined();
		// Fix round 1: the pushed entry is popped (history.back()) instead of
		// replaced outright, which would otherwise leave the entry beneath it
		// (the pre-view "#s=a") sitting right behind "#s=b" — so Back would
		// show a stale "#s=a" while the app still displays server b.
		expect(back).toHaveBeenCalledTimes(1);
		expect(location.hash).toBe('#s=a&p=1'); // unchanged until the pop lands
		// The browser's back() lands on the pre-view entry; we fix up its hash.
		location.hash = '#s=a';
		hashChanged();
		expect(location.hash).toBe('#s=b');
		expect(app.currentId).toBe('b');
	});

	test('closing a view reopened by browser forward goes back through it too (pushed set by syncView)', async () => {
		const { app, location, back, hashChanged } = setup('#s=a');
		app.start();
		await flush();
		app.openView({ kind: 'profile', player: '1' }); // pushes #s=a&p=1
		app.closeView(); // back() #1; pushed reset to false
		expect(back).toHaveBeenCalledTimes(1);
		location.hash = '#s=a';
		hashChanged();
		expect(app.view).toBeUndefined();
		// Browser forward re-enters the view's own history entry.
		location.hash = '#s=a&p=1';
		hashChanged();
		expect(app.view).toEqual({ kind: 'profile', player: '1' });
		// Closing again must go back through it (not replace, which would be
		// a no-op landing on the same "#s=a" and never actually move back).
		app.closeView();
		expect(back).toHaveBeenCalledTimes(2);
	});

	test('a hash for another server is ignored', async () => {
		const { app, location, hashChanged } = setup('#s=a');
		app.start();
		await flush();
		location.hash = '#s=b&activity';
		hashChanged();
		expect(app.view).toBeUndefined();
	});
});

describe('profile refresh (fix round 1)', () => {
	test('opening a profile fetches it at once', async () => {
		const { app, server } = setup('#s=a');
		server.profiles['a/1'] = makeProfile({ online: true });
		app.start();
		await flush();
		app.openView({ kind: 'profile', player: '1' });
		await flush();
		expect(app.profile).toMatchObject({ id: '1', online: true });
		expect(app.profileFailed).toBe(false);
	});

	test('a card poll quietly refreshes the open profile, and a status change lands without reopening', async () => {
		const { app, server } = setup('#s=a');
		server.profiles['a/1'] = makeProfile({ online: true, since: '2026-09-30T09:00:00Z' });
		app.start();
		await flush();
		app.openView({ kind: 'profile', player: '1' });
		await flush();
		expect(app.profile?.online).toBe(true);
		// The player goes offline; the next card poll's quiet profile refresh
		// picks it up without the profile view being reopened.
		server.profiles['a/1'] = makeProfile({ online: false, lastSeen: '2026-09-30T09:40:00Z' });
		await vi.advanceTimersByTimeAsync(15_000);
		await flush();
		expect(app.profile?.online).toBe(false);
		expect(app.profile?.lastSeen).toBe('2026-09-30T09:40:00Z');
	});

	test('a quiet refresh never clears the shown profile while the new fetch is in flight', async () => {
		const { app, server } = setup('#s=a');
		server.profiles['a/1'] = makeProfile();
		app.start();
		await flush();
		app.openView({ kind: 'profile', player: '1' });
		await flush();
		const shown = app.profile;
		expect(shown).not.toBeNull();
		let release!: () => void;
		const gate = new Promise<void>((r) => (release = r));
		server.override = async (call) => {
			if (call.path === '/api/servers/a/players/1') await gate;
			return server.route(call);
		};
		await vi.advanceTimersByTimeAsync(15_000);
		// The quiet refresh is in flight; the panel still shows the old data.
		expect(app.profile).toBe(shown);
		release();
		await flush();
		expect(app.profile).not.toBeUndefined();
	});

	test('does not refetch the profile while the tab is hidden', async () => {
		const { app, server, visibility } = setup('#s=a');
		server.profiles['a/1'] = makeProfile();
		app.start();
		await flush();
		app.openView({ kind: 'profile', player: '1' });
		await flush();
		visibility.isVisible = false;
		const before = server.count('/api/servers/a/players/1');
		await vi.advanceTimersByTimeAsync(15_000);
		expect(server.count('/api/servers/a/players/1')).toBe(before);
		visibility.show();
		await flush();
		expect(server.count('/api/servers/a/players/1')).toBe(before + 1);
	});

	test('closing the profile discards a slow fetch still in flight', async () => {
		const { app, server } = setup('#s=a');
		let release!: () => void;
		const gate = new Promise<void>((r) => (release = r));
		server.override = async (call) => {
			if (call.path === '/api/servers/a/players/1') await gate;
			return server.route(call);
		};
		server.profiles['a/1'] = makeProfile();
		app.start();
		await flush();
		app.openView({ kind: 'profile', player: '1' });
		app.closeView();
		release();
		await flush();
		// The late response is discarded: nothing reappears after closing.
		expect(app.profile).toBeUndefined();
	});

	test('switching server discards a slow fetch for the old one', async () => {
		const { app, server } = setup('#s=a');
		let release!: () => void;
		const gate = new Promise<void>((r) => (release = r));
		server.override = async (call) => {
			if (call.path === '/api/servers/a/players/1') await gate;
			return server.route(call);
		};
		server.profiles['a/1'] = makeProfile();
		app.start();
		await flush();
		app.openView({ kind: 'profile', player: '1' });
		app.select('b');
		release();
		await flush();
		expect(app.profile).toBeUndefined();
	});

	test('a failed fetch sets profileFailed; retryProfile() tries again at once', async () => {
		const { app, server } = setup('#s=a');
		server.profiles['a/1'] = 500;
		app.start();
		await flush();
		app.openView({ kind: 'profile', player: '1' });
		await flush();
		expect(app.profileFailed).toBe(true);
		server.profiles['a/1'] = makeProfile();
		app.retryProfile();
		await flush();
		expect(app.profileFailed).toBe(false);
		expect(app.profile).toMatchObject({ id: '1' });
	});
});

describe('activity refresh (fix round 1)', () => {
	test('opening the Activity view fetches the newest page and today at once', async () => {
		const { app, server } = setup('#s=a');
		server.activity['a'] = makeActivityPage([act('player_join', '2026-09-30T09:50:00Z', { name: 'Bjorn' })]);
		server.today['a'] = makeToday([{ id: '1', name: 'Ragnar', online: true, spans: [{ since: '2026-09-30T09:00:00Z' }] }]);
		app.start();
		await flush();
		app.openView({ kind: 'activity' });
		await flush();
		expect(app.activityPages[0]?.events.map((e) => e.name)).toEqual(['Bjorn']);
		expect(app.today?.players[0]).toMatchObject({ name: 'Ragnar', online: true });
		expect(app.activityFailed).toBe(false);
	});

	test('a card poll quietly merges a new event at the top and ends a left player’s bar, keeping pages loaded via "Show earlier"', async () => {
		const { app, server } = setup('#s=a');
		server.activity['a'] = makeActivityPage([act('player_join', '2026-09-30T09:50:00Z', { name: 'Bjorn' })], {
			from: '2026-09-27T00:00:00Z',
			earliest: '2026-09-20T00:00:00Z'
		});
		server.today['a'] = makeToday([{ id: '1', name: 'Ragnar', online: true, spans: [{ since: '2026-09-30T09:00:00Z' }] }]);
		app.start();
		await flush();
		app.openView({ kind: 'activity' });
		await flush();
		// "Show earlier" loads a second (older) page.
		const before = server.calls.length;
		await app.loadEarlierActivity();
		expect(server.calls.length).toBeGreaterThan(before);
		expect(app.activityPages.length).toBe(2);
		const earlierPage = app.activityPages[1];

		// Ragnar leaves (his span gets an `until`), and a new join appears in
		// the server's newest page; the next card poll picks both up.
		server.today['a'] = makeToday([
			{ id: '1', name: 'Ragnar', online: false, spans: [{ since: '2026-09-30T09:00:00Z', until: '2026-09-30T09:45:00Z' }] }
		]);
		server.activity['a'] = makeActivityPage(
			[
				act('player_join', '2026-09-30T10:00:00Z', { name: 'Astrid' }),
				act('player_join', '2026-09-30T09:50:00Z', { name: 'Bjorn' })
			],
			{ from: '2026-09-27T00:00:00Z', earliest: '2026-09-20T00:00:00Z' }
		);
		await vi.advanceTimersByTimeAsync(CARD_EVERY_MS);
		await flush();

		expect(app.activityPages[0]?.events.map((e) => e.name)).toEqual(['Astrid', 'Bjorn']);
		expect(app.today?.players[0]).toMatchObject({ online: false });
		expect(app.today?.players[0]?.spans[0]?.until).toBe('2026-09-30T09:45:00Z');
		// The page loaded by "Show earlier" is untouched by the poll refresh.
		expect(app.activityPages.length).toBe(2);
		expect(app.activityPages[1]).toBe(earlierPage);
	});

	test('does not refetch the activity while the tab is hidden', async () => {
		const { app, server, visibility } = setup('#s=a');
		server.activity['a'] = makeActivityPage();
		server.today['a'] = makeToday();
		app.start();
		await flush();
		app.openView({ kind: 'activity' });
		await flush();
		visibility.isVisible = false;
		const before = server.count('/api/servers/a/activity');
		await vi.advanceTimersByTimeAsync(CARD_EVERY_MS);
		expect(server.count('/api/servers/a/activity')).toBe(before);
		visibility.show();
		await flush();
		expect(server.count('/api/servers/a/activity')).toBe(before + 1);
	});

	test('closing the Activity view discards a slow fetch still in flight', async () => {
		const { app, server } = setup('#s=a');
		let release!: () => void;
		const gate = new Promise<void>((r) => (release = r));
		server.override = async (call) => {
			if (call.path === '/api/servers/a/activity') await gate;
			return server.route(call);
		};
		server.activity['a'] = makeActivityPage();
		server.today['a'] = makeToday();
		app.start();
		await flush();
		app.openView({ kind: 'activity' });
		app.closeView();
		release();
		await flush();
		expect(app.activityPages).toEqual([]);
	});

	test('a failed fetch sets activityFailed; retryActivity() tries again at once', async () => {
		const { app, server } = setup('#s=a');
		server.activity['a'] = 500;
		server.today['a'] = makeToday();
		app.start();
		await flush();
		app.openView({ kind: 'activity' });
		await flush();
		expect(app.activityFailed).toBe(true);
		server.activity['a'] = makeActivityPage();
		app.retryActivity();
		await flush();
		expect(app.activityFailed).toBe(false);
		expect(app.activityPages.length).toBe(1);
	});
});

describe('shared filters reset on server switch (fix round 1)', () => {
	test('old-server people IDs and category toggles never carry over to the new one', async () => {
		const { app } = setup('#s=a');
		app.start();
		await flush();
		filters.togglePerson('111');
		filters.toggle('death');
		expect(filters.active).toBe(true);
		app.select('b');
		await flush();
		expect(filters.people).toEqual([]);
		expect(filters.off).toEqual([]);
		expect(filters.active).toBe(false);
	});
});
