// Map and panel states (desktop). Each test serves /api/servers/demo as a
// modified copy of the real card (page.route + route.fetch), so everything
// else about the card stays what the seeded server returns.
import type { Page } from '@playwright/test';
import type { Card } from '../../src/lib/types';
import { ago, expect, overrideCard, test, unlock } from './helpers';

const banner = (page: Page) => page.getByTestId('state-banner');

test('offline: banner and "Server is resting"', async ({ page }) => {
	await overrideCard(page, (c): Card => {
		const { joinCode: _code, joinCodeAt: _at, ...rest } = c;
		return { ...rest, status: 'offline', players: 0, online: [], lastHeartbeat: ago(3 * 3600 + 5 * 60) };
	});
	await unlock(page);
	await expect(banner(page)).toContainText('Server offline · last seen online');
	await expect(banner(page)).toContainText('(3 h');
	await expect(page.getByRole('heading', { name: 'Server is resting' })).toBeVisible();
	await expect(page.locator('.pill', { hasText: 'Map updated' })).toHaveCount(0);
});

test('online with nobody on: "The longhouse is quiet"', async ({ page }) => {
	await overrideCard(page, (c) => ({ ...c, players: 0, online: [] }));
	await unlock(page);
	await expect(page.getByRole('heading', { name: 'The longhouse is quiet' })).toBeVisible();
	await expect(banner(page)).toHaveCount(0);
	await expect(page.getByRole('tab', { name: 'Online · 0' })).toBeVisible();
});

test('tiles rendering: the charting card', async ({ page }) => {
	await overrideCard(page, (c) => ({ ...c, tiles: { ...c.tiles, state: 'rendering', done: 389, total: 1365 } }));
	await unlock(page);
	const charting = page.getByTestId('charting-card');
	await expect(charting).toContainText('Charting the world for the first time');
	await expect(charting).toContainText('389 of 1,365 tiles');
	await expect(charting.getByRole('progressbar', { name: 'Charting progress' })).toHaveAttribute('aria-valuenow', '28');
	// Markers stay hidden and search is off while charting.
	await expect(page.getByRole('combobox', { name: 'Search the map' })).toBeDisabled();
	await expect(page.locator('.leaflet-marker-icon')).toHaveCount(0);
});

test('a 4 h old save: the stale banner', async ({ page }) => {
	await overrideCard(page, (c) => ({ ...c, world: { ...c.world!, savedAt: ago(4 * 3600 + 5 * 60) } }));
	await unlock(page);
	await expect(banner(page)).toContainText('Map data may be out of date');
	await expect(banner(page)).toContainText(/Map last updated 4 h \d+ min ago/);
	await expect(banner(page)).toContainText('Autosave may be failing');
	await expect(page.locator('.pill', { hasText: 'Map updated' })).toHaveCount(0);
});

test('tiles refused: "Can’t draw this world’s map yet"', async ({ page }) => {
	await overrideCard(page, (c) => ({ ...c, tiles: { ...c.tiles, state: 'refused', done: 0 } }));
	await unlock(page);
	await expect(banner(page)).toContainText('Can’t draw this world’s map yet');
	// Markers and the online list still work.
	await expect(page.locator('.leaflet-marker-icon').first()).toBeVisible();
	await expect(page.getByRole('list', { name: 'Online now' }).getByRole('listitem')).toHaveCount(3);
});

// Fog is the core promise: the tiles carry it, and no tile is requested
// before the snapshot names its fog key.
test('fog guard: no tiles while the snapshot is loading, then fog tiles', async ({ page }) => {
	// .png only: the biome grid (Task 4) is a single /tiles/{id}/{key}/biomes
	// fetch keyed by the card's tile key, not gated by the snapshot's fog
	// key, so it isn't part of this fog-key guard.
	const tileRequests: string[] = [];
	page.on('request', (r) => {
		if (r.url().includes('/tiles/') && r.url().endsWith('.png')) tileRequests.push(r.url());
	});
	let release!: () => void;
	const held = new Promise<void>((r) => (release = r));
	let snapshotRequested = false;
	await page.route('**/api/servers/demo/snapshot', async (route) => {
		snapshotRequested = true;
		await held;
		await route.continue();
	});
	await unlock(page);
	// The card is in (the server card shows), the snapshot is held back ~3 s.
	await expect(page.getByRole('region', { name: 'Server' }).locator('dd.players')).toHaveText('3/10');
	expect(snapshotRequested).toBe(true);
	for (let i = 0; i < 6; i++) {
		await expect(page.locator('img.leaflet-tile')).toHaveCount(0);
		await page.waitForTimeout(500);
	}
	expect(tileRequests).toEqual([]);
	release();
	await expect(page.locator('img.leaflet-tile-loaded').first()).toBeVisible();
	for (const u of tileRequests) expect(new URL(u).pathname).toMatch(/^\/tiles\/demo\/[^/]+\/[0-9a-f]{16}\//);
});

test('fog guard: a failing snapshot fetch never requests tiles', async ({ page }) => {
	let failures = 0;
	await page.route('**/api/servers/demo/snapshot', async (route) => {
		failures++;
		await route.fulfill({ status: 502, body: 'bad gateway' });
	});
	await unlock(page);
	await expect(page.getByRole('region', { name: 'Server' }).locator('dd.players')).toHaveText('3/10');
	await expect.poll(() => failures).toBeGreaterThan(0);
	await page.waitForTimeout(1500);
	await expect(page.locator('img.leaflet-tile')).toHaveCount(0);
});

// Review fix: losing contact with Farsight used to be invisible (the UI kept
// saying "Online" and the clocks kept ticking). The page's clock is faked so
// the 15 s polls can be stepped through without waiting them out.
test('lost contact: "Reconnecting…" once polls have failed for 45 s, the world clock stops, and both recover', async ({ page }) => {
	await page.clock.install();
	await unlock(page);
	const pill = page.getByTestId('weather-pill');
	await expect(pill).toBeVisible();
	const reconnecting = page.getByTestId('connection-banner');
	await page.route('**/api/servers**', (route) => route.abort('internetdisconnected'));
	// Failures at 15, 30 and 45 s: the streak is 30 s old, still a blip.
	for (let i = 0; i < 3; i++) {
		await page.clock.runFor(15_000);
		await page.waitForTimeout(200);
	}
	await expect(reconnecting).toHaveCount(0);
	// At 60 s the streak is 45 s old, give or take how long each failure
	// took to land; by 75 s it is past any doubt.
	await page.clock.runFor(30_000);
	await expect(reconnecting).toContainText('Reconnecting…');
	await expect(page.locator('.pill', { hasText: 'Map updated' })).toHaveCount(0);
	const frozen = await pill.innerText();
	await page.clock.runFor(60_000);
	await expect(pill).toHaveText(frozen);

	await page.unroute('**/api/servers**');
	await page.clock.runFor(15_000);
	await expect(reconnecting).toHaveCount(0);
	await expect(page.locator('.pill', { hasText: 'Map updated' })).toBeVisible();
	await page.clock.runFor(60_000);
	await expect(pill).not.toHaveText(frozen);
});
