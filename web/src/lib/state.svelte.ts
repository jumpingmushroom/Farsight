// App-wide reactive state: the unlocked servers, the current server's card and
// snapshot, polling, theme, toasts and the unlock flow. Components read the
// `app` singleton; every browser dependency is injectable so the logic is
// testable under Vitest (see state.svelte.test.ts).
//
// Refresh cadence (plan ruling): card every 15 s while the tab is visible and
// immediately on becoming visible; servers every 60 s; the snapshot whenever
// `card.world.savedAt` changes. `now` ticks every 30 s.

import { ApiError, getCard, getProfile, getSnapshot, listServers, unlock } from './api';
import type { TileSample } from './derive';
import { hashFor, parseHash, sameView, type View } from './share';
import type { Card, Profile, ServerSummary, SnapshotView } from './types';

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
	/**
	 * The open profile (fix round 1): undefined while loading or when no
	 * profile view is open, null when the server has never seen the
	 * player. Kept in AppState (not the component) so it refreshes on the
	 * same poll as the card, rather than only once on open.
	 */
	profile = $state<Profile | null | undefined>(undefined);
	/** The last profile fetch failed; retryProfile() tries again at once. */
	profileFailed = $state(false);
	/**
	 * The name shown on the row that opened the current view (fix round
	 * 1), set only on the first open (not when one view replaces
	 * another). DesktopShell reads and clears this to refocus that row —
	 * or a fallback, if it's gone — once the view closes, since the row
	 * fully unmounts while a profile is shown. Mobile doesn't need this:
	 * its row stays mounted under the sheet.
	 */
	viewOpenerName: string | undefined;

	private f: typeof fetch;
	private storage: Storage | null;
	private loc: Pick<Location, 'hash'> | undefined;
	private hist: Pick<History, 'replaceState' | 'state' | 'back'> | undefined;
	private onHashChange: (cb: () => void) => () => void;
	/** The open view added a history entry (openView), so closing it goes back. */
	private pushed = false;
	/**
	 * Set by select() when it must pop a pushed view's entry before
	 * switching server (fix round 1): the very next hashchange is that pop
	 * landing, whatever it carries, so syncView() only uses it to fix up
	 * the hash for the new server, instead of parsing it as a link.
	 */
	private pendingPop = false;
	/** (id, player) the shown profile/profileFailed are for, or the one just requested. */
	private profileFor: { id: string; player: string } | undefined;
	/** Bumped on every (re)fetch, close or switch: a stale fetch's result is dropped, which is as close to "abort" as a plain fetch gets. */
	private profileGen = 0;
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
			this.syncProfile();
		}
	}

	/** Switches server: updates the hash (replaceState), clears the old data and loads at once. */
	select(id: string): void {
		this.gen++;
		const popView = this.pushed && !!this.hist;
		this.currentId = id;
		this.view = undefined;
		this.pushed = false;
		this.card = undefined;
		this.snapshot = undefined;
		this.savedAt = undefined;
		this.tileSamples = [];
		this.syncProfile();
		if (popView) {
			// The open view had pushed a history entry. Replacing it outright
			// would leave the entry beneath it (the pre-view hash for the OLD
			// server) sitting right behind the new server's hash, so Back
			// would land on a stale "#s=<old>" while the app already shows
			// this one. Popping it first keeps that entry honest; syncView()
			// finishes the swap once the resulting hashchange lands.
			this.pendingPop = true;
			this.hist!.back();
		} else {
			this.replaceHash(id);
		}
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
			// A quiet refresh of the open profile, on the same cadence as the
			// card: it never clears what's shown (no skeleton), so a status
			// change (e.g. the player going offline) lands without reopening.
			this.syncProfile(true);
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
	 * replaces that entry. `openerName`, the name on the row that opened it,
	 * is kept only for the first open (desktop focus restore; fix round 1).
	 */
	openView(v: View, openerName?: string): void {
		const id = this.currentId;
		if (id === undefined || sameView(v, this.view)) return;
		const replace = this.view !== undefined;
		if (!replace) this.viewOpenerName = openerName;
		this.view = v;
		if (replace || !this.loc) {
			this.replaceHash(id);
		} else {
			this.pushed = true;
			this.loc.hash = hashFor(id, v);
		}
		this.syncProfile();
	}

	/** Closes the open view: back through its history entry, or by replacing the hash. */
	closeView(): void {
		if (this.view === undefined) return;
		this.view = undefined;
		if (this.pushed && this.hist) {
			this.pushed = false;
			this.hist.back();
		} else if (this.currentId !== undefined) {
			this.replaceHash(this.currentId);
		}
		this.syncProfile();
	}

	/** Retries a failed profile fetch at once, instead of waiting for the next poll. */
	retryProfile(): void {
		this.syncProfile(true);
	}

	/** Follows the hash after browser back or forward. */
	private syncView(): void {
		if (this.pendingPop) {
			// Our own select()-triggered pop landing: finish the hash swap for
			// the new server, whatever this hashchange carries.
			this.pendingPop = false;
			if (this.currentId !== undefined) this.replaceHash(this.currentId);
			return;
		}
		const link = parseHash(this.loc?.hash ?? '');
		if (link.server !== this.currentId) return;
		if (!sameView(link.view, this.view)) this.view = link.view;
		// A view reopened by browser forward (or back) still has a history
		// entry of its own, so closing it again must go back through it too.
		this.pushed = !!link.view;
		this.syncProfile();
	}

	/**
	 * Fetches or refreshes the open profile: a fresh load (clearing
	 * `profile`/`profileFailed` so the skeleton shows again) when the view
	 * just opened or now names a different player; otherwise, when `poll`
	 * is true, a quiet background refresh that only updates `profile` /
	 * `profileFailed` once it lands, so a card poll never flickers the
	 * panel. With no profile view open, this discards whatever the last
	 * fetch was doing and clears both fields — covering close and
	 * switching server, which call it with `this.view` already cleared.
	 */
	private syncProfile(poll = false): void {
		const id = this.currentId;
		const view = this.view;
		if (id === undefined || view?.kind !== 'profile') {
			if (this.profileFor !== undefined) {
				this.profileGen++;
				this.profileFor = undefined;
				this.profile = undefined;
				this.profileFailed = false;
			}
			return;
		}
		const player = view.player;
		const fresh = this.profileFor?.id !== id || this.profileFor?.player !== player;
		if (!fresh && !poll) return;
		if (fresh) {
			this.profile = undefined;
			this.profileFailed = false;
		}
		this.profileFor = { id, player };
		this.profileGen++;
		const g = this.profileGen;
		void (async () => {
			try {
				const p = await getProfile(id, player, this.f);
				if (g !== this.profileGen) return;
				this.profile = p;
				this.profileFailed = false;
			} catch (err) {
				if (g !== this.profileGen) return;
				this.profileFailed = true;
				this.log('farsight: profile refresh failed', err);
			}
		})();
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
		this.view = undefined;
		this.pushed = false;
		this.card = undefined;
		this.snapshot = undefined;
		this.savedAt = undefined;
		this.tileSamples = [];
		this.syncProfile();
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
