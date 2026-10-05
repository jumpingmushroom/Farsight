import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { ApiError, getActivity, getCard, getProfile, getSessionsToday, getSnapshot, listServers, tileUrl, unlock } from './api';
import type { ServerSummary } from './types';

function jsonResponse(body: unknown, status = 200): Response {
	return new Response(JSON.stringify(body), {
		status,
		headers: { 'Content-Type': 'application/json' }
	});
}

function textResponse(body: string, status: number): Response {
	// The Fetch Response constructor rejects a body on null-body statuses
	// (204/205/304), matching the real 204 the API returns on success.
	const nullBody = status === 204 || status === 205 || status === 304;
	return new Response(nullBody ? null : body, { status });
}

describe('listServers', () => {
	test('returns .servers', async () => {
		const servers: ServerSummary[] = [{ id: 'a', name: 'A', status: 'online', players: 1, maxPlayers: 10 }];
		const fake = vi.fn().mockResolvedValue(jsonResponse({ servers }));
		const result = await listServers(fake as unknown as typeof fetch);
		expect(result).toEqual(servers);
		const [url, init] = fake.mock.calls[0];
		expect(url).toBe('/api/servers');
		expect(init).toMatchObject({ credentials: 'same-origin', cache: 'no-store' });
	});
});

describe('getSnapshot', () => {
	test('404 -> null', async () => {
		const fake = vi.fn().mockResolvedValue(textResponse('not found', 404));
		const result = await getSnapshot('a', fake as unknown as typeof fetch);
		expect(result).toBeNull();
	});
	test('200 -> the snapshot', async () => {
		const snap = {
			savedAt: '2026-09-30T00:00:00Z',
			fogKey: '0123456789abcdef',
			explored: { source: 'zones', cell: 12, size: 2048, bits: '' },
			markers: [],
			locations: [],
			bases: [],
			players: []
		};
		const fake = vi.fn().mockResolvedValue(jsonResponse(snap));
		const result = await getSnapshot('a', fake as unknown as typeof fetch);
		expect(result).toEqual(snap);
	});

	test('200 -> the explored mask decoded onto the snapshot', async () => {
		const bits = new Uint8Array(512 * 1024);
		bits[1] = 4;
		const gz = new Uint8Array(await new Response(new Blob([bits]).stream().pipeThrough(new CompressionStream('gzip'))).arrayBuffer());
		let b64 = '';
		for (const b of gz) b64 += String.fromCharCode(b);
		const snap = {
			savedAt: '2026-09-30T00:00:00Z',
			fogKey: '0123456789abcdef',
			explored: { source: 'tables', cell: 12, size: 2048, bits: btoa(b64) },
			markers: [],
			locations: [],
			bases: [],
			players: []
		};
		const fake = vi.fn().mockResolvedValue(jsonResponse(snap));
		const result = await getSnapshot('a', fake as unknown as typeof fetch);
		expect(result?.fogKey).toBe('0123456789abcdef');
		expect(result?.mask?.length).toBe(512 * 1024);
		expect(result?.mask?.[1]).toBe(4);
	});
});

describe('getCard', () => {
	test('404 -> ApiError(404)', async () => {
		const fake = vi.fn().mockImplementation(async () => textResponse('locked', 404));
		try {
			await getCard('a', fake as unknown as typeof fetch);
			throw new Error('expected throw');
		} catch (e) {
			expect(e).toBeInstanceOf(ApiError);
			expect((e as ApiError).status).toBe(404);
		}
	});
	test('other non-2xx -> ApiError with status and text', async () => {
		const fake = vi.fn().mockImplementation(async () => textResponse('boom', 500));
		try {
			await getCard('a', fake as unknown as typeof fetch);
			throw new Error('expected throw');
		} catch (e) {
			expect(e).toBeInstanceOf(ApiError);
			expect((e as ApiError).status).toBe(500);
			expect((e as ApiError).message).toBe('boom');
		}
	});
});

describe('unlock', () => {
	test('204 -> ok', async () => {
		const fake = vi.fn().mockResolvedValue(textResponse('', 204));
		expect(await unlock('a', 'pw', fake as unknown as typeof fetch)).toBe('ok');
	});
	test('401 -> wrong', async () => {
		const fake = vi.fn().mockResolvedValue(jsonResponse({ error: 'wrong passphrase' }, 401));
		expect(await unlock('a', 'pw', fake as unknown as typeof fetch)).toBe('wrong');
	});
	test('429 -> limited', async () => {
		const fake = vi.fn().mockResolvedValue(jsonResponse({ error: 'too many attempts' }, 429));
		expect(await unlock('a', 'pw', fake as unknown as typeof fetch)).toBe('limited');
	});
	test('sends a JSON body with credentials same-origin', async () => {
		const fake = vi.fn().mockResolvedValue(textResponse('', 204));
		await unlock('example', 'sekrit', fake as unknown as typeof fetch);
		const [url, init] = fake.mock.calls[0];
		expect(url).toBe('/api/unlock');
		expect(init.method).toBe('POST');
		expect(init.credentials).toBe('same-origin');
		expect(init.headers).toMatchObject({ 'Content-Type': 'application/json' });
		expect(JSON.parse(init.body)).toEqual({ server: 'example', passphrase: 'sekrit' });
	});
});

describe('tileUrl', () => {
	test('escapes id, key and fog key', () => {
		expect(tileUrl('a b', 'k', 'f/0')).toBe('/tiles/a%20b/k/f%2F0/{z}/{x}/{y}.png');
	});
});

describe('timeouts', () => {
	beforeEach(() => vi.useFakeTimers());
	afterEach(() => vi.useRealTimers());

	// Never resolves or rejects on its own; only the timeout ends the request.
	function hangingFetch() {
		return vi.fn(() => new Promise<Response>(() => {})) as unknown as typeof fetch;
	}

	test('getCard: a hung request rejects with ApiError(0, "timeout") after the default 20 s', async () => {
		const fake = hangingFetch();
		const p = getCard('a', fake);
		const assertion = expect(p).rejects.toMatchObject({ status: 0, message: 'timeout' });
		await vi.advanceTimersByTimeAsync(20_000);
		await assertion;
	});

	test('an explicit shorter timeout overrides the default', async () => {
		const fake = hangingFetch();
		const p = getCard('a', fake, 1000);
		const assertion = expect(p).rejects.toBeInstanceOf(ApiError);
		await vi.advanceTimersByTimeAsync(999);
		// Not yet: the default (20 s) would have fired, but the timeout hasn't.
		let settled = false;
		p.catch(() => (settled = true));
		await Promise.resolve();
		expect(settled).toBe(false);
		await vi.advanceTimersByTimeAsync(1);
		await assertion;
	});

	test('listServers times out at the default 20 s', async () => {
		const fake = hangingFetch();
		const p = listServers(fake);
		const assertion = expect(p).rejects.toMatchObject({ status: 0, message: 'timeout' });
		await vi.advanceTimersByTimeAsync(20_000);
		await assertion;
	});

	test('getSnapshot times out at the default 60 s', async () => {
		const fake = hangingFetch();
		const p = getSnapshot('a', fake);
		const assertion = expect(p).rejects.toMatchObject({ status: 0, message: 'timeout' });
		await vi.advanceTimersByTimeAsync(60_000);
		await assertion;
	});

	test('unlock times out at the default 15 s and rejects rather than resolving "wrong"', async () => {
		const fake = hangingFetch();
		const p = unlock('a', 'pw', fake);
		const assertion = expect(p).rejects.toMatchObject({ status: 0, message: 'timeout' });
		await vi.advanceTimersByTimeAsync(15_000);
		await assertion;
	});

	test('a request that resolves before its timeout is unaffected, and the timer is cleared', async () => {
		const fake = vi.fn(
			() =>
				new Promise<Response>((resolve) => {
					setTimeout(() => resolve(jsonResponse({ servers: [] })), 500);
				})
		) as unknown as typeof fetch;
		const p = listServers(fake, 1000);
		await vi.advanceTimersByTimeAsync(500);
		expect(await p).toEqual([]);
		// If the 1000 ms timer weren't cleared it would still be harmless here,
		// but this proves advancing past it doesn't throw an unhandled rejection.
		await vi.advanceTimersByTimeAsync(1000);
	});

	// A server can send headers promptly and then stall the body (a slow or
	// wedged connection after the response starts). The timeout must cover
	// the whole request — fetch() resolving is not enough — so these use a
	// fake fetch whose headers resolve immediately but whose json()/text()
	// never do.
	function headersOnlyResponse(status: number, ok = status >= 200 && status < 300) {
		return {
			ok,
			status,
			json: () => new Promise(() => {}),
			text: () => new Promise(() => {})
		} as unknown as Response;
	}

	test('getSnapshot: a stalled response body still times out at the default 60 s', async () => {
		const fake = vi.fn().mockResolvedValue(headersOnlyResponse(200)) as unknown as typeof fetch;
		const p = getSnapshot('a', fake);
		const assertion = expect(p).rejects.toMatchObject({ status: 0, message: 'timeout' });
		await vi.advanceTimersByTimeAsync(60_000);
		await assertion;
	});

	test('unlock: a stalled response body (on an unrecognised status, read via text()) still times out at the default 15 s', async () => {
		// 200 hits none of unlock's explicit status checks (204/401/429), so it
		// falls through to the `await res.text()` branch that builds the thrown ApiError.
		const fake = vi.fn().mockResolvedValue(headersOnlyResponse(200, false)) as unknown as typeof fetch;
		const p = unlock('a', 'pw', fake);
		const assertion = expect(p).rejects.toMatchObject({ status: 0, message: 'timeout' });
		await vi.advanceTimersByTimeAsync(15_000);
		await assertion;
	});
});

describe('getProfile (Plan 7)', () => {
	test('escapes the ids; 404 -> null', async () => {
		const fake = vi.fn().mockResolvedValue(textResponse('not found', 404));
		expect(await getProfile('a b', 'Xbox/2', fake as unknown as typeof fetch)).toBeNull();
		expect(fake.mock.calls[0][0]).toBe('/api/servers/a%20b/players/Xbox%2F2');
	});
	test('200 -> the profile; 500 -> ApiError', async () => {
		const ok = vi.fn().mockResolvedValue(jsonResponse({ id: '1', name: 'A' }));
		expect(await getProfile('a', '1', ok as unknown as typeof fetch)).toMatchObject({ id: '1', name: 'A' });
		const bad = vi.fn().mockResolvedValue(textResponse('boom', 500));
		await expect(getProfile('a', '1', bad as unknown as typeof fetch)).rejects.toMatchObject({ status: 500 });
	});
});

describe('getActivity / getSessionsToday (Plan 7)', () => {
	test('the first page has no before; an earlier page passes it', async () => {
		const fake = vi.fn(async (_url: string) => jsonResponse({ events: [] }));
		await getActivity('a', undefined, fake as unknown as typeof fetch);
		await getActivity('a', '2026-09-26T22:00:00Z', fake as unknown as typeof fetch);
		expect(fake.mock.calls[0][0]).toBe('/api/servers/a/activity');
		expect(fake.mock.calls[1][0]).toBe('/api/servers/a/activity?before=2026-09-26T22%3A00%3A00Z');
	});
	test('an explicit days (fix round 2: a quiet refresh pins `from` across midnight) is passed through, with or without before', async () => {
		const fake = vi.fn(async (_url: string) => jsonResponse({ events: [] }));
		await getActivity('a', undefined, fake as unknown as typeof fetch, undefined, 4);
		await getActivity('a', '2026-09-26T22:00:00Z', fake as unknown as typeof fetch, undefined, 5);
		expect(fake.mock.calls[0][0]).toBe('/api/servers/a/activity?days=4');
		expect(fake.mock.calls[1][0]).toBe('/api/servers/a/activity?before=2026-09-26T22%3A00%3A00Z&days=5');
	});
	test('sessions today; errors throw', async () => {
		const fake = vi.fn().mockResolvedValue(jsonResponse({ players: [] }));
		await getSessionsToday('a', fake as unknown as typeof fetch);
		expect(fake.mock.calls[0][0]).toBe('/api/servers/a/sessions/today');
		const bad = vi.fn().mockResolvedValue(textResponse('not found', 404));
		await expect(getActivity('a', undefined, bad as unknown as typeof fetch)).rejects.toBeInstanceOf(ApiError);
	});
});
