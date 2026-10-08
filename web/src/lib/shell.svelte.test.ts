import './testing/leaflet-node';
import { describe, expect, test, vi } from 'vitest';
import { ShellModel, ShellPrefs, keyOf } from './shell.svelte';
import { AppState } from './state.svelte';
import { fixtureSnapshot, fixtureWorld } from './testing/markers-fixture';
import type { Card } from './types';

// Effects don't run under Vitest (Node, Svelte's server build), so the
// effect bodies are public methods (checkSelection, checkServer) that the
// tests call directly.

function makeCard(id: string, over: Partial<Card> = {}): Card {
	return {
		id,
		name: 'Testheim',
		crossplay: false,
		maxPlayers: 10,
		status: 'online',
		players: 0,
		online: [],
		recent: [],
		activity: [],
		world: fixtureWorld(),
		tiles: { state: 'complete', done: 10, total: 10, key: 'k1' },
		...over
	};
}

function setup(opts: { prefs?: ShellPrefs; onselect?: (id: string | undefined) => void; onswitch?: () => void } = {}) {
	const app = new AppState({ storage: null, root: null, location: { hash: '' }, onHashChange: () => () => {} });
	app.currentId = 'a';
	app.card = makeCard('a');
	app.snapshot = fixtureSnapshot();
	app.now = new Date('2026-09-30T10:05:00Z');
	const prefs = opts.prefs ?? new ShellPrefs();
	const model = new ShellModel({ app, prefs, onselect: opts.onselect, onswitch: opts.onswitch });
	return { app, prefs, model };
}

describe('ShellModel', () => {
	test('reads the current server’s card only', () => {
		const { app, model } = setup();
		expect(model.card?.id).toBe('a');
		app.currentId = 'b';
		expect(model.card).toBeUndefined();
	});

	test('memoises the markers across a card poll, and rebuilds them for a new save', () => {
		const { app, model } = setup();
		const first = model.all;
		expect(first.length).toBeGreaterThan(0);
		expect(model.counts.portals).toBe(7);
		app.card = makeCard('a'); // a poll: a new object, the same save
		expect(model.all).toBe(first);
		app.snapshot = { ...fixtureSnapshot(), savedAt: '2026-09-30T10:20:00Z' };
		app.card = makeCard('a', { world: { ...fixtureWorld(), savedAt: '2026-09-30T10:20:00Z' } });
		expect(model.all).not.toBe(first);
	});

	test('no markers while waiting for a save or charting', () => {
		const { app, model } = setup();
		app.card = makeCard('a', { tiles: { state: 'rendering', done: 3, total: 10 } });
		expect(model.markersOn).toBe(false);
		expect(model.all).toEqual([]);
	});

	test('the world clock stops while Farsight can’t be reached', () => {
		const { app, model } = setup();
		app.card = makeCard('a', { clock: { netTime: 9000, at: '2026-09-30T10:00:00Z', running: true, source: 'save' } });
		expect(model.clockRunning).toBe(true);
		app.disconnected = true;
		expect(model.clockRunning).toBe(false);
	});

	test('select() records the id and its kind and position, after the shell’s hook', () => {
		const order: string[] = [];
		const { model, prefs } = setup({ onselect: (id) => order.push(`hook:${id}:${prefs.selectedId}`) });
		model.select('portal-3');
		expect(model.selectedId).toBe('portal-3');
		expect(model.selected?.id).toBe('portal-3');
		expect(prefs.selectedKey).toBe(keyOf(model.selected!));
		model.select(undefined);
		expect(model.selected).toBeUndefined();
		expect(prefs.selectedKey).toBeUndefined();
		expect(order).toEqual(['hook:portal-3:undefined', 'hook:undefined:portal-3']);
	});

	test('a new snapshot keeps the selection only while its id still names the same kind at the same place', () => {
		const { app, model } = setup();
		model.select('portal-3');
		app.snapshot = { ...fixtureSnapshot(), savedAt: '2026-09-30T10:20:00Z' };
		app.card = makeCard('a', { world: { ...fixtureWorld(), savedAt: '2026-09-30T10:20:00Z' } });
		model.checkSelection();
		expect(model.selectedId).toBe('portal-3');

		const moved = fixtureSnapshot();
		moved.savedAt = '2026-09-30T10:40:00Z';
		moved.markers = moved.markers.map((m) => (m.id === 'portal-3' ? { ...m, x: m.x + 400 } : m));
		app.snapshot = moved;
		app.card = makeCard('a', { world: { ...fixtureWorld(), savedAt: '2026-09-30T10:40:00Z' } });
		model.checkSelection();
		expect(model.selectedId).toBeUndefined();
	});

	test('a server switch drops the selection and runs the shell’s hook; the first server and a repeat don’t', () => {
		const onswitch = vi.fn();
		const { app, model } = setup({ onswitch });
		model.checkServer();
		model.select('portal-3');
		model.checkServer();
		expect(onswitch).not.toHaveBeenCalled();
		app.currentId = 'b';
		model.checkServer();
		expect(onswitch).toHaveBeenCalledTimes(1);
		expect(model.selectedId).toBeUndefined();
	});

	test('the selection’s pin is shown only with its layer on', () => {
		const { model } = setup();
		model.zoom = 3;
		model.select('portal-3');
		expect(model.selectedShown).toBe(true);
		model.toggleLayer('portals');
		expect(model.layers.portals).toBe(false);
		expect(model.selectedShown).toBe(false);
	});

	test('layer toggles, portal links and the selection survive a shell switch (a new model on the same prefs)', () => {
		const prefs = new ShellPrefs();
		const { app, model } = setup({ prefs });
		model.toggleLayer('beds');
		model.togglePortalLinks();
		model.select('tame-1');
		const next = new ShellModel({ app, prefs });
		expect(next.layers.beds).toBe(!new ShellPrefs().layers.beds);
		expect(next.portalLinks).toBe(false);
		expect(next.selected?.id).toBe('tame-1');
		next.checkSelection();
		expect(next.selectedId).toBe('tame-1');
	});

	test('toast() drops an empty sub-line', () => {
		const { app, model } = setup();
		model.toast('Link copied', '');
		expect(app.toast).toEqual({ title: 'Link copied' });
		model.toast('Code copied', '123 456');
		expect(app.toast).toEqual({ title: 'Code copied', sub: '123 456' });
	});
});
