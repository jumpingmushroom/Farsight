// Shared e2e helpers. The base URL comes from global-setup.ts, which starts
// a seeded farsight on a free port and exports it as process.env.BASE_URL
// (workers inherit the runner's environment).
import { test as base, expect, type BrowserContext, type Page } from '@playwright/test';
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

export const PASS: Record<string, string> = { demo: 'demo-pass', quiet: 'quiet-pass' };

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
