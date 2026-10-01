// Desktop (1440×900) e2e against the seeded `demo` and unseeded `quiet`
// servers (global-setup.ts). Numbered tests follow the Task 9 brief; the
// "carried" ones are the regressions carried over from reviews.
import { test as pwTest } from '@playwright/test';
import type { Page } from '@playwright/test';
import { checkGuard, expect, pinsOfKind, test, unlock, watchGuard } from './helpers';

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

test('3 · tiles load from the seeded tile set', async ({ page }) => {
	const tileUrls: string[] = [];
	page.on('request', (r) => {
		if (r.url().includes('/tiles/')) tileUrls.push(new URL(r.url()).pathname);
	});
	await unlock(page);
	await expect
		.poll(() =>
			page.locator('img.leaflet-tile').evaluateAll((imgs) => imgs.filter((i) => (i as HTMLImageElement).naturalWidth === 256).length)
		)
		.toBeGreaterThan(0);
	expect(tileUrls.length).toBeGreaterThan(0);
	for (const u of tileUrls) expect(u).toMatch(/^\/tiles\/demo\/12345-2-r\d+\/\d+\/\d+\/\d+\.png$/);
});

test('fix · a fast wheel zoom-out never bares terrain past the shrinking fog canvas', async ({ page }) => {
	await unlock(page);
	await expect(page.locator('canvas.fs-fog')).toHaveCount(1);
	const box = (await page.locator('.leaflet-container').first().boundingBox())!;
	await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
	for (let i = 0; i < 6; i++) {
		await page.mouse.wheel(0, -300);
		await page.waitForTimeout(400);
	}
	await page.waitForTimeout(1000);
	// Every frame: wherever the (transformed) fog canvas leaves the map
	// bare, the fog skirt must be showing.
	await page.evaluate(() => {
		const w = window as unknown as { __bare: number; __frames: number; __stop: boolean };
		w.__bare = 0;
		w.__frames = 0;
		w.__stop = false;
		const map = document.querySelector('.leaflet-container')!;
		const tick = () => {
			const fog = document.querySelector('canvas.fs-fog');
			const skirt = document.querySelector<HTMLElement>('.fs-fog-skirt');
			if (fog) {
				const a = fog.getBoundingClientRect();
				const b = map.getBoundingClientRect();
				const bare = a.left > b.left + 1 || a.top > b.top + 1 || a.right < b.right - 1 || a.bottom < b.bottom - 1;
				if (bare && (!skirt || skirt.hidden)) w.__bare++;
				w.__frames++;
			}
			if (!w.__stop) requestAnimationFrame(tick);
		};
		requestAnimationFrame(tick);
	});
	for (let i = 0; i < 4; i++) {
		await page.mouse.wheel(0, 400);
		await page.waitForTimeout(60);
	}
	await page.waitForTimeout(1500);
	const r = await page.evaluate(() => {
		const w = window as unknown as { __bare: number; __frames: number; __stop: boolean };
		w.__stop = true;
		return { bare: w.__bare, frames: w.__frames };
	});
	expect(r.frames).toBeGreaterThan(10);
	expect(r.bare, 'frames with terrain bared past the fog').toBe(0);
	// Once the repaint lands, the canvas covers the view and the skirt hides.
	await expect(page.locator('.fs-fog-skirt')).toBeHidden();
});

test('fix · fog and terrain stay at the same zoom in every frame of a fast wheel zoom', async ({ page }) => {
	await unlock(page);
	await expect(page.locator('canvas.fs-fog')).toHaveCount(1);
	await page.waitForTimeout(1000);
	// Each frame: the fog's effective zoom (the zoom it was drawn at plus its
	// CSS scale) against the topmost tile level with a loaded tile (its zoom
	// plus the tile's rendered scale). They differ when the fog lags the
	// terrain — e.g. new-level tiles shown at the target zoom while the fog
	// is still on its way there.
	await page.evaluate(() => {
		const w = window as unknown as { __off: number[]; __frames: number; __stop: boolean };
		w.__off = [];
		w.__frames = 0;
		w.__stop = false;
		const tick = () => {
			const fog = document.querySelector<HTMLCanvasElement>('canvas.fs-fog');
			let best: { z: number; img: HTMLImageElement } | null = null;
			let bestZ = -Infinity;
			for (const c of document.querySelectorAll<HTMLElement>('.leaflet-tile-container')) {
				const img = [...c.querySelectorAll<HTMLImageElement>('img.leaflet-tile-loaded')].find((i) => i.complete && i.naturalWidth > 0);
				const m = img?.src.match(/\/(\d+)\/-?\d+\/-?\d+\.png/);
				const zi = Number(c.style.zIndex || 0);
				if (img && m && zi > bestZ) {
					bestZ = zi;
					best = { z: Number(m[1]), img };
				}
			}
			if (fog?.dataset.zoom && best) {
				const fogEff = Number(fog.dataset.zoom) + Math.log2(fog.getBoundingClientRect().width / parseFloat(fog.style.width));
				const tileEff = best.z + Math.log2(best.img.getBoundingClientRect().width / 256);
				w.__off.push(Math.abs(fogEff - tileEff));
				w.__frames++;
			}
			if (!w.__stop) requestAnimationFrame(tick);
		};
		requestAnimationFrame(tick);
	});
	const box = (await page.locator('.leaflet-container').first().boundingBox())!;
	await page.mouse.move(box.x + box.width * 0.6, box.y + box.height / 2);
	for (let i = 0; i < 6; i++) {
		await page.mouse.wheel(0, -200);
		await page.waitForTimeout(50);
	}
	await page.waitForTimeout(2000);
	for (let i = 0; i < 6; i++) {
		await page.mouse.wheel(0, 200);
		await page.waitForTimeout(50);
	}
	await page.waitForTimeout(2000);
	const r = await page.evaluate(() => {
		const w = window as unknown as { __off: number[]; __frames: number; __stop: boolean };
		w.__stop = true;
		return { frames: w.__frames, worst: Math.max(0, ...w.__off), off: w.__off.filter((d) => d > 0.05).length };
	});
	expect(r.frames).toBeGreaterThan(10);
	expect(r.off, `frames with fog and terrain at different zooms (worst ${r.worst.toFixed(2)} levels)`).toBe(0);
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
	await expect(lines).toHaveCount(2);
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

test('11 · the crypt outside the explored zones never appears', async ({ page }) => {
	const snapshot = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/servers/demo/snapshot');
	await unlock(page);
	// The server filters locations by explored zone: only the inside crypt ships.
	const body = await (await snapshot).json();
	const crypts = (body.locations as { id: string; type: string }[]).filter((l) => l.type === 'SunkenCrypt4');
	expect(crypts.map((l) => l.id)).toEqual(['loc-140']);
	await markersReady(page);
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
