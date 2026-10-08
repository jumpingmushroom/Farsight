// Desktop (1440×900) e2e against the seeded `demo` and unseeded `quiet`
// servers (global-setup.ts). Numbered tests follow the Task 9 brief; the
// "carried" ones are the regressions carried over from reviews; the "fix"
// ones are regressions carried over from code review fix rounds.
import { test as pwTest } from '@playwright/test';
import type { Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import {
	checkGuard,
	expect,
	overrideCard,
	pinsOfKind,
	postSnapshot,
	refreshNow,
	screenPixel,
	test,
	tabToFirstPin,
	tilesLoaded,
	unlock,
	watchGuard,
	worldToScreen
} from './helpers';

const FIXTURE_SNAPSHOT = fileURLToPath(new URL('../fixtures/snapshot.json', import.meta.url));

const search = (page: Page) => page.getByRole('combobox', { name: 'Search the map' });
const serverCard = (page: Page) => page.getByRole('region', { name: 'Server' });
const markerCard = (page: Page) => page.getByTestId('marker-card');
const switcher = (page: Page) => page.getByRole('button', { name: /^Switch server:/ });

/** Waits until the snapshot's markers are on the map. */
async function markersReady(page: Page): Promise<void> {
	await expect(page.locator('.leaflet-marker-icon').first()).toBeVisible();
}

test('1 · unlock dialog: wrong passphrase, then unlock shows the server card', async ({ page }) => {
	await page.goto('/');
	const dialog = page.getByRole('dialog', { name: 'Unlock a server' });
	await expect(dialog).toBeVisible();
	await dialog.getByLabel('Server').fill('demo');
	await dialog.getByLabel('Passphrase').fill('not-the-passphrase');
	await dialog.getByRole('button', { name: 'Unlock' }).click();
	await expect(dialog.getByRole('alert')).toHaveText('Wrong passphrase.');

	await dialog.getByLabel('Passphrase').fill('demo-pass');
	await dialog.getByRole('button', { name: 'Unlock' }).click();
	await expect(dialog).toBeHidden();
	const card = serverCard(page);
	await expect(switcher(page)).toHaveAccessibleName('Switch server: Demo');
	await expect(card).toContainText('Online');
	await expect(card.locator('dd.players')).toHaveText('3/10');
	await expect(card).toContainText('0.221.4');
});

test('2 · share link unlocks quiet, drops the passphrase from the URL, and the switcher switches', async ({ page }) => {
	await unlock(page, 'demo');
	await page.goto('about:blank');
	await page.goto('/#s=quiet&k=quiet-pass');
	await expect(page).toHaveURL(/\/#s=quiet$/);
	expect(page.url()).not.toContain('k=');
	await expect(switcher(page)).toHaveAccessibleName('Switch server: Quiet Fjord');

	await switcher(page).click();
	const list = page.getByRole('listbox', { name: 'Servers' });
	await expect(list.getByRole('option')).toHaveCount(2);
	await expect(list.getByRole('option', { name: /Demo/ })).toBeVisible();
	await expect(list.getByRole('option', { name: /Quiet Fjord/ })).toHaveAttribute('aria-selected', 'true');
	await list.getByRole('option', { name: /Demo/ }).click();
	await expect(switcher(page)).toHaveAccessibleName('Switch server: Demo');
	await expect(page).toHaveURL(/\/#s=demo$/);
	await expect(serverCard(page).locator('dd.players')).toHaveText('3/10');
});

test('fix · switching server clears the typed search query and closes the results', async ({ page }) => {
	await unlock(page, 'demo');
	await page.goto('about:blank');
	await page.goto('/#s=quiet&k=quiet-pass');
	await expect(switcher(page)).toHaveAccessibleName('Switch server: Quiet Fjord');
	await switcher(page).click();
	await page.getByRole('listbox', { name: 'Servers' }).getByRole('option', { name: /Demo/ }).click();
	await expect(switcher(page)).toHaveAccessibleName('Switch server: Demo');
	await markersReady(page);
	await search(page).fill('copper');
	await expect(page.getByRole('listbox', { name: 'Search results' })).toBeVisible();

	await switcher(page).click();
	await page.getByRole('listbox', { name: 'Servers' }).getByRole('option', { name: /Quiet Fjord/ }).click();
	await expect(switcher(page)).toHaveAccessibleName('Switch server: Quiet Fjord');
	await expect(search(page)).toHaveValue('');
	await expect(page.getByRole('listbox', { name: 'Search results' })).toHaveCount(0);
});

test('fix · switching to a server with no tiles leaves none of the old server’s on screen', async ({ page }) => {
	// Fix round 2, item 1: AtlasMap used to defer removing the old tile
	// layer on *any* change, not just a fog-key-only one, so demo's terrain
	// stayed on screen under "Quiet Fjord" (which has no snapshot, so no
	// tiles) until something else happened to clear it. The switcher must
	// be used for the final switch (not a page reload, which would remount
	// AtlasMap fresh and never exercise the bug): first both servers are
	// unlocked via the share-link flow (as test 2 does), landing on quiet;
	// the switcher goes to demo (tiles appear in the one mounted AtlasMap),
	// then — the actual check — back to quiet.
	await unlock(page, 'demo');
	await page.goto('about:blank');
	await page.goto('/#s=quiet&k=quiet-pass');
	await expect(switcher(page)).toHaveAccessibleName('Switch server: Quiet Fjord');

	await switcher(page).click();
	await page.getByRole('listbox', { name: 'Servers' }).getByRole('option', { name: /^Demo/ }).click();
	await expect(switcher(page)).toHaveAccessibleName('Switch server: Demo');
	await tilesLoaded(page);
	await expect(page.locator('img.leaflet-tile')).not.toHaveCount(0);

	await switcher(page).click();
	await page.getByRole('listbox', { name: 'Servers' }).getByRole('option', { name: /Quiet Fjord/ }).click();
	await expect(switcher(page)).toHaveAccessibleName('Switch server: Quiet Fjord');

	await expect(page.locator('img.leaflet-tile')).toHaveCount(0);
});

test('3 · fog tiles load with the snapshot’s fog key; no fog canvas; zoom animation on', async ({ page }) => {
	// .png only: the biome grid (Task 4) also lives under /tiles/{id}/{key}/
	// but is a single /biomes fetch, keyed by the card's tile key rather than
	// the snapshot's fog key, so it doesn't belong in this fog-key check.
	const tileUrls: string[] = [];
	page.on('request', (r) => {
		if (r.url().includes('/tiles/') && r.url().endsWith('.png')) tileUrls.push(new URL(r.url()).pathname);
	});
	const snapshot = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/servers/demo/snapshot');
	await unlock(page);
	const { fogKey } = (await (await snapshot).json()) as { fogKey: string };
	expect(fogKey).toMatch(/^[0-9a-f]{16}$/);
	await tilesLoaded(page);
	expect(tileUrls.length).toBeGreaterThan(0);
	for (const u of tileUrls) expect(u).toMatch(new RegExp(`^/tiles/demo/12345-2-r\\d+/${fogKey}/\\d+/\\d+/\\d+\\.png$`));
	await expect(page.locator('canvas.fs-fog')).toHaveCount(0);
	await expect(page.locator('.fs-fog-skirt')).toHaveCount(0);
	// Leaflet adds its zoom-animation proxy only when zoomAnimation is on;
	// a zoom then animates the map pane.
	await expect(page.locator('.leaflet-proxy')).toHaveCount(1);
	const animating = page.waitForSelector('.leaflet-map-pane.leaflet-zoom-anim', { state: 'attached' });
	await page.getByRole('button', { name: 'Zoom in' }).click();
	await animating;
});

test('fog · terrain deep in unexplored land is fog-coloured on screen, explored land is not', async ({ page }) => {
	await unlock(page);
	await tilesLoaded(page);
	// worldToScreen reads the world disc's own on-screen box, so compute
	// both screen points before hiding it below.
	// x = −8 000 m is 6 km west of every explored zone (they span −1 920…4 600 m).
	const fogged = await worldToScreen(page, -8000, 0);
	// (−500, 300) is three zones inside the explored area, away from pins:
	// the flat fake terrain shows there (meadow green, 163,178,92).
	const clear = await worldToScreen(page, -500, 300);
	// Hide the disc pane (z-index 150, below the tiles at 200): without
	// this, a fogged sample would also pass if the tile were transparent
	// over the parchment disc fill, which is the same colour (fix round 1,
	// item 5). Hiding it proves the tile pixel itself carries the fog.
	await page.addStyleTag({ content: '.leaflet-disc-pane { display: none !important; }' });
	const [r, g, b] = await screenPixel(page, fogged.x, fogged.y);
	// #cfbe9c ± the grain (±9) and hatching; the fake terrain there is meadow or forest green.
	expect(Math.abs(r - 0xcf), `r ${r}`).toBeLessThanOrEqual(20);
	expect(Math.abs(g - 0xbe), `g ${g}`).toBeLessThanOrEqual(20);
	expect(Math.abs(b - 0x9c), `b ${b}`).toBeLessThanOrEqual(20);
	const [cr, cg, cb] = await screenPixel(page, clear.x, clear.y);
	expect(Math.abs(cr - 0xcf) + Math.abs(cg - 0xbe) + Math.abs(cb - 0x9c), `rgb ${cr},${cg},${cb}`).toBeGreaterThan(60);
});

test('fix · a fog-key change never bares the map (the old tiles stay until the new ones are in)', async ({ page }, testInfo) => {
	// `swap` is a dedicated server (global-setup.ts), seeded once and never
	// touched by anything else, so reposting to it below can't race a
	// parallel test's view of demo's state (fix round 2, item 2).
	await unlock(page, 'swap');
	await tilesLoaded(page);

	// Track, every animation frame, whether at least one rendered terrain
	// tile is on screen. A frame with none between the new fog key landing
	// and its tiles finishing would be the old remove-then-add regression
	// (fix round 1, item 1): the whole map briefly bares to the parchment
	// disc.
	await page.evaluate(() => {
		const w = window as unknown as { __zero: number; __frames: number; __stop: boolean };
		w.__zero = 0;
		w.__frames = 0;
		w.__stop = false;
		const tick = () => {
			if (document.querySelectorAll('img.leaflet-tile-loaded').length === 0) w.__zero++;
			w.__frames++;
			if (!w.__stop) requestAnimationFrame(tick);
		};
		requestAnimationFrame(tick);
	});

	// Re-post swap's seeded snapshot with one more explored zone (a new fog
	// key, same server id and tile key) and a newer savedAt, as a real save
	// would. A fixed saveId would hang a CI retry: on retry 1 `swap`
	// already holds it (INSERT OR IGNORE drops the repost), so savedAt
	// never changes and nextSnapshot below waits forever. The zone must
	// differ per attempt too — otherwise a retry re-adds a zone the mask
	// already has, the fog key doesn't actually change, and the swap this
	// test is about never happens (final review M4).
	const snapshot = JSON.parse(readFileSync(FIXTURE_SNAPSHOT, 'utf8'));
	snapshot.serverId = 'swap';
	snapshot.saveId = `fog-swap-${Date.now()}`;
	snapshot.savedAt = new Date().toISOString();
	snapshot.readAt = snapshot.savedAt;
	snapshot.exploredZones.push([100 + testInfo.retry, 100]);
	const nextSnapshot = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/servers/swap/snapshot');
	await postSnapshot('swap', snapshot);
	// Forces state.svelte.ts's immediate refresh-on-visible (around lines
	// 146–149) instead of waiting out its 15 s poll (fix round 2, item 2).
	await refreshNow(page);
	await nextSnapshot;
	await tilesLoaded(page);
	// The whole round trip is fast now (the visibilitychange fix above),
	// so wait for a few more rAF ticks to accumulate rather than asserting
	// on a frame count sampled at one arbitrary instant.
	await expect
		.poll(() => page.evaluate(() => (window as unknown as { __frames: number }).__frames))
		.toBeGreaterThan(5);

	const r = await page.evaluate(() => {
		const w = window as unknown as { __zero: number; __frames: number; __stop: boolean };
		w.__stop = true;
		return { zero: w.__zero, frames: w.__frames };
	});
	expect(r.zero, 'frames with no loaded terrain tile on screen').toBe(0);
});

test('fix · an unchanged card poll keeps the same tile layer (final review I1)', async ({ page }) => {
	// AtlasMap's tile effect used to read card.id/card.tiles.key straight
	// off the card prop, which is a new object every 15 s poll, so the
	// effect (and so the whole tile layer) rebuilt on every poll even when
	// neither the server nor the tile key actually changed.
	const tileUrls: string[] = [];
	page.on('request', (r) => {
		if (r.url().includes('/tiles/')) tileUrls.push(r.url());
	});
	await unlock(page, 'demo');
	await tilesLoaded(page);

	// Tag every current tile <img>, so "the same tile layer" can be checked
	// as "the same DOM nodes", not just "the same count".
	await page.evaluate(() => {
		document.querySelectorAll('img.leaflet-tile').forEach((img, i) => img.setAttribute('data-fs-test-tag', String(i)));
	});
	const before = await page
		.locator('img.leaflet-tile')
		.evaluateAll((imgs) => imgs.map((i) => i.getAttribute('data-fs-test-tag')));
	expect(before.length).toBeGreaterThan(0);

	tileUrls.length = 0; // only requests made after this point count
	const card = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/servers/demo');
	await refreshNow(page); // one card poll, same key and fog key
	await card;
	// No network round trip to wait on for a negative assertion: give a
	// generous moment for an (incorrect) rebuild to happen.
	await page.waitForTimeout(500);

	const after = await page
		.locator('img.leaflet-tile')
		.evaluateAll((imgs) => imgs.map((i) => i.getAttribute('data-fs-test-tag')));
	expect(after, 'the same tile <img> elements, not a rebuilt layer').toEqual(before);
	expect(tileUrls, 're-requests of the same tile URLs').toEqual([]);
});

test('4 · online tab: players, recently online and activity', async ({ page }) => {
	await unlock(page);
	const online = page.getByRole('list', { name: 'Online now' });
	for (const name of ['Astrid', 'Bjorn', 'Sigrun']) await expect(online).toContainText(name);
	await expect(online.getByRole('listitem')).toHaveCount(3);
	await expect(page.getByRole('list', { name: 'Recently online' })).toContainText('Ulf');
	const activity = page.getByRole('region', { name: 'Recent activity' });
	// Plan 7: the newest save's world events lead the list now.
	await expect(activity).toContainText('Sigrun joined');
	await expect(activity).toContainText('Moder defeated');
	await expect(activity.getByRole('listitem').filter({ hasText: 'Autosave finished' })).toHaveCount(1);
});

test('fix · desktop profile: focus moves to Back on open, and back to the opening row on close (button, Escape, browser back)', async ({
	page
}) => {
	await unlock(page);
	const online = page.getByRole('list', { name: 'Online now' });
	const row = online.getByRole('button', { name: 'Profile of Astrid' });
	const back = page.getByRole('button', { name: 'Back' });

	await row.click();
	await expect(back).toBeFocused();
	await expect(page).toHaveURL(/#s=demo&p=76561190000000001$/);
	await back.click();
	await expect(page.getByTestId('profile-panel')).toHaveCount(0);
	await expect(row).toBeFocused();

	await row.click();
	await expect(back).toBeFocused();
	await page.keyboard.press('Escape');
	await expect(page.getByTestId('profile-panel')).toHaveCount(0);
	await expect(row).toBeFocused();

	await row.click();
	await page.goBack();
	await expect(page.getByTestId('profile-panel')).toHaveCount(0);
	await expect(row).toBeFocused();
});

test('fix · desktop profile: closing falls back to the Online tab when the opening row is gone', async ({ page }) => {
	await unlock(page);
	const online = page.getByRole('list', { name: 'Online now' });
	await online.getByRole('button', { name: 'Profile of Astrid' }).click();
	const back = page.getByRole('button', { name: 'Back' });
	await expect(back).toBeFocused();

	// Astrid drops off the online (and recent) lists before the next poll;
	// the open profile itself is unaffected (it's keyed on the player, not
	// the card). Adaptation (Plan 7): Astrid's seed now also has closed
	// sessions on earlier days (Task 9's seeded history), so the real
	// card's own `recent` list carries her too — strip both, or her row
	// would still exist there and the fallback would (correctly) refocus
	// it instead of falling back to the Online tab.
	await overrideCard(page, (c) => ({
		...c,
		online: c.online.filter((p) => p.name !== 'Astrid'),
		recent: c.recent.filter((r) => r.name !== 'Astrid')
	}));
	await refreshNow(page);
	await expect(page.getByTestId('profile-panel')).toContainText('Astrid');

	await back.click();
	await expect(page.getByTestId('profile-panel')).toHaveCount(0);
	await expect(page.getByRole('tab', { name: /^Online/ })).toBeFocused();
});

// Review fix: the map came first in the DOM and Leaflet makes every pin a
// tab stop, so a keyboard user went through every pin before reaching the
// panel, search, layers or zoom.
test('fix · keyboard: Tab reaches the panel, search, layers and zoom before the first map pin', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	const seen = await tabToFirstPin(page);
	expect(seen.at(-1), 'the pins are still reachable').toBe('pin');
	for (const label of ['Collapse panel', 'Search the map', 'Layers · 6 on', 'Zoom in', 'Reset view']) {
		expect(seen, `${label} before the first pin`).toContain(label);
	}
});

test('5 · world tab: day, bosses, next up and world rules', async ({ page }) => {
	await unlock(page);
	await page.getByRole('tab', { name: 'World' }).click();
	const panel = page.getByRole('tabpanel');
	await expect(panel.locator('.tile', { hasText: 'In-game day' })).toContainText('214');
	await expect(panel.locator('.tile', { hasText: 'Bosses' }).locator('.big')).toHaveText('4 / 8');
	await expect(panel.locator('li.boss')).toHaveCount(8);
	await expect(panel.locator('li.boss.defeated')).toHaveCount(4);
	await expect(panel).toContainText('Next up: Yagluth');
	await expect(panel.getByRole('button', { name: 'Show altar' })).toBeVisible();
	const combat = panel.getByRole('region', { name: 'World rules' }).locator('.tile', { has: page.locator('dt', { hasText: /^Combat$/ }) });
	await expect(combat.locator('dd')).toHaveText('Hard');
	await expect(combat.locator('dd')).toHaveClass(/changed/);

	await markersReady(page);
	await panel.getByRole('button', { name: 'Show altar' }).click();
	await expect(markerCard(page)).toContainText('Yagluth');
});

test('6 · search: copper finds the unpaired portal, Big finds the tame', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	await search(page).fill('copper');
	const options = page.getByRole('listbox', { name: 'Search results' }).getByRole('option');
	// The sign "To the copper mine" matches too; exactly one portal does.
	await expect(options.filter({ hasText: 'Portal ·' })).toHaveCount(1);
	await expect(options.first()).toContainText('copper');
	await expect(options.first()).toContainText('Portal · unpaired');
	await options.first().click();
	await expect(markerCard(page)).toContainText('copper');
	await expect(markerCard(page).locator('.badge')).toHaveText(['Unpaired']);

	await search(page).fill('Big');
	await expect(options).toHaveCount(1);
	await expect(options.first()).toContainText('Big Mama');
	await expect(options.first()).toContainText('Lox');
});

test('7 · layers: beds on adds beds, portals off removes portal pins and lines', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	const lines = page.locator('.leaflet-portalLines-pane path');
	// Anything on the map (a pin or a cluster's tooltip) that mentions a kind.
	const mentions = (kind: string) => page.locator(`.leaflet-marker-icon[title*="${kind}"]`);
	// Only one line: portal-3's partner (portal-4, the "mountain" tag) is
	// unexplored and filtered out (fix round 1, item 6), so only the "home"
	// pair (portal-1/portal-2) still draws one. Lines are one path per
	// style class, one subpath (an M command) per pair.
	await expect(lines).toHaveCount(1);
	await expect(lines).toHaveClass('pl');
	expect((await lines.getAttribute('d'))!.match(/M/g)).toHaveLength(1);
	await expect(mentions('Bed')).toHaveCount(0);

	await page.getByRole('button', { name: /^Layers ·/ }).click();
	const layers = page.getByRole('group', { name: 'Map layers' });
	await layers.getByRole('switch', { name: /^Beds/ }).click();
	await expect(layers.getByRole('switch', { name: /^Beds/ })).toHaveAttribute('aria-checked', 'true');
	// Bed pins sit next to the bases and cluster with them at this zoom.
	await expect(mentions('Bed').first()).toBeVisible();

	await layers.getByRole('switch', { name: /^Portals/ }).click();
	await expect(layers.getByRole('switch', { name: /^Portals/ })).toHaveAttribute('aria-checked', 'false');
	await expect(mentions('Portal')).toHaveCount(0);
	await expect(pinsOfKind(page, 'Portal')).toHaveCount(0);
	await expect(lines).toHaveCount(0);
});

test('8 · jump to partner selects the other portal', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	await search(page).fill('home');
	const options = page.getByRole('listbox', { name: 'Search results' }).getByRole('option');
	await expect(options).toHaveCount(2);
	await options.first().click();
	const card = markerCard(page);
	await expect(card).toContainText('Paired');
	// The selected pair's line takes the selected style (its own path).
	const lines = page.locator('.leaflet-portalLines-pane path');
	await expect(lines).toHaveCount(1);
	await expect(lines).toHaveClass('pl pl-sel');
	const ends = ['X 131 · Z −70', 'X 3,210 · Z 71'];
	const where = card.locator('.where');
	await expect(where).toHaveText(new RegExp(`^(${ends.join('|')})$`));
	const first = (await where.textContent())!.trim();
	await card.getByRole('button', { name: 'Jump to partner →' }).click();
	await expect(where).toHaveText(ends.find((e) => e !== first)!);
	await expect(card.locator('.title')).toHaveText('home');
});

test('9 · join dialog: code, copy toast, address; quiet has no code box', async ({ page }) => {
	await page.addInitScript(() => {
		Object.defineProperty(Navigator.prototype, 'clipboard', {
			configurable: true,
			get: () => ({
				writeText: async (t: string) => {
					(window as unknown as { __copied: string }).__copied = t;
				}
			})
		});
	});
	await unlock(page);
	await page.getByRole('button', { name: 'How to join' }).click();
	const dialog = page.getByRole('dialog', { name: 'Join Demo' });
	await expect(dialog.locator('.code')).toHaveText('318 742');
	await dialog.getByRole('button', { name: 'Copy code' }).click();
	await expect(page.locator('.toast')).toContainText('Join code copied');
	expect(await page.evaluate(() => (window as unknown as { __copied: string }).__copied)).toBe('318742');
	await expect(dialog.locator('.addr')).toHaveText('play.example.net:2456');
	await dialog.getByRole('button', { name: 'Close' }).click();
	await expect(dialog).toBeHidden();

	await unlock(page, 'quiet');
	await page.getByRole('button', { name: 'How to join' }).click();
	const quiet = page.getByRole('dialog', { name: 'Join Quiet Fjord' });
	await expect(quiet).toBeVisible();
	await expect(quiet).toContainText('crossplay off');
	await expect(quiet.getByText('Join code')).toHaveCount(0);
	await expect(quiet.getByRole('button', { name: 'Copy code' })).toHaveCount(0);
});

test('10 · theme toggle flips data-theme and persists across a reload', async ({ page }) => {
	await unlock(page);
	const html = page.locator('html');
	await expect(html).toHaveAttribute('data-theme', 'dark');
	await page.getByRole('button', { name: 'Toggle theme' }).click();
	await expect(html).toHaveAttribute('data-theme', 'light');
	await page.reload();
	await expect(page.locator('main.shell')).toBeVisible();
	await expect(html).toHaveAttribute('data-theme', 'light');
	await page.getByRole('button', { name: 'Toggle theme' }).click();
	await expect(html).toHaveAttribute('data-theme', 'dark');
});

test('11 · the crypt outside the explored cells never appears', async ({ page }) => {
	const snapshot = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/servers/demo/snapshot');
	await unlock(page);
	// The server filters markers, locations and bases by the 12 m explored
	// mask: only the inside crypt ships, and both markers deliberately
	// placed outside every explored cell (fix round 1, item 6) are gone too.
	const body = await (await snapshot).json();
	expect(body.explored).toMatchObject({ source: 'zones', cell: 12, size: 2048 });
	const crypts = (body.locations as { id: string; type: string }[]).filter((l) => l.type === 'SunkenCrypt4');
	expect(crypts.map((l) => l.id)).toEqual(['loc-140']);
	expect((body.locations as { id: string }[]).map((l) => l.id)).not.toContain('loc-311');
	const markerIds = (body.markers as { id: string }[]).map((m) => m.id);
	expect(markerIds).not.toContain('portal-4');
	expect(markerIds).not.toContain('tame-4');
	expect(body.markers).toHaveLength(12);
	expect(body.bases).toHaveLength(2);
	await markersReady(page);

	// portal-3's partner (portal-4, "mountain") was filtered out: it shows
	// as unpaired, with an honest note that neither claims no partner
	// exists nor tells the player to build or retag one (fix round 1,
	// item 4).
	await search(page).fill('mountain');
	const mountainOptions = page.getByRole('listbox', { name: 'Search results' }).getByRole('option');
	await expect(mountainOptions).toHaveCount(1);
	await mountainOptions.first().click();
	await expect(markerCard(page).locator('.badge')).toHaveText(['Unpaired']);
	await expect(markerCard(page)).toContainText('No explored portal carries the tag “mountain”, so it leads nowhere yet.');

	// Dungeons aren't searchable (plan ruling "Search": portal, base, tame,
	// sign, altar, trader), so neither crypt comes up.
	await search(page).fill('Sunken');
	await expect(page.getByRole('status').filter({ hasText: 'Nothing matches “Sunken”' })).toBeVisible();
	// Layer counts: Landmarks (Eikthyr, Yagluth, Haldor, the Forge of
	// Potential), Dungeons (the inside crypt, the troll cave), Minor places
	// (the bear cave) — the single "Dungeons & locations" layer is gone
	// (spec 2026-10-05, cursor biome and locations).
	await page.getByRole('button', { name: /^Layers ·/ }).click();
	const layers = page.getByRole('group', { name: 'Map layers' });
	await expect(layers.getByRole('switch', { name: /^Landmarks/ }).locator('.count')).toHaveText('4');
	await expect(layers.getByRole('switch', { name: /^Dungeons/ }).locator('.count')).toHaveText('2');
	await expect(layers.getByRole('switch', { name: /^Minor places/ }).locator('.count')).toHaveText('1');
});

test('12 · biome under the cursor, and the three location layers', async ({ page }) => {
	await unlock(page);
	await markersReady(page);

	// Layers panel default state: Landmarks on, Dungeons and Minor places
	// off (spec 2026-10-05).
	await page.getByRole('button', { name: /^Layers ·/ }).click();
	const layers = page.getByRole('group', { name: 'Map layers' });
	await expect(layers.getByRole('switch', { name: /^Landmarks/ })).toHaveAttribute('aria-checked', 'true');
	await expect(layers.getByRole('switch', { name: /^Dungeons/ })).toHaveAttribute('aria-checked', 'false');
	await expect(layers.getByRole('switch', { name: /^Minor places/ })).toHaveAttribute('aria-checked', 'false');
	await page.keyboard.press('Escape');

	// The Forge of Potential (AncientUpgradeStation, loc-420, explored):
	// Landmarks has no zoom gating, so it shows at the default zoom — here
	// inside the spawn cluster alongside the base and the home portal, so
	// this reads its kicker off the cluster's own tooltip, same as test 7's
	// bed-in-a-cluster check.
	await expect(page.locator('.leaflet-marker-icon[title*="Forge of Potential"]')).toBeVisible();

	// Hovering explored ground (world origin, Meadows for the seeded demo
	// world — checked with a throwaway worldgen.NewBase(12345, 2).Biome
	// call) shows the biome name in the readout's place slot.
	const origin = await worldToScreen(page, 0, 0);
	await page.mouse.move(origin.x, origin.y);
	await expect(page.locator('.readout .place')).toHaveText('Meadows');
});

// --- Carried from reviews ----------------------------------------------------

test('carried · the search highlight survives a card poll; Enter opens it', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	const input = search(page);
	await input.fill('s');
	const options = page.getByRole('listbox', { name: 'Search results' }).getByRole('option');
	await expect(options).toHaveCount(3);
	await input.press('ArrowDown');
	await input.press('ArrowDown');
	const second = options.nth(1);
	await expect(second).toHaveAttribute('aria-selected', 'true');
	await expect(second).toContainText('Longhouse');
	const id = await second.getAttribute('id');
	await expect(input).toHaveAttribute('aria-activedescendant', id!);

	await page.waitForResponse((r) => new URL(r.url()).pathname === '/api/servers/demo', { timeout: 20_000 });
	await expect(options).toHaveCount(3);
	await expect(options.nth(1)).toHaveAttribute('aria-selected', 'true');
	await expect(options.nth(1)).toContainText('Longhouse');
	await expect(input).toHaveAttribute('aria-activedescendant', id!);

	await input.press('Enter');
	await expect(markerCard(page).locator('.title')).toHaveText('Longhouse');
});

test('carried · closing the join dialog with Esc returns focus to "How to join"', async ({ page }) => {
	await unlock(page);
	const join = page.getByRole('button', { name: 'How to join' });
	await join.click();
	const dialog = page.getByRole('dialog', { name: 'Join Demo' });
	await expect(dialog).toBeVisible();
	await expect(dialog.getByRole('button', { name: 'Close' })).toBeFocused();
	await page.keyboard.press('Escape');
	await expect(dialog).toBeHidden();
	await expect(join).toBeFocused();
});

test('carried · cancelling "Add a server…" returns focus to the switcher', async ({ page }) => {
	await unlock(page);
	await switcher(page).click();
	await page.getByRole('button', { name: 'Add a server…' }).click();
	const dialog = page.getByRole('dialog', { name: 'Unlock a server' });
	await expect(dialog).toBeVisible();
	await page.keyboard.press('Escape');
	await expect(dialog).toBeHidden();
	await expect(switcher(page)).toBeFocused();
});

test('carried · without navigator.clipboard, Copy copies the raw digits via execCommand', async ({ page }) => {
	await page.addInitScript(() => {
		Object.defineProperty(Navigator.prototype, 'clipboard', { configurable: true, get: () => undefined });
		const w = window as unknown as { __exec: { cmd: string; value: string | null }[] };
		w.__exec = [];
		Document.prototype.execCommand = function (cmd: string): boolean {
			const a = document.activeElement;
			w.__exec.push({ cmd, value: a instanceof HTMLTextAreaElement ? a.value : null });
			return cmd === 'copy';
		};
	});
	await unlock(page);
	await page.getByRole('button', { name: 'How to join' }).click();
	const dialog = page.getByRole('dialog', { name: 'Join Demo' });
	await dialog.getByRole('button', { name: 'Copy code' }).click();
	await expect(page.locator('.toast')).toContainText('Join code copied');
	const calls = await page.evaluate(() => (window as unknown as { __exec: unknown[] }).__exec);
	expect(calls).toEqual([{ cmd: 'copy', value: '318742' }]);
	// The temporary textarea is gone.
	await expect(page.locator('textarea')).toHaveCount(0);
});

test('carried · at 1280×800 with the panel open the map-updated pill clears the search box', async ({ page }) => {
	await page.setViewportSize({ width: 1280, height: 800 });
	await unlock(page);
	await expect(page.getByRole('region', { name: 'Server' })).toBeVisible();
	const pill = page.locator('.pill', { hasText: 'Map updated' });
	const box = page.locator('.search');
	await expect(pill).toBeVisible();
	await expect(box).toBeVisible();
	// Polled: the pill's slot is placed from its measured width after the first frame.
	await expect
		.poll(async () => {
			const a = (await pill.boundingBox())!;
			const b = (await box.boundingBox())!;
			return a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;
		}, { message: 'the map-updated pill overlaps the search box' })
		.toBe(false);
});

test('favicon · /favicon.svg and /favicon.ico are served, and the page links the SVG', async ({ page, request }) => {
	// Browsers fetch /favicon.ico on their own when a page has no icon
	// link, so both must be 200 from the Go server, not its 404.
	for (const [path, type] of [
		['/favicon.svg', 'image/svg+xml'],
		['/favicon.ico', 'image/'] // x-icon or vnd.microsoft.icon, per the host's MIME table
	]) {
		const res = await request.get(path);
		expect(res.status(), path).toBe(200);
		expect(res.headers()['content-type'], path).toContain(type);
	}
	await page.goto('/');
	await expect(page.locator('link[rel="icon"]')).toHaveAttribute('href', /\/favicon\.svg$/);
	await expect(page.locator('link[rel="icon"]')).toHaveAttribute('type', 'image/svg+xml');
});

// --- The shared guard fixture (helpers.ts) ------------------------------------
// These call watchGuard/checkGuard directly and assert on the specific
// message, instead of a bare `test.fail()`. A bare test.fail() only asserts
// that the test ends up failing, not why: it would still pass (reported as
// "expected to fail") if the guard fired for the wrong reason, or didn't
// fire at all but something unrelated in the test happened to throw. They
// use the base Playwright `test` (not the one exported by ./helpers), so the
// shared auto `guard` fixture doesn't also watch the same page and fail the
// test a second, uninspected way.

// The base Playwright `test` has no baseURL fixture (only helpers.ts's
// extended `test` sets one, from BASE_URL), so these navigate with the full
// URL rather than a relative "/".
function guardBase(): string {
	return process.env.BASE_URL ?? 'http://127.0.0.1';
}
function guardOrigin(): string {
	return new URL(guardBase()).origin;
}

pwTest.describe('guard self-tests', () => {
	pwTest('an uncaught page error is recorded, and checkGuard throws mentioning it', async ({ page, context }) => {
		const problems = watchGuard(context, guardOrigin());
		await page.goto(guardBase());
		await page.evaluate(() =>
			setTimeout(() => {
				throw new Error('guard probe');
			})
		);
		await page.waitForTimeout(200);
		expect(() => checkGuard(problems)).toThrow(/page error: guard probe/);
	});

	pwTest('a CSP violation is recorded, and checkGuard throws mentioning it', async ({ page, context }) => {
		const problems = watchGuard(context, guardOrigin());
		await page.goto(guardBase());
		// An inline script is refused by the page's CSP (script-src 'self' + hashes).
		await page.addScriptTag({ content: 'window.__probe = 1;' }).catch(() => {});
		await page.waitForTimeout(200);
		expect(() => checkGuard(problems)).toThrow(/Content Security Policy|Refused to/);
	});

	pwTest.describe('with the CSP bypassed', () => {
		// The CSP alone would refuse the request; bypass it to check the request guard itself.
		pwTest.use({ bypassCSP: true });
		pwTest('a request to another origin is recorded, and checkGuard throws mentioning it', async ({ page, context }) => {
			const problems = watchGuard(context, guardOrigin());
			await page.route('http://third-party.invalid/**', (r) => r.abort());
			await page.goto(guardBase());
			await page.evaluate(() => fetch('http://third-party.invalid/pixel.gif', { mode: 'no-cors' }).catch(() => {}));
			await page.waitForTimeout(200);
			expect(() => checkGuard(problems)).toThrow(/third-party|another origin/);
		});
	});

	// A negative control: without provoking anything, checkGuard must not
	// throw — otherwise the three tests above would pass for the wrong
	// reason (checkGuard throwing unconditionally).
	pwTest('checkGuard does not throw when nothing went wrong', async ({ page, context }) => {
		const problems = watchGuard(context, guardOrigin());
		await page.goto(guardBase());
		await page.waitForTimeout(200);
		expect(() => checkGuard(problems)).not.toThrow();
	});
});

// --- Plan 7: player profiles and the activity timeline ----------------------

const profilePanel = (page: Page) => page.getByRole('complementary', { name: 'Player profile' });
const activityPanel = (page: Page) => page.getByRole('complementary', { name: 'Activity' });
const stat = (root: ReturnType<typeof profilePanel>, k: string) => root.locator('.stat', { hasText: k }).locator('dd');

test('profile · Online → Profile: Astrid’s figures from the seed, Map →, browser back', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	await page.getByRole('list', { name: 'Online now' }).getByRole('button', { name: 'Profile of Astrid' }).click();
	await expect(page).toHaveURL(/#s=demo&p=76561190000000001$/);
	const profile = profilePanel(page);
	await expect(profile.getByRole('heading', { name: 'Astrid' })).toBeVisible();
	await expect(profile.locator('.status')).toHaveText(/^Online now · 1h \d+m$/);
	// Three sessions: 2 h 30 m and 2 h on earlier days, and the open one.
	await expect(stat(profile, 'Sessions')).toHaveText('3');
	await expect(stat(profile, 'All time')).toHaveText(/^5h \d\dm$/);
	await expect(profile.getByRole('list', { name: 'Hours online per day' }).getByRole('listitem')).toHaveCount(7);
	await expect(profile.locator('.facts')).toContainText('1 · Longhouse');
	await expect(profile.getByRole('region', { name: 'Bases' })).toContainText('2,184 pieces · Meadows');
	// home and mountain are hers; mountain's partner is unexplored, so it shows unpaired.
	await expect(profile.getByRole('region', { name: 'Portals placed' })).toContainText('2 portals');
	await expect(profile.getByRole('region', { name: 'Tames they named' })).toContainText('Big Mama · Lox');
	await expect(profile.getByRole('region', { name: 'Deaths' })).toContainText('0 spotted · 0 this week');

	await profile.getByRole('region', { name: 'Bases' }).getByRole('button', { name: /Longhouse/ }).click();
	await expect(markerCard(page)).toContainText('Longhouse');

	await page.goBack();
	await expect(profile).toBeHidden();
	await expect(page).toHaveURL(/#s=demo$/);
	await expect(serverCard(page)).toBeVisible();
});

test('profile · Recently online → Ulf; Bjorn’s tombstone; a linked profile; an unknown player', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	await page.getByRole('list', { name: 'Recently online' }).getByRole('button', { name: 'Profile of Ulf' }).click();
	const profile = profilePanel(page);
	await expect(profile.locator('.status')).toHaveText(/^Last seen /);
	await expect(stat(profile, 'Sessions')).toHaveText('2');
	await expect(stat(profile, 'All time')).toHaveText('2h 10m');
	await expect(profile.getByRole('region', { name: 'Bases' })).toContainText('Eastwatch');
	await expect(profile.getByRole('region', { name: 'Portals placed' })).toContainText('None yet.');
	await profile.getByRole('button', { name: 'Back' }).click();
	await expect(profile).toBeHidden();
	await expect(page).toHaveURL(/#s=demo$/);

	await page.getByRole('list', { name: 'Online now' }).getByRole('button', { name: 'Profile of Bjorn' }).click();
	const deaths = profilePanel(page).getByRole('region', { name: 'Deaths' });
	await expect(deaths).toContainText('1 spotted · 1 this week');
	await deaths.getByRole('button', { name: /Tombstone in the Meadows · since save/ }).click();
	await expect(markerCard(page)).toContainText('Bjorn');

	// A linked profile opens on load; an unknown player says so.
	await page.goto('about:blank');
	await page.goto('/#s=demo&p=76561190000000004');
	await expect(profilePanel(page).getByRole('heading', { name: 'Ulf' })).toBeVisible();
	await page.goto('about:blank');
	await page.goto('/#s=demo&p=nobody');
	await expect(profilePanel(page)).toContainText('This player hasn’t been seen on this server.');
});

test('activity · Full timeline: world-save events, chips, people, the short list follows, back', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	await page.getByRole('region', { name: 'Recent activity' }).getByRole('button', { name: 'Full timeline →' }).click();
	await expect(page).toHaveURL(/#s=demo&activity$/);
	const tl = activityPanel(page);
	for (const text of [
		'New tombstone: Bjorn, in the Meadows',
		'New portal “copper”, not paired with anything yet',
		'New tame: Big Mama (Lox)',
		'New base: Eastwatch (Plains)',
		'Longhouse grew by 124 pieces',
		'Moder defeated',
		'Raid: The forest is moving',
		'Sigrun joined'
	]) {
		await expect(tl).toContainText(text);
	}
	await expect(tl.getByRole('listitem').filter({ hasText: 'Moder defeated' })).toContainText(/World save · \d\d:\d\d/);
	await expect(tl.getByRole('listitem').filter({ hasText: 'Autosave finished' }).first()).toContainText('Server log');
	const today = tl.locator('.today-row');
	for (const name of ['Astrid', 'Bjorn', 'Sigrun']) await expect(today.filter({ hasText: name })).toHaveCount(1);

	const deaths = tl.getByRole('button', { name: /^Deaths/ });
	await expect(deaths).toHaveAttribute('aria-pressed', 'true');
	await expect(deaths.locator('.count')).toHaveText('1');
	await deaths.click();
	await expect(deaths).toHaveAttribute('aria-pressed', 'false');
	await expect(tl).not.toContainText('New tombstone');

	await tl.getByRole('group', { name: 'People' }).getByRole('button', { name: /Ulf/ }).click();
	await expect(tl).toContainText('Ulf left after 40m');
	await expect(tl).not.toContainText('Sigrun joined');
	await expect(tl).toContainText('Autosave finished'); // the server's own events stay
	await tl.getByRole('button', { name: 'Reset filters' }).click();
	await expect(tl).toContainText('New tombstone');
	await expect(tl).toContainText('Sigrun joined');

	// The side panel's short list shares the filters.
	await deaths.click();
	await tl.getByRole('button', { name: 'Back' }).click();
	await expect(tl).toBeHidden();
	await expect(page).toHaveURL(/#s=demo$/);
	const short = page.getByRole('region', { name: 'Recent activity' });
	await expect(short).toContainText('Moder defeated');
	await expect(short).not.toContainText('New tombstone');
});

test('activity · Show earlier reaches the start of tracking; Show on map →', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	await page.getByRole('region', { name: 'Recent activity' }).getByRole('button', { name: 'Full timeline →' }).click();
	const tl = activityPanel(page);
	await expect(tl).toContainText('Moder defeated');
	await expect(tl).not.toContainText('Sigrun left after 1h 00m');
	await tl.getByRole('button', { name: 'Show earlier' }).click();
	await expect(tl).toContainText('Sigrun left after 1h 00m');
	await expect(tl).toContainText(/Tracking began \d+ [A-Z][a-z]{2}/);
	await expect(tl.getByRole('button', { name: 'Show earlier' })).toHaveCount(0);
	await expect(tl.getByText(/^Last 6 days/)).toBeVisible();

	await tl.getByRole('listitem').filter({ hasText: 'New tombstone: Bjorn' }).getByRole('button', { name: 'Show on map →' }).click();
	await expect(markerCard(page)).toContainText('Bjorn');
	await expect(tl).toBeVisible();
});

test('activity · a world event in unexplored ground never reaches the timeline', async ({ page }) => {
	// Adaptation (beyond the brief): global-setup.ts's two-save pair also
	// drops portal-4 ("mountain"'s partner, already outside every explored
	// zone in the fixture) from the earlier save alone, so it reads as a
	// brand-new portal with a Pos in unexplored ground — exercising the
	// server's drop-in-unexplored-ground rule end to end.
	await unlock(page);
	await markersReady(page);
	await page.getByRole('region', { name: 'Recent activity' }).getByRole('button', { name: 'Full timeline →' }).click();
	const tl = activityPanel(page);
	await expect(tl).toContainText('New portal “copper”, not paired with anything yet');
	await expect(tl).not.toContainText('New portal “mountain”');
	await expect(tl).not.toContainText('mountain” now paired');
	await expect(tl.getByRole('button', { name: /^Portals/ }).locator('.count')).toHaveText('1');
});

test('fix · desktop activity: focus moves to Back on open, and back to "Full timeline →" on close (button, Escape, browser back)', async ({
	page
}) => {
	await unlock(page);
	const row = page.getByRole('region', { name: 'Recent activity' }).getByRole('button', { name: 'Full timeline →' });
	const back = page.getByRole('button', { name: 'Back' });

	await row.click();
	await expect(back).toBeFocused();
	await expect(page).toHaveURL(/#s=demo&activity$/);
	await back.click();
	await expect(page.getByTestId('activity-panel')).toHaveCount(0);
	await expect(row).toBeFocused();

	await row.click();
	await expect(back).toBeFocused();
	await page.keyboard.press('Escape');
	await expect(page.getByTestId('activity-panel')).toHaveCount(0);
	await expect(row).toBeFocused();

	await row.click();
	await page.goBack();
	await expect(page.getByTestId('activity-panel')).toHaveCount(0);
	await expect(row).toBeFocused();
});

// Review fix: with the panel collapsed, closing a view re-renders the
// collapsed pill, not the panel, so neither the opening row nor the Online
// tab exists and focus used to drop to <body>.
test('fix · desktop: closing a profile or the timeline over the collapsed panel focuses the collapsed pill', async ({ page }) => {
	await unlock(page);
	const back = page.getByRole('button', { name: 'Back' });
	const pill = page.getByRole('button', { name: /^Expand panel/ });
	const openers = [
		{ open: page.getByRole('list', { name: 'Online now' }).getByRole('button', { name: 'Profile of Astrid' }), panel: 'profile-panel' },
		{ open: page.getByRole('region', { name: 'Recent activity' }).getByRole('button', { name: 'Full timeline →' }), panel: 'activity-panel' }
	];
	for (const { open, panel } of openers) {
		// Open the view, close it, collapse the panel, then reopen the view
		// with browser forward: it now sits over the collapsed panel.
		if (await pill.count()) await pill.click();
		await open.click();
		await expect(back).toBeFocused();
		await page.goBack();
		await expect(page.getByTestId(panel)).toHaveCount(0);
		await page.getByRole('button', { name: 'Collapse panel' }).click();
		await expect(pill).toBeVisible();
		await page.goForward();
		await expect(page.getByTestId(panel)).toBeVisible();
		await back.click();
		await expect(page.getByTestId(panel)).toHaveCount(0);
		await expect(pill).toBeFocused();
	}
});

// Review fix: the popover's left edge was clamped clear of the panel only
// when the popover was placed (on a selection or a map move), so widening
// the panel left it underneath and narrowing it left it stranded.
test('fix · desktop: the marker popover follows the panel’s width', async ({ page }) => {
	await unlock(page);
	await markersReady(page);
	await search(page).fill('copper');
	await page.getByRole('listbox', { name: 'Search results' }).getByRole('option').filter({ hasText: 'Portal · unpaired' }).click();
	const left = async () => Math.round((await markerCard(page).boundingBox())!.x);
	// The pin settles at the visible centre (x = 908), the popover 26 px right
	// of it; only then does a drag pan the map rather than stop the animation.
	await expect.poll(left).toBe(934);
	await expect(page.locator('.leaflet-map-pane.leaflet-zoom-anim')).toHaveCount(0);
	await page.waitForTimeout(500);
	// Drag the pin to x ≈ 500: the popover follows, still clear of the panel.
	await page.mouse.move(850, 820);
	await page.mouse.down();
	for (let i = 1; i <= 20; i++) await page.mouse.move(850 - i * 20, 820);
	await page.mouse.up();
	// Leaflet's inertia carries the pan on a little: wait until it stops.
	let last = NaN;
	await expect
		.poll(async () => {
			const prev = last;
			last = await left();
			return last === prev;
		})
		.toBe(true);
	const beside = last;
	expect(beside).toBeGreaterThanOrEqual(376 + 16);
	expect(beside).toBeLessThan(552);

	// The Activity view widens the panel to 552 px: the popover moves clear of it.
	await page.getByRole('region', { name: 'Recent activity' }).getByRole('button', { name: 'Full timeline →' }).click();
	await expect(page.getByTestId('activity-panel')).toBeVisible();
	await expect.poll(left).toBeGreaterThanOrEqual(552 + 16);
	// Back to the 376 px panel: the popover returns beside its pin.
	await page.getByRole('button', { name: 'Back' }).click();
	await expect(page.getByTestId('activity-panel')).toHaveCount(0);
	await expect.poll(left).toBe(beside);
});

// --- Plan 9: time and weather -------------------------------------------------

const weatherPill = (page: Page) => page.getByTestId('weather-pill');

test('weather · the pill shows a phase and clock; the dropdown lists the seeded explored biomes', async ({ page }) => {
	await unlock(page);
	const pill = weatherPill(page);
	await expect(pill).toBeVisible();
	const button = pill.locator('button.pill');
	// The weather word itself (Clear, Rain, …) depends on wall-clock time
	// (the period draw), so only the phase/clock structure is asserted here;
	// "Meadows" is the biggest base's biome in the seed (Longhouse) and
	// doesn't change with time.
	await expect(button.locator('.title')).toHaveText(/^(Morning|Day|Evening|Night) · \d{2}:\d{2}$/);
	await expect(button.locator('.line')).toHaveText(/^Meadows: .+$/);

	await expect(button).toHaveAttribute('aria-expanded', 'false');
	await button.click();
	await expect(button).toHaveAttribute('aria-expanded', 'true');
	const panel = page.getByRole('region', { name: 'Time and weather' });
	await expect(panel).toBeVisible();
	await expect(panel).toContainText(/^Day \d+ ·/);

	// global-setup.ts posts snapshot.json with -explored, which rasterises
	// exploredZones into the 12 m mask the server samples against the biome
	// grid; these six biomes are exactly what that fixture covers, and (unlike
	// the weather itself) don't depend on wall-clock time.
	const table = panel.getByRole('table', { name: 'Weather by biome' });
	for (const biome of ['Ocean', 'Meadows', 'Black Forest', 'Swamp', 'Mountains', 'Plains']) {
		await expect(table.getByText(biome, { exact: true })).toHaveCount(1);
	}

	await page.keyboard.press('Escape');
	await expect(button).toHaveAttribute('aria-expanded', 'false');
	await expect(panel).toHaveCount(0);
});
