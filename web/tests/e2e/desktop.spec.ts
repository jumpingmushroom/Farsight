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
	const tileUrls: string[] = [];
	page.on('request', (r) => {
		if (r.url().includes('/tiles/')) tileUrls.push(new URL(r.url()).pathname);
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
	await expect(activity).toContainText('Astrid joined');
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
	// the card).
	await overrideCard(page, (c) => ({ ...c, online: c.online.filter((p) => p.name !== 'Astrid') }));
	await refreshNow(page);
	await expect(page.getByTestId('profile-panel')).toContainText('Astrid');

	await back.click();
	await expect(page.getByTestId('profile-panel')).toHaveCount(0);
	await expect(page.getByRole('tab', { name: /^Online/ })).toBeFocused();
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
	// Layer counts: Eikthyr, Yagluth, Haldor, the inside crypt, the troll cave.
	await page.getByRole('button', { name: /^Layers ·/ }).click();
	await expect(
		page.getByRole('group', { name: 'Map layers' }).getByRole('switch', { name: /^Dungeons & locations/ }).locator('.count')
	).toHaveText('5');
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
