// App-wide reactive state: the unlocked servers, the current server's card and
// snapshot, polling, theme, toasts and the unlock flow. Components read the
// `app` singleton; every browser dependency is injectable so the logic is
// testable under Vitest (see state.svelte.test.ts).
//
// Refresh cadence (plan ruling): card every 15 s while the tab is visible and
// immediately on becoming visible; servers every 60 s; the snapshot whenever
// `card.world.savedAt` changes. `now` ticks every 30 s.

import { ApiError, getCard, getSnapshot, listServers, unlock } from './api';
import type { TileSample } from './derive';
import { hashFor, parseHash, sameView, type View } from './share';
import type { Card, ServerSummary, SnapshotView } from './types';

export type Theme = 'dark' | 'light';
export type UnlockResult = 'ok' | 'wrong' | 'limited';

export const THEME_KEY = 'farsight.theme';
export const CARD_EVERY_MS = 15_000;
export const SERVERS_EVERY_MS = 60_000;
export const NOW_EVERY_MS = 30_000;
export const TOAST_MS = 2600;
const MAX_TILE_SAMPLES = 5;

export interface Visibility {
	visible(): boolean;
	/** Calls `cb` whenever the page becomes visible; returns an unsubscribe. */
	onVisible(cb: () => void): () => void;
}

export interface AppDeps {
	fetch?: typeof fetch;
	storage?: Storage | null;
	location?: Pick<Location, 'hash'>;
	history?: Pick<History, 'replaceState' | 'state' | 'back'>;
	/** Calls `cb` on every `hashchange` (browser back and forward); returns an unsubscribe. */
	onHashChange?: (cb: () => void) => () => void;
	visibility?: Visibility;
	/** Element that carries `data-theme` (the `<html>` element in the browser). */
	root?: { dataset: DOMStringMap | Record<string, string | undefined> } | null;
	/** Logs one line per failure streak. */
	log?: (message: string, err: unknown) => void;
}

export interface UnlockPrompt {
	server?: string;
	error?: string;
	/** Where focus returns when the dialog closes (the switcher that opened it). */
	returnTo?: () => HTMLElement | null | undefined;
}

export function unlockMessage(r: Exclude<UnlockResult, 'ok'> | 'error'): string {
	if (r === 'wrong') return 'Wrong passphrase.';
	if (r === 'limited') return 'Too many attempts. Try again in a few minutes.';
	return 'Couldn’t reach Farsight. Check your connection and try again.';
}

const hasDocument = () => typeof document !== 'undefined';

function documentVisibility(): Visibility {
	return {
		visible: () => !hasDocument() || document.visibilityState !== 'hidden',
		onVisible(cb) {
			if (!hasDocument()) return () => {};
			const h = () => {
				if (document.visibilityState === 'visible') cb();
			};
			document.addEventListener('visibilitychange', h);
			return () => document.removeEventListener('visibilitychange', h);
		}
	};
}

function defaultStorage(): Storage | null {
	try {
		return typeof localStorage !== 'undefined' ? localStorage : null;
	} catch {
		return null;
	}
}

/** A running refresh: its generation, whether another run was asked for meanwhile, and its promise. */
interface Flight {
	gen: number;
	again: boolean;
	done: Promise<void>;
}

export class AppState {
	servers = $state<ServerSummary[]>([]);
	/** The first /api/servers call has completed. */
	loaded = $state(false);
	currentId = $state<string | undefined>(undefined);
	card = $state<Card | undefined>(undefined);
	/** undefined = not loaded, null = none yet. */
	snapshot = $state<SnapshotView | null | undefined>(undefined);
	theme = $state<Theme>('dark');
	now = $state(new Date());
	tileSamples: TileSample[] = [];
	toast = $state<{ title: string; sub?: string } | undefined>(undefined);
	/** Set when the unlock dialog should be shown (share link failed, "Add a server…"). */
	unlockPrompt = $state<UnlockPrompt | undefined>(undefined);
	/** The open view (a profile or the activity timeline), mirrored in the URL hash. */
	view = $state<View | undefined>(undefined);

	private f: typeof fetch;
	private storage: Storage | null;
	private loc: Pick<Location, 'hash'> | undefined;
	private hist: Pick<History, 'replaceState' | 'state' | 'back'> | undefined;
	private onHashChange: (cb: () => void) => () => void;
	/** The open view added a history entry (openView), so closing it goes back. */
	private pushed = false;
	private vis: Visibility;
	private root: AppDeps['root'];
	private log: (message: string, err: unknown) => void;

	private savedAt: string | undefined;
	/** Bumped by a server switch (select, moveOff); responses from older generations are dropped. */
	private gen = 0;
	/** The refresh in flight, if any (single flight per generation). */
	private flight: Flight | undefined;
	private stopFn: (() => void) | undefined;
	private failing = false;
	private toastTimer: ReturnType<typeof setTimeout> | undefined;

	constructor(deps: AppDeps = {}) {
		this.f = deps.fetch ?? ((input, init) => fetch(input, init));
		this.storage = deps.storage !== undefined ? deps.storage : defaultStorage();
		this.loc = deps.location ?? (typeof location !== 'undefined' ? location : undefined);
		this.hist = deps.history ?? (typeof history !== 'undefined' ? history : undefined);
		this.vis = deps.visibility ?? documentVisibility();
		this.root = deps.root !== undefined ? deps.root : hasDocument() ? document.documentElement : null;
		this.log = deps.log ?? ((m, e) => console.warn(m, e));
		this.onHashChange =
			deps.onHashChange ??
			((cb) => {
				if (typeof window === 'undefined') return () => {};
				window.addEventListener('hashchange', cb);
				return () => window.removeEventListener('hashchange', cb);
			});
	}

	/** Reads the theme and hash, unlocks a share link, loads servers and starts timers. Returns stop(). */
	start(): () => void {
		// Idempotent while running: a second call returns the same stop().
		if (this.stopFn) return this.stopFn;
		let stopped = false;
		this.theme = this.readTheme();
		this.applyTheme();

		void this.boot(() => stopped);

		const timers = [
			setInterval(() => {
				if (this.vis.visible()) void this.refresh();
			}, CARD_EVERY_MS),
			setInterval(() => void this.refreshServers(), SERVERS_EVERY_MS),
			setInterval(() => (this.now = new Date()), NOW_EVERY_MS)
		];
		const offVisible = this.vis.onVisible(() => {
			this.now = new Date();
			void this.refresh();
		});
		const offHash = this.onHashChange(() => this.syncView());

		const stop = () => {
			if (stopped) return;
			stopped = true;
			for (const t of timers) clearInterval(t);
			offVisible();
			offHash();
			clearTimeout(this.toastTimer);
			if (this.stopFn === stop) this.stopFn = undefined;
		};
		this.stopFn = stop;
		return stop;
	}

	private async boot(stopped: () => boolean): Promise<void> {
		const link = parseHash(this.loc?.hash ?? '');
		if (link.server && link.key) {
			// The passphrase leaves the address bar (and history) before the
			// request, so a hanging unlock never leaves it visible.
			const key = link.key;
			this.replaceHash(link.server);
			let result: UnlockResult | 'error';
			try {
				result = await this.tryUnlock(link.server, key);
			} catch (err) {
				this.log('farsight: unlock failed', err);
				result = 'error';
			}
			if (result !== 'ok') this.unlockPrompt = { server: link.server, error: unlockMessage(result) };
		}
		if (stopped()) return;
		if (!this.loaded) await this.refreshServers();
		if (stopped() || this.currentId !== undefined) return;
		const pick = this.servers.find((s) => s.id === link.server)?.id ?? this.servers[0]?.id;
		if (pick === undefined) return;
		this.select(pick);
		// A linked profile or timeline opens over its server. It added no
		// history entry, so closing it replaces the hash instead of going back.
		if (pick === link.server && link.view) {
			this.view = link.view;
			this.replaceHash(pick);
		}
	}

	/** Switches server: updates the hash (replaceState), clears the old data and loads at once. */
	select(id: string): void {
		this.gen++;
		this.currentId = id;
		this.view = undefined;
		this.pushed = false;
		this.card = undefined;
		this.snapshot = undefined;
		this.savedAt = undefined;
		this.tileSamples = [];
		this.replaceHash(id);
		void this.refresh();
	}

	/**
	 * Fetches the current card, and the snapshot when `world.savedAt` changed.
	 *
	 * Single flight per server: while a refresh for the current server is in
	 * flight, another call (a tick, the tab becoming visible) starts nothing
	 * and asks for one follow-up run after it, so a snapshot download slower
	 * than the poll interval still lands. Only a server switch (`select`,
	 * `moveOff`) bumps the generation, which discards the old flight.
	 */
	refresh(): Promise<void> {
		if (this.currentId === undefined) return Promise.resolve();
		const g = this.gen;
		if (this.flight && this.flight.gen === g) {
			this.flight.again = true;
			return this.flight.done;
		}
		const flight: Flight = { gen: g, again: false, done: Promise.resolve() };
		this.flight = flight;
		flight.done = (async () => {
			try {
				do {
					flight.again = false;
					const id = this.currentId;
					if (id === undefined || g !== this.gen) return;
					await this.load(id, g);
				} while (flight.again && g === this.gen);
			} finally {
				if (this.flight === flight) this.flight = undefined;
			}
		})();
		return flight.done;
	}

	private async load(id: string, g: number): Promise<void> {
		const stale = () => g !== this.gen || this.currentId !== id;
		try {
			const card = await getCard(id, this.f);
			if (stale()) return;
			this.card = card;
			this.sampleTiles(card);
			this.ok();
			const savedAt = card.world?.savedAt;
			if (savedAt === undefined) {
				this.savedAt = undefined;
				this.snapshot = null;
			} else if (savedAt !== this.savedAt) {
				const snap = await getSnapshot(id, this.f);
				if (stale()) return;
				// A 404 here races the card (the save isn't stored yet): keep
				// what is shown and don't record savedAt, so the next poll
				// fetches it again. Same for a failure (thrown below).
				if (snap === null) return;
				this.snapshot = snap;
				this.savedAt = savedAt;
			}
		} catch (err) {
			if (stale()) return;
			if (err instanceof ApiError && err.status === 404) {
				this.dropServer(id);
				return;
			}
			this.fail(err);
		}
	}

	async refreshServers(): Promise<void> {
		try {
			const list = await listServers(this.f);
			this.ok();
			const cur = this.currentId;
			// moveOff reads the neighbour index from the old list, so it runs first.
			if (cur !== undefined && !list.some((s) => s.id === cur)) this.moveOff(cur, list);
			this.servers = list;
		} catch (err) {
			this.fail(err);
		} finally {
			this.loaded = true;
		}
	}

	/** POSTs the unlock. On 'ok' reloads servers and selects `server`. The passphrase is not kept. */
	async tryUnlock(server: string, passphrase: string): Promise<UnlockResult> {
		const r = await unlock(server, passphrase, this.f);
		if (r === 'ok') {
			await this.refreshServers();
			this.select(server);
		}
		return r;
	}

	setTheme(t: Theme): void {
		this.theme = t;
		try {
			this.storage?.setItem(THEME_KEY, t);
		} catch {
			// Private mode or blocked storage: the choice just won't persist.
		}
		this.applyTheme();
	}

	showToast(title: string, sub?: string): void {
		clearTimeout(this.toastTimer);
		this.toast = sub === undefined ? { title } : { title, sub };
		this.toastTimer = setTimeout(() => (this.toast = undefined), TOAST_MS);
	}

	openUnlock(server?: string, returnTo?: () => HTMLElement | null | undefined): void {
		const p: UnlockPrompt = server === undefined ? {} : { server };
		if (returnTo) p.returnTo = returnTo;
		this.unlockPrompt = p;
	}

	closeUnlock(): void {
		this.unlockPrompt = undefined;
	}

	/**
	 * Opens a profile or the activity timeline. The first view opened adds a
	 * history entry (so browser back closes it); opening another over it
	 * replaces that entry.
	 */
	openView(v: View): void {
		const id = this.currentId;
		if (id === undefined || sameView(v, this.view)) return;
		const replace = this.view !== undefined;
		this.view = v;
		if (replace || !this.loc) {
			this.replaceHash(id);
			return;
		}
		this.pushed = true;
		this.loc.hash = hashFor(id, v);
	}

	/** Closes the open view: back through its history entry, or by replacing the hash. */
	closeView(): void {
		if (this.view === undefined) return;
		this.view = undefined;
		if (this.pushed && this.hist) {
			this.pushed = false;
			this.hist.back();
			return;
		}
		if (this.currentId !== undefined) this.replaceHash(this.currentId);
	}

	/** Follows the hash after browser back or forward. */
	private syncView(): void {
		const link = parseHash(this.loc?.hash ?? '');
		if (link.server !== this.currentId) return;
		if (!sameView(link.view, this.view)) this.view = link.view;
		if (!link.view) this.pushed = false;
	}

	// --- internals ------------------------------------------------------------

	private readTheme(): Theme {
		try {
			const v = this.storage?.getItem(THEME_KEY);
			return v === 'light' || v === 'dark' ? v : 'dark';
		} catch {
			return 'dark';
		}
	}

	private applyTheme(): void {
		if (this.root) this.root.dataset.theme = this.theme;
	}

	private replaceHash(id: string): void {
		try {
			// Keep history.state: SvelteKit stores its navigation index there.
			this.hist?.replaceState(this.hist.state, '', hashFor(id, this.view));
		} catch (err) {
			this.log('farsight: could not update the URL', err);
		}
	}

	private sampleTiles(card: Card): void {
		const { state, done } = card.tiles;
		if (state === 'queued' || state === 'rendering') {
			this.tileSamples = [...this.tileSamples, { at: Date.now(), done }].slice(-MAX_TILE_SAMPLES);
		} else {
			this.tileSamples = [];
		}
	}

	/** The server got locked (404): forget it and move to the next one. */
	private dropServer(id: string): void {
		const rest = this.servers.filter((s) => s.id !== id);
		this.moveOff(id, rest);
		this.servers = rest;
	}

	/** Leaves `id`, which is not in `rest`: selects its neighbour, or nothing. */
	private moveOff(id: string, rest: ServerSummary[]): void {
		const idx = Math.max(0, this.servers.findIndex((s) => s.id === id));
		const next = rest[Math.min(idx, rest.length - 1)];
		if (next) {
			this.select(next.id);
			return;
		}
		this.gen++;
		this.currentId = undefined;
		this.card = undefined;
		this.snapshot = undefined;
		this.savedAt = undefined;
		this.tileSamples = [];
	}

	private ok(): void {
		this.failing = false;
	}

	private fail(err: unknown): void {
		if (this.failing) return;
		this.failing = true;
		this.log('farsight: refresh failed; keeping the last data', err);
	}
}

export const app = new AppState();
