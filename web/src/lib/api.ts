// Typed fetch client for the Plan 4a JSON API (see the spec's "API contract"
// section). Every request is same-origin only; GETs are never cached so the
// UI always sees the latest state.

import { decodeExplored } from './explored';
import type { ActivityPage, Card, Profile, ServerSummary, SnapshotView, TodaySessions } from './types';

export class ApiError extends Error {
	status: number;
	constructor(status: number, message: string) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
	}
}

// Every request carries a timeout, so a hung connection never wedges the
// single-flight poll in state.svelte.ts (it just ends the flight with an
// ApiError, and the next tick retries). AbortController + setTimeout rather
// than AbortSignal.timeout, which Safari before 16 lacks. The timer races
// the request itself (not just its abort signal) so a fake/real fetch that
// never settles still ends the request once the timeout elapses.
const SERVERS_TIMEOUT_MS = 20_000;
const CARD_TIMEOUT_MS = 20_000;
const SNAPSHOT_TIMEOUT_MS = 60_000;
const UNLOCK_TIMEOUT_MS = 15_000;
const PROFILE_TIMEOUT_MS = 20_000;
const ACTIVITY_TIMEOUT_MS = 20_000;

// `run` covers the whole request — fetch() resolving is not enough, since a
// server can send headers promptly and then stall the body — so the caller
// does its status handling and its res.json()/res.text() call *inside*
// `run`, all under the same race. The AbortController stays live for that
// entire span: the same `signal` that aborts the fetch also aborts an
// in-flight body read on the response it returned. The timer is cleared
// only once `run` (headers *and* body) has fully settled, one way or another.
function withTimeout<T>(ms: number, run: (signal: AbortSignal) => Promise<T>): Promise<T> {
	const ctrl = new AbortController();
	let timer: ReturnType<typeof setTimeout>;
	const timeout = new Promise<never>((_, reject) => {
		timer = setTimeout(() => {
			ctrl.abort();
			reject(new ApiError(0, 'timeout'));
		}, ms);
	});
	return Promise.race([run(ctrl.signal), timeout]).finally(() => clearTimeout(timer));
}

function jsonInit(signal: AbortSignal): RequestInit {
	return { credentials: 'same-origin', cache: 'no-store', signal };
}

export async function listServers(f: typeof fetch = fetch, timeoutMs = SERVERS_TIMEOUT_MS): Promise<ServerSummary[]> {
	return withTimeout(timeoutMs, async (signal) => {
		const res = await f('/api/servers', jsonInit(signal));
		if (!res.ok) throw new ApiError(res.status, await res.text());
		const body = (await res.json()) as { servers: ServerSummary[] };
		return body.servers;
	});
}

export async function getCard(id: string, f: typeof fetch = fetch, timeoutMs = CARD_TIMEOUT_MS): Promise<Card> {
	return withTimeout(timeoutMs, async (signal) => {
		const res = await f(`/api/servers/${encodeURIComponent(id)}`, jsonInit(signal));
		if (!res.ok) throw new ApiError(res.status, await res.text());
		return (await res.json()) as Card;
	});
}

export async function getSnapshot(
	id: string,
	f: typeof fetch = fetch,
	timeoutMs = SNAPSHOT_TIMEOUT_MS
): Promise<SnapshotView | null> {
	return withTimeout(timeoutMs, async (signal) => {
		const res = await f(`/api/servers/${encodeURIComponent(id)}/snapshot`, jsonInit(signal));
		if (res.status === 404) return null;
		if (!res.ok) throw new ApiError(res.status, await res.text());
		const snap = (await res.json()) as SnapshotView;
		const mask = await decodeExplored(snap.explored);
		return mask ? { ...snap, mask } : snap;
	});
}

/** A player's profile; null when the server doesn't know them (404). */
export async function getProfile(
	id: string,
	player: string,
	f: typeof fetch = fetch,
	timeoutMs = PROFILE_TIMEOUT_MS
): Promise<Profile | null> {
	return withTimeout(timeoutMs, async (signal) => {
		const res = await f(`/api/servers/${encodeURIComponent(id)}/players/${encodeURIComponent(player)}`, jsonInit(signal));
		if (res.status === 404) return null;
		if (!res.ok) throw new ApiError(res.status, await res.text());
		return (await res.json()) as Profile;
	});
}

/** Three local days of activity ending at `before` (the start of an earlier page), or at the end of today. */
export async function getActivity(
	id: string,
	before?: string,
	f: typeof fetch = fetch,
	timeoutMs = ACTIVITY_TIMEOUT_MS
): Promise<ActivityPage> {
	const q = before ? `?before=${encodeURIComponent(before)}` : '';
	return withTimeout(timeoutMs, async (signal) => {
		const res = await f(`/api/servers/${encodeURIComponent(id)}/activity${q}`, jsonInit(signal));
		if (!res.ok) throw new ApiError(res.status, await res.text());
		return (await res.json()) as ActivityPage;
	});
}

export async function getSessionsToday(id: string, f: typeof fetch = fetch, timeoutMs = ACTIVITY_TIMEOUT_MS): Promise<TodaySessions> {
	return withTimeout(timeoutMs, async (signal) => {
		const res = await f(`/api/servers/${encodeURIComponent(id)}/sessions/today`, jsonInit(signal));
		if (!res.ok) throw new ApiError(res.status, await res.text());
		return (await res.json()) as TodaySessions;
	});
}

export async function unlock(
	server: string,
	passphrase: string,
	f: typeof fetch = fetch,
	timeoutMs = UNLOCK_TIMEOUT_MS
): Promise<'ok' | 'wrong' | 'limited'> {
	// A timeout here surfaces through the generic catch in UnlockDialog as the
	// network-error message, not as "wrong passphrase" (unlock() only returns
	// 'wrong' on an actual 401; a timeout throws instead).
	return withTimeout(timeoutMs, async (signal) => {
		const res = await f('/api/unlock', {
			method: 'POST',
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ server, passphrase }),
			signal
		});
		if (res.status === 204) return 'ok';
		if (res.status === 401) return 'wrong';
		if (res.status === 429) return 'limited';
		throw new ApiError(res.status, await res.text());
	});
}

/** The fog tiles of tile set `key` under fog key `fog` (spec 2026-10-01 §2). */
export function tileUrl(id: string, key: string, fog: string): string {
	return `/tiles/${encodeURIComponent(id)}/${encodeURIComponent(key)}/${encodeURIComponent(fog)}/{z}/{x}/{y}.png`;
}
