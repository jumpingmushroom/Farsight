// Shared e2e helpers. The base URL comes from global-setup.ts, which starts
// a seeded farsight on a free port and exports it as process.env.BASE_URL
// (workers inherit the runner's environment).
import { test as base, expect, type BrowserContext, type Page } from '@playwright/test';
import { gzipSync } from 'node:zlib';

import type { Card } from '../../src/lib/types';

/**
 * Wires up the guard's detectors on `context` (and its pages present or
 * future) and returns the live, ever-growing list of problems it records:
 * any uncaught page error, any console message about the Content Security
 * Policy ("Content Security Policy", "Refused to"), and any request to an
 * origin other than `origin` (data: and blob: URLs are local).
 *
 * Exported (with `checkGuard`) so the guard self-tests (desktop.spec.ts) can
 * drive and inspect the guard directly, instead of only ever observing it
 * indirectly through the `guard` fixture's automatic pass/fail.
 */
export function watchGuard(context: BrowserContext, origin: string): string[] {
	const problems: string[] = [];
	const watch = (p: Page) => {
		p.on('pageerror', (e) => problems.push(`page error: ${e.message}`));
		p.on('console', (m) => {
			const t = m.text();
			if (t.includes('Content Security Policy') || t.includes('Refused to')) problems.push(`console ${m.type()}: ${t}`);
		});
	};
	context.pages().forEach(watch);
	context.on('page', watch);
	context.on('request', (r) => {
		const u = r.url();
		if (u.startsWith('data:') || u.startsWith('blob:') || u === 'about:blank') return;
		if (new URL(u).origin !== origin) problems.push(`request to another origin: ${u}`);
	});
	return problems;
}

/**
 * Fails when `problems` isn't empty. Exported so the guard self-tests can
 * call it directly and assert on the thrown message, rather than a bare
 * `test.fail()` — which only asserts *that* the test fails, not *why*, so it
 * would still pass (as "expected to fail") if the guard fired for the wrong
 * reason, or didn't fire at all but something else in the test threw.
 */
export function checkGuard(problems: string[]): void {
	expect(problems, 'page errors, CSP violations or third-party requests').toEqual([]);
}

/**
 * Every spec uses this `test`. Besides the base URL it adds an automatic
 * guard (see `watchGuard`/`checkGuard`) that fails the test on any uncaught
 * page error, CSP violation or third-party request, watching every page of
 * the test's context.
 */
export const test = base.extend<{ guard: void }>({
	baseURL: async ({}, use) => {
		await use(process.env.BASE_URL);
	},
	guard: [
		async ({ context }, use) => {
			const origin = new URL(process.env.BASE_URL ?? 'http://127.0.0.1').origin;
			const problems = watchGuard(context, origin);
			await use();
			checkGuard(problems);
		},
		{ auto: true }
	]
});
export { expect };

export const PASS: Record<string, string> = { demo: 'demo-pass', quiet: 'quiet-pass', swap: 'swap-pass' };

/**
 * Agent tokens for the servers global-setup.ts seeds directly (not through
 * the shared `FARSIGHT_SEED_TOKEN`/demo keep-alive): `postSnapshot`'s
 * default.
 */
export const SEED_TOKENS: Record<string, string> = { demo: 'demo-token', swap: 'swap-token' };

/**
 * Opens a share link for `server` and waits until the app has consumed it:
 * the hash is back to `#s=<server>` and a shell is up.
 */
export async function unlock(page: Page, server = 'demo'): Promise<void> {
	// A hash-only navigation doesn't reload an open page, so start clean.
	if (page.url() !== 'about:blank') await page.goto('about:blank');
	await page.goto(`/#s=${server}&k=${PASS[server]}`);
	await expect(page).toHaveURL(new RegExp(`/#s=${server}$`));
	await expect(page.locator('main.shell')).toBeVisible();
}

/** A single map pin (not a cluster) by its tooltip, e.g. "Portal · copper". */
export function pin(page: Page, title: string) {
	return page.locator(`.leaflet-marker-icon.fs-pin[title="${title}"]`);
}

/** Map pins (not clusters) whose tooltip starts with `kicker ·`. */
export function pinsOfKind(page: Page, kicker: string) {
	return page.locator(`.leaflet-marker-icon.fs-pin[title^="${kicker} ·"]`);
}

/**
 * Serves `/api/servers/demo` as the real card passed through `edit`, so a
 * state test starts from what the server actually returns.
 */
export async function overrideCard(page: Page, edit: (c: Card) => Card): Promise<void> {
	await page.route('**/api/servers/demo', async (route) => {
		const res = await route.fetch();
		const card = (await res.json()) as Card;
		await route.fulfill({ response: res, json: edit(card) });
	});
}

/** An RFC 3339 time `sec` seconds ago. */
export function ago(sec: number): string {
	return new Date(Date.now() - sec * 1000).toISOString();
}

/**
 * Posts a snapshot to a seeded server, gzip + bearer, as a real agent would
 * (global-setup.ts's own `postHeartbeat` does the same for events). Uses
 * the base URL global-setup.ts exports on `process.env`, which worker
 * processes inherit; `token` defaults to `SEED_TOKENS[server]`.
 */
export async function postSnapshot(server: string, snapshot: unknown, token = SEED_TOKENS[server]): Promise<void> {
	const body = gzipSync(JSON.stringify(snapshot));
	const res = await fetch(`${process.env.BASE_URL}/ingest/${server}/snapshot`, {
		method: 'POST',
		headers: {
			Authorization: `Bearer ${token}`,
			'Content-Type': 'application/json',
			'Content-Encoding': 'gzip'
		},
		body
	});
	if (!res.ok) throw new Error(`postSnapshot: ${res.status} ${await res.text()}`);
}

/**
 * Makes the page's `state.svelte.ts` refresh immediately instead of waiting
 * out its 15 s poll: it refetches on `visibilitychange` while the page is
 * visible (state.svelte.ts's `onVisible` handler), so this forces
 * `document.visibilityState` to `'visible'` (it's a read-only getter) before
 * dispatching the event, in case the page isn't the OS-focused tab.
 */
export async function refreshNow(page: Page): Promise<void> {
	await page.evaluate(() => {
		if (document.visibilityState !== 'visible') {
			Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true });
		}
		document.dispatchEvent(new Event('visibilitychange'));
	});
}

/** The RGB of the page's pixel at (x, y), read from a screenshot. */
export async function screenPixel(page: Page, x: number, y: number): Promise<[number, number, number]> {
	const png = await page.screenshot({ clip: { x: Math.round(x), y: Math.round(y), width: 1, height: 1 } });
	return page.evaluate(async (bytes) => {
		const bmp = await createImageBitmap(new Blob([new Uint8Array(bytes)], { type: 'image/png' }));
		const ctx = new OffscreenCanvas(1, 1).getContext('2d')!;
		ctx.drawImage(bmp, 0, 0);
		const d = ctx.getImageData(0, 0, 1, 1).data;
		return [d[0], d[1], d[2]] as [number, number, number];
	}, Array.from(png as unknown as Uint8Array));
}

/** Screen position of world point (x, z), from the world disc's on-screen box (radius 10 500 m). */
export async function worldToScreen(page: Page, x: number, z: number): Promise<{ x: number; y: number }> {
	const box = (await page.locator('path.world-disc').boundingBox())!;
	const r = box.width / 2;
	return { x: box.x + r + (x / 10500) * r, y: box.y + box.height / 2 - (z / 10500) * r };
}

/** Waits until every tile image on the map has loaded. */
export async function tilesLoaded(page: Page): Promise<void> {
	await expect
		.poll(() =>
			page
				.locator('img.leaflet-tile')
				.evaluateAll((imgs) => imgs.length > 0 && imgs.every((i) => (i as HTMLImageElement).complete && (i as HTMLImageElement).naturalWidth === 256))
		)
		.toBe(true);
}
