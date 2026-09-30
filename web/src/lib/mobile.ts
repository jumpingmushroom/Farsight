// Mobile layout logic (DESIGN-NOTES §1.2, §3.17, §3.18, §5.11 and the Mobile
// ruling): the bottom-sheet drag/snap decisions, the map dimming, the zoom
// column and marker-centring offsets, and the top-bar sub-line.

import { mapPill, type BannerTone, type MapView } from './derive';
import type { WorldCard } from './types';

export type Snap = 'peek' | 'pulled';
export type MobileOverlay = 'none' | 'menu' | 'join' | 'server';

/** Movement (px) under which a pointer-down/up on the handle is a tap. */
export const TAP_SLOP = 6;
/** Release speed (px/ms) from which a drag counts as a flick in its direction. */
export const FLICK_V = 0.5;
/** A full or auto sheet dismisses when dragged down this far (or a quarter of its height, if less). */
export const DISMISS_PX = 120;

/** The sheet height while dragging: the start height minus the pointer's dy, clamped to [min, max]. */
export function dragHeight(startH: number, dy: number, minH: number, maxH: number): number {
	return Math.min(maxH, Math.max(minH, startH - dy));
}

/**
 * Peek or pulled on release. A flick (|vy| ≥ FLICK_V; positive = down) goes
 * in its direction; otherwise the nearest snap by height, the midpoint
 * going to pulled.
 */
export function releaseSnap(h: number, peekH: number, pulledH: number, vy: number): Snap {
	if (vy >= FLICK_V) return 'peek';
	if (vy <= -FLICK_V) return 'pulled';
	return h - peekH >= pulledH - h ? 'pulled' : 'peek';
}

/** Whether a full or auto sheet dragged down by `dy` (px) closes on release. */
export function releaseDismiss(dy: number, height: number, vy: number): boolean {
	if (dy <= 0 || vy <= -FLICK_V) return false;
	if (vy >= FLICK_V) return true;
	return dy >= Math.min(DISMISS_PX, height / 4);
}

/** Vertical speed (px/ms) over the samples within `windowMs` of the last one; 0 when unknown. */
export function velocity(samples: { t: number; y: number }[], windowMs = 100): number {
	if (samples.length < 2) return 0;
	const last = samples[samples.length - 1];
	let first = last;
	for (let i = samples.length - 2; i >= 0; i--) {
		if (last.t - samples[i].t > windowMs) break;
		first = samples[i];
	}
	const dt = last.t - first.t;
	return dt > 0 ? (last.y - first.y) / dt : 0;
}

export function isTap(dy: number): boolean {
	return Math.abs(dy) < TAP_SLOP;
}

/** Map brightness behind the sheets: .7 pulled, .55 behind the menu, join and server sheets. */
export function mobileDim(snap: Snap, overlay: MobileOverlay): number {
	if (overlay !== 'none') return 0.55;
	return snap === 'pulled' ? 0.7 : 1;
}

/** The zoom column's `bottom` (px): 47 px above the peek sheet (§1.2: 208 over a 161 px sheet). */
export function zoomBottom(peekH: number): number {
	return peekH + 47;
}

/** The vertical offset from the map centre that puts a selected marker at `y` (§1.2 frame 4: y ≈ 300). */
export function centerDy(containerH: number, y = 300): number {
	return y - containerH / 2;
}

/**
 * The bottom strip (px) the map treats as covered while a marker card is
 * docked, so the maxBounds limit lets a marker be centred at `y`: the
 * visible area becomes [0, 2y].
 */
export function cardPadBottom(containerH: number, y = 300): number {
	return Math.max(0, containerH - 2 * y);
}

/** The short state titles (StateBanner's announcements), reused by the mobile top bar. */
export const BANNER_SHORT: Record<BannerTone, string> = {
	offline: 'Server offline',
	stale: 'Map data may be out of date',
	refused: 'Can’t draw this world’s map yet'
};

export interface TopBarSub {
	text: string;
	/** The 7 px dot: cold (map fine / charting), offline (neutral), warn (accent), or none. */
	tone: 'cold' | 'offline' | 'warn' | 'none';
}

/**
 * The top-bar sub-line (§3.17, ruling 5): "Choose a server" with the
 * switcher open; else "Map {N} min ago · next ~{M} min" when the map is
 * ready, or a compact state text.
 */
export function topBarSub(view: MapView | undefined, world: WorldCard | undefined, now: Date, switcherOpen: boolean): TopBarSub {
	if (switcherOpen) return { text: 'Choose a server', tone: 'none' };
	if (!view) return { text: '', tone: 'none' };
	const o = view.overlay;
	switch (o.kind) {
		case 'waiting':
			return { text: 'Waiting for the first world save…', tone: 'none' };
		case 'charting':
			return { text: `Charting the world · ${o.pct}%`, tone: 'cold' };
		case 'banner':
			return { text: BANNER_SHORT[o.tone], tone: o.tone === 'offline' ? 'offline' : 'warn' };
		case 'pill': {
			if (!world) return { text: '', tone: 'none' };
			const p = mapPill(world, now);
			const age = p.age === 'just now' ? 'Map updated just now' : `Map ${p.age}`;
			const next = p.next === undefined ? '' : p.next === 'any moment' ? ' · next save any moment' : ` · next ${p.next}`;
			return { text: age + next, tone: 'cold' };
		}
	}
}
