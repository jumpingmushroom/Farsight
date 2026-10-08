// Mobile (390×844, touch) e2e against the seeded `demo` server
// (global-setup.ts). Numbered tests follow the Task 9 brief; the "carried"
// ones are the regressions carried over from reviews.
import type { Page } from '@playwright/test';
import { expect, pin, tabToFirstPin, test, unlock } from './helpers';

const topBar = (page: Page) => page.getByTestId('mobile-top-bar');
const peek = (page: Page) => page.getByTestId('sheet-peek');
const pulled = (page: Page) => page.getByTestId('sheet-pulled');
const handle = (page: Page) => page.getByRole('button', { name: /^(Expand|Collapse) sheet$/ });

/**
 * A touch drag from (x, y0) to (x, y1) through CDP touch events, which
 * Chromium turns into the pointer events the sheet listens to.
 */
async function touchDrag(page: Page, x: number, y0: number, y1: number, steps = 10): Promise<void> {
	const cdp = await page.context().newCDPSession(page);
	await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y: y0 }] });
	for (let i = 1; i <= steps; i++) {
		await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x, y: y0 + ((y1 - y0) * i) / steps }] });
	}
	await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
	await cdp.detach();
}

async function centre(page: Page, selector: string) {
	const b = await page.locator(selector).first().boundingBox();
	if (!b) throw new Error(`${selector} has no box`);
	return { x: b.x + b.width / 2, y: b.y + b.height / 2 };
}

test('1 · after unlock: top bar and peek sheet', async ({ page }) => {
	await unlock(page);
	await expect(topBar(page).getByRole('button', { name: 'Switch server: Demo' })).toBeVisible();
	await expect(peek(page)).toBeVisible();
	await expect(peek(page).getByRole('heading', { name: 'Online now' })).toBeVisible();
	await expect(page.getByTestId('online-count')).toHaveText('3 / 10');
	const chips = page.getByRole('list', { name: 'Online players' });
	for (const name of ['Astrid', 'Bjorn', 'Sigrun']) await expect(chips).toContainText(name);
});

test('2 · dragging the sheet up shows the pulled list', async ({ page }) => {
	await unlock(page);
	await expect(peek(page)).toBeVisible();
	const h = await centre(page, '[data-sheet-handle]');
	await touchDrag(page, h.x, h.y, 250);
	await expect(pulled(page)).toBeVisible();
	await expect(pulled(page).getByRole('list', { name: 'Online now' })).toContainText('Sigrun');
	await expect(pulled(page).getByRole('heading', { name: 'Recently online' })).toBeVisible();
	await expect(pulled(page).getByRole('list', { name: 'Recently online' })).toContainText('Ulf');
});

test('3 · the menu searches; picking a result docks the marker card', async ({ page }) => {
	await unlock(page);
	await page.getByRole('button', { name: 'Search and layers' }).tap();
	const menu = page.getByRole('dialog', { name: 'Search and layers' });
	await expect(menu).toBeVisible();
	await menu.getByRole('searchbox', { name: 'Search the map' }).fill('copper');
	const result = menu.getByRole('option').filter({ hasText: 'Portal · unpaired' });
	await expect(result).toHaveCount(1);
	await result.tap();
	await expect(menu).toBeHidden();
	const card = page.getByTestId('mobile-marker-card');
	await expect(card).toBeVisible();
	await expect(card).toContainText('copper');
	await expect(card.getByRole('button', { name: 'Close' })).toBeVisible();
	await card.getByRole('button', { name: 'Close' }).tap();
	await expect(card).toBeHidden();
	await expect(peek(page)).toBeVisible();
});

test('fix · mobile profile: "Map →" closes the sheet and focuses the docked marker card', async ({ page }) => {
	await unlock(page);
	const h = await centre(page, '[data-sheet-handle]');
	await touchDrag(page, h.x, h.y, 250);
	await expect(pulled(page)).toBeVisible();
	// .click() rather than .tap(): a second synthetic touch sequence right
	// after touchDrag's own CDP session is flaky here (observed hanging
	// without reaching the row's onclick); a plain click exercises the same
	// handler.
	await pulled(page).getByRole('button', { name: 'Profile of Astrid' }).click();
	const sheet = page.getByTestId('sheet-full');
	await expect(sheet).toBeVisible();
	await page.getByRole('button', { name: /Longhouse/ }).click();
	await expect(sheet).toHaveCount(0);
	const card = page.getByTestId('mobile-marker-card');
	await expect(card).toBeVisible();
	await expect(card).toContainText('Longhouse');
	await expect(card.getByRole('button', { name: 'Close' })).toBeFocused();
});

test('fix · a search result is activatable by keyboard (focus + Enter)', async ({ page }) => {
	await unlock(page);
	await page.getByRole('button', { name: 'Search and layers' }).tap();
	const menu = page.getByRole('dialog', { name: 'Search and layers' });
	await menu.getByRole('searchbox', { name: 'Search the map' }).fill('copper');
	const result = menu.getByRole('option').filter({ hasText: 'Portal · unpaired' });
	await expect(result).toHaveCount(1);
	// In the Tab order (the menu's rows are not driven by aria-activedescendant).
	await expect(result).toHaveAttribute('tabindex', '0');
	await result.focus();
	await expect(result).toBeFocused();
	await page.keyboard.press('Enter');
	await expect(menu).toBeHidden();
	await expect(page.getByTestId('mobile-marker-card')).toContainText('copper');
});

test('fix · a search result is activatable by a plain click event (assistive tech)', async ({ page }) => {
	await unlock(page);
	await page.getByRole('button', { name: 'Search and layers' }).tap();
	const menu = page.getByRole('dialog', { name: 'Search and layers' });
	await menu.getByRole('searchbox', { name: 'Search the map' }).fill('copper');
	const result = menu.getByRole('option').filter({ hasText: 'Portal · unpaired' });
	await expect(result).toHaveCount(1);
	await result.dispatchEvent('click');
	await expect(menu).toBeHidden();
	await expect(page.getByTestId('mobile-marker-card')).toContainText('copper');
});

test('4 · Join opens the full-height join sheet with the code', async ({ page }) => {
	await unlock(page);
	await peek(page).getByRole('button', { name: 'Join', exact: true }).tap();
	const sheet = page.getByTestId('sheet-full');
	await expect(sheet).toBeVisible();
	const dialog = sheet.getByRole('dialog', { name: 'Join Demo' });
	await expect(dialog.locator('.code')).toHaveText('318 742');
	// Full height: 8 px below the top (over the top bar) down to the bottom edge.
	await expect
		.poll(async () => {
			const b = (await sheet.boundingBox())!;
			return [Math.round(b.y), Math.round(b.y + b.height)];
		})
		.toEqual([8, 844]);
});

test('5 · tapping the server name opens the server sheet', async ({ page }) => {
	await unlock(page);
	await topBar(page).getByRole('button', { name: 'Switch server: Demo' }).tap();
	const sheet = page.getByRole('dialog', { name: 'Servers' });
	await expect(sheet).toBeVisible();
	await expect(sheet.getByRole('option', { name: /Demo/ })).toHaveAttribute('aria-selected', 'true');
	await expect(sheet.getByRole('button', { name: 'Add a server…' })).toBeVisible();
});

// --- Carried from reviews ----------------------------------------------------

test('carried · a tapped marker centres at y ≈ 300', async ({ page }) => {
	await unlock(page);
	await expect(page.locator('.leaflet-marker-icon.fs-pin').first()).toBeVisible();
	// The first single pin in the open map area, away from the edges and sheets.
	const target = await page.locator('.leaflet-marker-icon.fs-pin').evaluateAll((els) => {
		for (const e of els) {
			const r = e.getBoundingClientRect();
			const x = r.x + r.width / 2;
			const y = r.y + r.height / 2;
			if (x > 40 && x < 350 && y > 150 && y < 560) return { title: e.getAttribute('title')!, x, y };
		}
		return undefined;
	});
	expect(target, 'a single pin in the open map area').toBeDefined();
	expect(Math.abs(target!.y - 300)).toBeGreaterThan(20); // so the test shows movement
	await page.touchscreen.tap(target!.x, target!.y);
	await expect(page.getByTestId('mobile-marker-card')).toBeVisible();
	// The pin's centre, only once it holds still for two frames (the pan animates).
	const settledY = async () => {
		const y = async () => {
			const b = await pin(page, target!.title).boundingBox();
			return b ? Math.round(b.y + b.height / 2) : NaN;
		};
		const y0 = await y();
		await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
		const y1 = await y();
		return y0 === y1 ? y1 : NaN;
	};
	await expect
		.poll(async () => {
			const y = await settledY();
			return Number.isNaN(y) ? 'moving' : Math.abs(y - 300) <= 20 ? 'centred' : `at y=${y}`;
		})
		.toBe('centred');
});

// Review fix: as on desktop, the map's pins came first in Tab order.
test('fix · keyboard: Tab reaches the top bar, the sheet and zoom before the first map pin', async ({ page }) => {
	await unlock(page);
	await expect(page.locator('.leaflet-marker-icon.fs-pin').first()).toBeVisible();
	const seen = await tabToFirstPin(page);
	expect(seen.at(-1), 'the pins are still reachable').toBe('pin');
	for (const label of ['Switch server: Demo', 'Search and layers', 'Expand sheet', 'Zoom in']) {
		expect(seen, `${label} before the first pin`).toContain(label);
	}
});

// Crossing the 768 px breakpoint mid-zoom (a tablet rotating) swaps the
// shells, removing the map while Leaflet's zoom animation is still running;
// its end-of-animation timer then threw on the removed map (the guard
// fixture fails the test on any page error).
test('fix · switching shells mid-zoom leaves no error behind', async ({ page }) => {
	await unlock(page);
	await expect(page.locator('.leaflet-marker-icon.fs-pin').first()).toBeVisible();
	await page.getByRole('button', { name: 'Zoom in' }).click();
	await page.setViewportSize({ width: 1024, height: 844 });
	await expect(page.getByRole('combobox', { name: 'Search the map' })).toBeVisible();
	await page.waitForTimeout(500);
});

test('carried · Esc on the pulled sheet collapses it to peek', async ({ page }) => {
	await unlock(page);
	await handle(page).tap();
	await expect(pulled(page)).toBeVisible();
	await expect(handle(page)).toHaveAccessibleName('Collapse sheet');
	await page.keyboard.press('Escape');
	await expect(peek(page)).toBeVisible();
	await expect(pulled(page)).toHaveCount(0);
	await expect(handle(page)).toHaveAccessibleName('Expand sheet');
});

// --- Plan 7: player profiles and the activity timeline ----------------------

test('profile · pulled sheet → Profile: the full-height sheet; Map → docks the card; back closes', async ({ page }) => {
	await unlock(page);
	// A tap, not a drag: a tap right after a touch drag can land as the
	// fling-stopping tap, which fires no click.
	await handle(page).tap();
	await expect(pulled(page)).toBeVisible();
	await pulled(page).getByRole('button', { name: 'Profile of Astrid' }).tap();
	const sheet = page.getByRole('dialog', { name: 'Player profile' });
	await expect(sheet.getByRole('heading', { name: 'Astrid' })).toBeVisible();
	await expect(sheet.locator('.stat', { hasText: 'Sessions' }).locator('dd')).toHaveText('3');
	await expect(page).toHaveURL(/#s=demo&p=76561190000000001$/);
	await page.goBack();
	await expect(sheet).toBeHidden();

	await pulled(page).getByRole('button', { name: 'Profile of Astrid' }).tap();
	await sheet.getByRole('region', { name: 'Bases' }).getByRole('button', { name: /Longhouse/ }).tap();
	await expect(sheet).toBeHidden();
	await expect(page.getByTestId('mobile-marker-card')).toContainText('Longhouse');
	await expect(page).toHaveURL(/#s=demo$/);
});

test('profile · Recently online → Ulf: figures from the seed, Map →, Back button, focus returns', async ({ page }) => {
	await unlock(page);
	await handle(page).tap();
	await expect(pulled(page)).toBeVisible();
	const row = pulled(page).getByRole('button', { name: 'Profile of Ulf' });
	await row.tap();
	const sheet = page.getByRole('dialog', { name: 'Player profile' });
	await expect(sheet.getByRole('heading', { name: 'Ulf' })).toBeVisible();
	await expect(sheet.locator('.stat', { hasText: 'Sessions' }).locator('dd')).toHaveText('2');
	await expect(sheet.getByRole('region', { name: 'Bases' })).toContainText('Eastwatch');
	await expect(page).toHaveURL(/#s=demo&p=76561190000000004$/);

	// Back first: unlike "Map →" (below), it doesn't collapse the pulled
	// sheet, so the same `row` still resolves for the second open.
	await sheet.getByRole('button', { name: 'Back' }).tap();
	await expect(sheet).toBeHidden();
	await expect(page).toHaveURL(/#s=demo$/);

	await row.tap();
	await expect(sheet).toBeVisible();
	await sheet.getByRole('region', { name: 'Bases' }).getByRole('button', { name: /Eastwatch/ }).tap();
	await expect(sheet).toBeHidden();
	await expect(page.getByTestId('mobile-marker-card')).toContainText('Eastwatch');
});

test('activity · menu → Full timeline: chips, Show earlier, Show on map →', async ({ page }) => {
	await unlock(page);
	await page.getByRole('button', { name: 'Search and layers' }).tap();
	await page.getByRole('dialog', { name: 'Search and layers' }).getByRole('button', { name: /Full timeline/ }).tap();
	const sheet = page.getByRole('dialog', { name: 'Activity' });
	await expect(sheet).toContainText('Moder defeated');
	await expect(page).toHaveURL(/#s=demo&activity$/);
	const bosses = sheet.getByRole('button', { name: /^Bosses/ });
	await bosses.tap();
	await expect(sheet).not.toContainText('Moder defeated');
	await bosses.tap();
	await sheet.getByRole('button', { name: 'Show earlier' }).tap();
	await expect(sheet).toContainText('Sigrun left after 1h 00m');

	await sheet.getByRole('listitem').filter({ hasText: 'New portal “copper”' }).getByRole('button', { name: 'Show on map →' }).tap();
	await expect(sheet).toBeHidden();
	await expect(page.getByTestId('mobile-marker-card')).toContainText('copper');
});

test('activity · a world event in unexplored ground never reaches the mobile timeline', async ({ page }) => {
	await unlock(page);
	await page.getByRole('button', { name: 'Search and layers' }).tap();
	await page.getByRole('dialog', { name: 'Search and layers' }).getByRole('button', { name: /Full timeline/ }).tap();
	const sheet = page.getByRole('dialog', { name: 'Activity' });
	await expect(sheet).toContainText('New portal “copper”, not paired with anything yet');
	await expect(sheet).not.toContainText('New portal “mountain”');
});

test('activity · the menu button opens the sheet, and Back closes it and returns focus', async ({ page }) => {
	await unlock(page);
	const menuBtn = page.getByRole('button', { name: 'Search and layers' });
	await menuBtn.tap();
	await page.getByRole('dialog', { name: 'Search and layers' }).getByRole('button', { name: /Full timeline/ }).tap();
	const sheet = page.getByRole('dialog', { name: 'Activity' });
	await expect(sheet).toBeVisible();
	await expect(page).toHaveURL(/#s=demo&activity$/);

	await sheet.getByRole('button', { name: 'Back' }).tap();
	await expect(sheet).toBeHidden();
	await expect(page).toHaveURL(/#s=demo$/);
	await expect(menuBtn).toBeFocused();
});

// --- Plan 9: time and weather -------------------------------------------------

test('weather · the chip opens the sheet, listing the seeded explored biomes', async ({ page }) => {
	await unlock(page);
	const chip = page.getByTestId('weather-chip');
	await expect(chip).toBeVisible();
	// The weather word (Clear, Rain, …) depends on wall-clock time (the
	// period draw), so only the label/clock structure is asserted here.
	await expect(chip).toHaveAttribute('aria-label', /^Time and weather: (Morning|Day|Evening|Night) · \d{2}:\d{2} · .+$/);
	await chip.tap();

	const sheet = page.getByRole('dialog', { name: 'Time and weather' });
	await expect(sheet).toBeVisible();
	await expect(sheet).toContainText(/Day \d+ ·/);

	// global-setup.ts posts snapshot.json with -explored; these six biomes
	// are exactly what that fixture's exploredZones cover, independent of
	// wall-clock time (unlike the weather itself).
	const table = sheet.getByRole('table', { name: 'Weather by biome' });
	for (const biome of ['Ocean', 'Meadows', 'Black Forest', 'Swamp', 'Mountains', 'Plains']) {
		await expect(table.getByText(biome, { exact: true })).toHaveCount(1);
	}

	await page.keyboard.press('Escape');
	await expect(sheet).toBeHidden();
});
