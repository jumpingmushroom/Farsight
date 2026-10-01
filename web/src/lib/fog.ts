// Fog of war (DESIGN-NOTES §5.5): a client-side mask built from the
// snapshot's explored zones, painted into ONE canvas covering the padded
// viewport (an L.Renderer, like L.Canvas) in the `fog` pane above the tiles.
// A single canvas has no tile edges, so no seams at fractional zoom. The
// parchment texture is a small pattern anchored to world pixels in screen
// space (the noise and hatching neither scale with zoom nor swim when
// panning), clipped to the world disc, with the explored zones punched out
// through a blurred, smoothed 1-px-per-zone mask canvas.

import L from 'leaflet';
import { CRS, WORLD_RADIUS, metresPerPixel, toLatLng, zoneOf } from './geo';

/** Zone grid: col = zx + ZOFF, row = ZOFF − zz (world +z is up). */
export const ZN = 330;
export const ZOFF = 165;
const ZONE = 64;

export const FOG_COLOR = '#cfbe9c';
const HATCH = 'rgba(120,98,66,.12)';
const HATCH_EVERY = 9;
const NOISE = 18; // ±9 on each RGB channel
const MAX_BLUR = 24;
/** Canvas padding around the view, per side, as a fraction of its size (like L.Renderer). */
export const FOG_PADDING = 0.25;
/** Side of the texture pattern tile: a multiple of the 9-px hatch, so it repeats seamlessly. */
export const PATTERN_SIZE = 126;
const NOISE_SEED = 0x5eed_f06;
/** Punch-out block side in device px (see punchPlan). */
const PUNCH_BLOCK = 64;

/** ZN×ZN bytes, 1 = explored. Zones outside the grid are ignored. */
export function zoneMask(zones: [number, number][]): Uint8Array {
	const m = new Uint8Array(ZN * ZN);
	for (const [zx, zz] of zones) {
		const col = zx + ZOFF;
		const row = ZOFF - zz;
		if (col >= 0 && col < ZN && row >= 0 && row < ZN) m[row * ZN + col] = 1;
	}
	return m;
}

/** Whether the zone containing world point (x, z) is explored in `mask`. */
export function isExplored(mask: Uint8Array, x: number, z: number): boolean {
	const [zx, zz] = zoneOf(x, z);
	const col = zx + ZOFF;
	const row = ZOFF - zz;
	return col >= 0 && col < ZN && row >= 0 && row < ZN && mask[row * ZN + col] === 1;
}

const masks = new WeakMap<object, Uint8Array>();

/** zoneMask, cached per zones array, so each snapshot builds its mask once. */
export function maskForZones(zones: [number, number][]): Uint8Array {
	let m = masks.get(zones);
	if (!m) {
		m = zoneMask(zones);
		masks.set(zones, m);
	}
	return m;
}

/** The world rect the mask covers: x0 is the west edge, z0 the north (top) edge. */
export function maskWorldRect(): { x0: number; z0: number; size: number } {
	return { x0: -ZOFF * ZONE - ZONE / 2, z0: ZOFF * ZONE + ZONE / 2, size: ZN * ZONE };
}

/** Small deterministic PRNG returning floats in [0, 1). */
export function mulberry32(seed: number): () => number {
	let a = seed >>> 0;
	return () => {
		a = (a + 0x6d2b79f5) >>> 0;
		let t = a;
		t = Math.imul(t ^ (t >>> 15), t | 1);
		t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
		return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
	};
}

type Pt = { x: number; y: number };
const mod = (a: number, n: number) => ((a % n) + n) % n;

export type MaskRect = { x: number; y: number; size: number; cell: number };

/**
 * Where the ZN×ZN mask lands at `zoom`: its NW corner (x, y), side and cell
 * size in pixels, relative to the world pixel `origin` (a projected point at
 * the same zoom, e.g. the canvas's top-left).
 */
export function maskDrawRect(origin: Pt, zoom: number): MaskRect {
	const r = maskWorldRect();
	const nw = CRS.latLngToPoint(toLatLng(r.x0, r.z0), zoom);
	const size = r.size / metresPerPixel(zoom);
	return { x: nw.x - origin.x, y: nw.y - origin.y, size, cell: size / ZN };
}

/**
 * The mask cells to draw for a w×h view: those overlapping the view grown
 * by `margin` px (the blur's reach), plus one spare cell each side, clamped
 * to the grid. Null when none do.
 */
export function maskCells(
	rect: MaskRect,
	w: number,
	h: number,
	margin: number
): { c0: number; r0: number; c1: number; r1: number } | null {
	const c0 = Math.max(0, Math.floor((-margin - rect.x) / rect.cell) - 1);
	const r0 = Math.max(0, Math.floor((-margin - rect.y) / rect.cell) - 1);
	const c1 = Math.min(ZN, Math.ceil((w + margin - rect.x) / rect.cell) + 1);
	const r1 = Math.min(ZN, Math.ceil((h + margin - rect.y) / rect.cell) + 1);
	return c1 > c0 && r1 > r0 ? { c0, r0, c1, r1 } : null;
}

/**
 * The pattern translation for a canvas whose top-left is world pixel
 * `origin`: the pattern's own (0,0) sits on world pixel (0,0), so the
 * texture stays put while the canvas moves under it. `period` is the tile
 * side in the same units as `origin` (device px: PATTERN_SIZE × DPR).
 */
export function patternOffset(origin: Pt, period = PATTERN_SIZE): Pt {
	return { x: mod(-origin.x, period) + 0, y: mod(-origin.y, period) + 0 };
}

/** The world disc (radius 10 500 m) at `zoom`, relative to world pixel `origin`. */
export function worldDisc(origin: Pt, zoom: number): { x: number; y: number; r: number } {
	const c = CRS.latLngToPoint(toLatLng(0, 0), zoom);
	return { x: c.x - origin.x, y: c.y - origin.y, r: WORLD_RADIUS / metresPerPixel(zoom) };
}

export type Box = { x: number; y: number; w: number; h: number };

/**
 * The fog skirt for a zoom transform (all in one pixel space): what of the
 * `view` the transformed `canvas` no longer covers, as the canvas's `hole`
 * and the world `disc` relative to the view's top-left. Null when the canvas
 * still covers the view, or when the bare part is all off-world (the disc
 * lies inside the hole or misses the view).
 */
export function fogSkirt(view: Box, canvas: Box, disc: { x: number; y: number; r: number }): { hole: Box; disc: { x: number; y: number; r: number } } | null {
	const covers =
		canvas.x <= view.x && canvas.y <= view.y && canvas.x + canvas.w >= view.x + view.w && canvas.y + canvas.h >= view.y + view.h;
	if (covers) return null;
	const hole = { x: canvas.x - view.x, y: canvas.y - view.y, w: canvas.w, h: canvas.h };
	const d = { x: disc.x - view.x, y: disc.y - view.y, r: disc.r };
	const inHole = d.x - d.r >= hole.x && d.y - d.r >= hole.y && d.x + d.r <= hole.x + hole.w && d.y + d.r <= hole.y + hole.h;
	const missesView = d.x + d.r <= 0 || d.y + d.r <= 0 || d.x - d.r >= view.w || d.y - d.r >= view.h;
	return inHole || missesView ? null : { hole, disc: d };
}

/** Explored-cell counts over any cell range of a mask (a summed-area table). */
export type MaskSums = { count(c0: number, r0: number, c1: number, r1: number): number };
const sumsCache = new WeakMap<Uint8Array, MaskSums>();

/** The mask's summed-area table, built once per mask. Cells off the grid count as unexplored. */
export function maskSums(mask: Uint8Array): MaskSums {
	let s = sumsCache.get(mask);
	if (s) return s;
	const N = ZN + 1;
	const t = new Int32Array(N * N);
	for (let r = 0; r < ZN; r++) {
		let row = 0;
		for (let c = 0; c < ZN; c++) {
			row += mask[r * ZN + c];
			t[(r + 1) * N + c + 1] = t[r * N + c + 1] + row;
		}
	}
	const cl = (v: number) => Math.max(0, Math.min(ZN, v));
	s = {
		count(c0, r0, c1, r1) {
			c0 = cl(c0);
			r0 = cl(r0);
			c1 = cl(c1);
			r1 = cl(r1);
			if (c1 <= c0 || r1 <= r0) return 0;
			return t[r1 * N + c1] - t[r0 * N + c1] - t[r1 * N + c0] + t[r0 * N + c0];
		}
	};
	sumsCache.set(mask, s);
	return s;
}

type Rect4 = [number, number, number, number];

/**
 * Splits the punch-out over a w×h canvas (all in device px) into square
 * blocks: blocks whose every cell within `margin` is explored are simply
 * cleared (the blurred mask is ~1 there), blocks with no explored cell
 * within `margin` stay fog, and the rest — the soft band — get the blurred
 * mask. Blocks sit on whole pixels, and runs along a row are merged.
 */
export function punchPlan(
	sums: MaskSums,
	rect: { x: number; y: number; cell: number },
	w: number,
	h: number,
	block: number,
	margin: number
): { clear: Rect4[]; band: Rect4[]; bandBounds: { x0: number; y0: number; x1: number; y1: number } | null } {
	const clear: Rect4[] = [];
	const band: Rect4[] = [];
	let bb: { x0: number; y0: number; x1: number; y1: number } | null = null;
	const colOf = (x: number) => (x - rect.x) / rect.cell;
	const rowOf = (y: number) => (y - rect.y) / rect.cell;
	for (let y = 0; y < h; y += block) {
		const bh = Math.min(block, h - y);
		const r0 = Math.floor(rowOf(y - margin));
		const r1 = Math.ceil(rowOf(y + bh + margin));
		let run: { kind: 0 | 1 | 2; x: number } | null = null;
		const flush = (end: number) => {
			if (!run || run.kind === 0) return;
			const r: Rect4 = [run.x, y, end - run.x, bh];
			if (run.kind === 1) clear.push(r);
			else {
				band.push(r);
				bb = bb
					? { x0: Math.min(bb.x0, r[0]), y0: Math.min(bb.y0, y), x1: Math.max(bb.x1, end), y1: Math.max(bb.y1, y + bh) }
					: { x0: r[0], y0: y, x1: end, y1: y + bh };
			}
		};
		for (let x = 0; x < w; x += block) {
			const bw = Math.min(block, w - x);
			const c0 = Math.floor(colOf(x - margin));
			const c1 = Math.ceil(colOf(x + bw + margin));
			const n = sums.count(c0, r0, c1, r1);
			const kind: 0 | 1 | 2 = n === 0 ? 0 : n === (c1 - c0) * (r1 - r0) ? 1 : 2;
			if (!run || run.kind !== kind) {
				flush(x);
				run = { kind, x };
			}
		}
		flush(w);
	}
	return { clear, band, bandBounds: bb };
}

/** Scratch downscale for a blur of `sigma` device px: keeps ≥ 2 px of blur, 1…8. */
export function blurScale(sigma: number): number {
	return Math.max(1, Math.min(8, Math.floor(sigma / 2)));
}

/** The punch-out blur in CSS px: about 0.9 zones, capped. */
export function fogBlur(zoom: number): number {
	return Math.min(MAX_BLUR, (0.9 * ZONE) / metresPerPixel(zoom));
}

// --- JS blur fallback --------------------------------------------------------
// Older Safari has no CanvasRenderingContext2D.filter: the soft band's mask
// is then blurred here, three box passes ≈ a gaussian of the same sigma.

/** Box radii for `passes` box blurs approximating a gaussian of `sigma` (W. Jarosz / I. Kutskir). */
export function boxRadiiForGauss(sigma: number, passes = 3): number[] {
	if (!(sigma > 0)) return new Array(passes).fill(0);
	const wIdeal = Math.sqrt((12 * sigma * sigma) / passes + 1);
	let wl = Math.floor(wIdeal);
	if (wl % 2 === 0) wl--;
	const wu = wl + 2;
	const m = Math.round((12 * sigma * sigma - passes * wl * wl - 4 * passes * wl - 3 * passes) / (-4 * wl - 4));
	return Array.from({ length: passes }, (_, i) => ((i < m ? wl : wu) - 1) / 2);
}

/** In-place box blur of radius `r` over a w×h single-channel image; zero outside it. */
export function boxBlur(a: Float32Array, w: number, h: number, r: number, tmp = new Float32Array(a.length)): void {
	if (r <= 0) return;
	const k = 1 / (2 * r + 1);
	// Horizontal: a → tmp.
	for (let y = 0; y < h; y++) {
		const row = y * w;
		let sum = 0;
		for (let x = 0; x < Math.min(r, w); x++) sum += a[row + x];
		for (let x = 0; x < w; x++) {
			if (x + r < w) sum += a[row + x + r];
			if (x - r - 1 >= 0) sum -= a[row + x - r - 1];
			tmp[row + x] = sum * k;
		}
	}
	// Vertical: tmp → a.
	for (let x = 0; x < w; x++) {
		let sum = 0;
		for (let y = 0; y < Math.min(r, h); y++) sum += tmp[y * w + x];
		for (let y = 0; y < h; y++) {
			if (y + r < h) sum += tmp[(y + r) * w + x];
			if (y - r - 1 >= 0) sum -= tmp[(y - r - 1) * w + x];
			a[y * w + x] = sum * k;
		}
	}
}

/** In-place gaussian-like blur (`passes` box blurs) of a w×h single-channel image. */
export function gaussBlur(a: Float32Array, w: number, h: number, sigma: number, passes = 3): void {
	const tmp = new Float32Array(a.length);
	for (const r of boxRadiiForGauss(sigma, passes)) boxBlur(a, w, h, r, tmp);
}

let filterOk: boolean | undefined;

/** Whether canvas 2D `filter` works here (feature-detected once; old iOS Safari lacks it). */
export function canvasFilterSupported(): boolean {
	if (filterOk !== undefined) return filterOk;
	filterOk = false;
	try {
		const C = (globalThis as { CanvasRenderingContext2D?: { prototype: object } }).CanvasRenderingContext2D;
		if (C && 'filter' in C.prototype) {
			const ctx = document.createElement('canvas').getContext('2d');
			if (ctx) {
				ctx.filter = 'blur(2px)';
				filterOk = ctx.filter === 'blur(2px)';
			}
		}
	} catch {
		filterOk = false;
	}
	return filterOk;
}

/** Blurs the scratch's w×h top-left region by `sigma` px in JS (alpha only; the punch-out uses alpha). */
function blurScratchInJs(pctx: CanvasRenderingContext2D, w: number, h: number, sigma: number): void {
	const img = pctx.getImageData(0, 0, w, h);
	const d = img.data;
	const a = new Float32Array(w * h);
	for (let i = 0; i < a.length; i++) a[i] = d[i * 4 + 3];
	gaussBlur(a, w, h, sigma);
	for (let i = 0; i < a.length; i++) {
		const o = i * 4;
		d[o] = d[o + 1] = d[o + 2] = 255;
		d[o + 3] = a[i];
	}
	pctx.putImageData(img, 0, 0);
}

// One mask canvas per mask array (i.e. per snapshot).
const maskCanvases = new WeakMap<Uint8Array, HTMLCanvasElement>();

function maskCanvas(mask: Uint8Array): HTMLCanvasElement {
	let c = maskCanvases.get(mask);
	if (c) return c;
	c = document.createElement('canvas');
	c.width = ZN;
	c.height = ZN;
	const ctx = c.getContext('2d');
	if (ctx) {
		const img = ctx.createImageData(ZN, ZN);
		for (let i = 0; i < mask.length; i++) {
			if (!mask[i]) continue;
			const o = i * 4;
			img.data[o] = img.data[o + 1] = img.data[o + 2] = img.data[o + 3] = 255;
		}
		ctx.putImageData(img, 0, 0);
	}
	maskCanvases.set(mask, c);
	return c;
}

const tiles = new Map<number, HTMLCanvasElement>();

/**
 * The parchment texture tile (§5.5) at `side` = PATTERN_SIZE × DPR device
 * px, built once per side: #cfbe9c with ±9 grey noise per CSS pixel from a
 * fixed seed (the same grain on every load, at any DPR) and 45° hatching
 * every 9 CSS px, 1 CSS px wide. Lines run where x − y ≡ 0 (mod 9); since
 * 126 is a multiple of 9 and every line crossing the tile is drawn, the
 * tile repeats without a seam. Built at device resolution so the fill needs
 * no scaling (a scaled pattern fill is several times slower).
 */
function texture(side: number): HTMLCanvasElement | undefined {
	let c = tiles.get(side);
	if (c) return c;
	const base = document.createElement('canvas');
	base.width = base.height = PATTERN_SIZE;
	const bctx = base.getContext('2d');
	c = document.createElement('canvas');
	c.width = c.height = side;
	const ctx = c.getContext('2d');
	if (!bctx || !ctx) return undefined;
	bctx.fillStyle = FOG_COLOR;
	bctx.fillRect(0, 0, PATTERN_SIZE, PATTERN_SIZE);
	const img = bctx.getImageData(0, 0, PATTERN_SIZE, PATTERN_SIZE);
	const d = img.data;
	const rand = mulberry32(NOISE_SEED);
	for (let o = 0; o < d.length; o += 4) {
		const n = (rand() - 0.5) * NOISE;
		d[o] += n;
		d[o + 1] += n;
		d[o + 2] += n;
	}
	bctx.putImageData(img, 0, 0);
	ctx.imageSmoothingEnabled = false;
	ctx.drawImage(base, 0, 0, side, side);
	ctx.scale(side / PATTERN_SIZE, side / PATTERN_SIZE);
	ctx.strokeStyle = HATCH;
	ctx.lineWidth = 1;
	ctx.beginPath();
	for (let x = -PATTERN_SIZE - HATCH_EVERY; x <= PATTERN_SIZE + HATCH_EVERY; x += HATCH_EVERY) {
		ctx.moveTo(x - HATCH_EVERY, -HATCH_EVERY);
		ctx.lineTo(x + PATTERN_SIZE + HATCH_EVERY, PATTERN_SIZE + HATCH_EVERY);
	}
	ctx.stroke();
	tiles.set(side, c);
	return c;
}

let textureUrl: string | undefined;

/** The texture tile at 1× as a data URL, for the skirt's CSS background (undefined without canvas). */
function textureDataUrl(): string | undefined {
	if (textureUrl === undefined) {
		try {
			textureUrl = texture(PATTERN_SIZE)?.toDataURL() ?? '';
		} catch {
			textureUrl = '';
		}
	}
	return textureUrl || undefined;
}

let scratch: HTMLCanvasElement | undefined;

/**
 * The shared blur canvas, at least w×h. It holds the soft band's bounding
 * box at 1/blurScale resolution plus the blur pad, so it stays well below
 * the fog canvas's size; only the used part is cleared.
 *
 * Exported for `releaseScratch`'s test (fog.test.ts): the two together are
 * otherwise only reachable through `_destroyContainer`/`_draw`, which need a
 * working canvas 2D context that the Vitest (Node) environment doesn't have.
 */
export function scratchCanvas(w: number, h: number): HTMLCanvasElement {
	if (!scratch) scratch = document.createElement('canvas');
	if (scratch.width < w) scratch.width = w;
	if (scratch.height < h) scratch.height = h;
	return scratch;
}

/** Frees the scratch canvas's backing store (the fog layer was removed). */
export function releaseScratch(): void {
	if (!scratch) return;
	scratch.width = scratch.height = 0;
	scratch = undefined;
}

export type FogLayer = L.Layer & { redraw(): FogLayer };

type RendererThis = L.Layer & {
	_map: L.Map & { _animatingZoom?: boolean };
	_frame?: number;
	_dprOff?: () => void;
	_schedule(): void;
	_watchDpr(): void;
	_container?: HTMLCanvasElement;
	_skirt?: HTMLDivElement;
	_center: L.LatLng;
	getPane(): HTMLElement | undefined;
	_hideSkirt(): void;
	_ctx?: CanvasRenderingContext2D | null;
	_pattern?: CanvasPattern | null;
	_patternSide?: number;
	_bounds: L.Bounds;
	_zoom: number;
	_update(): void;
	_draw(): void;
	options: L.LayerOptions & { padding: number };
};

type RendererProto = {
	_update(this: RendererThis): void;
	getEvents(this: RendererThis): L.LeafletEventHandlerFnMap;
	onAdd(this: RendererThis, map: L.Map): void;
	_updateTransform(this: RendererThis, center: L.LatLng, zoom: number): void;
};
type MapInternals = L.Map & {
	_animatingZoom?: boolean;
	_getMapPanePos(): L.Point;
	_getNewPixelOrigin(center: L.LatLng, zoom: number): L.Point;
};
const Renderer = L.Renderer as unknown as { prototype: RendererProto; extend(props: object): new (o?: object) => FogLayer };

const raf = (cb: () => void): number =>
	typeof requestAnimationFrame === 'function' ? requestAnimationFrame(cb) : (setTimeout(cb, 16) as unknown as number);
const caf = (id: number): void =>
	typeof cancelAnimationFrame === 'function' ? cancelAnimationFrame(id) : clearTimeout(id);

/**
 * The fog layer: one canvas in the `fog` pane covering the view plus
 * FOG_PADDING. Repaints on moveend (which follows every zoomend), resize,
 * viewreset, a device-pixel-ratio change and `redraw()` are coalesced into
 * one animation frame (setView fires viewreset and moveend; a window resize
 * fires resize many times, then moveend); the frame runs before the next
 * paint, so nothing stale is shown. During a zoom animation L.Renderer
 * scales the existing canvas (`_onAnimZoom`/`_updateTransform`); it is
 * repainted once the zoom ends. A zoom-out shrinks that canvas below the
 * view (a quick wheel zoom-out of 2.5 levels leaves it at 18%), so the
 * `fs-fog-skirt` covers the bare part — the view minus the shrunken canvas,
 * within the world disc at the target zoom — until that repaint. `getMask` returns the current explored-zone
 * mask (undefined draws nothing); call `redraw()` when it changes.
 */
export function createFogLayer(getMask: () => Uint8Array | undefined): FogLayer {
	const Fog = Renderer.extend({
		options: { pane: 'fog', padding: FOG_PADDING },

		getEvents(this: RendererThis) {
			const events = Renderer.prototype.getEvents.call(this);
			events.moveend = this._schedule;
			events.resize = this._schedule;
			events.viewreset = this._schedule;
			events.move = onMove;
			return events;
		},

		/** Asks for one repaint on the next animation frame (dirty flag). */
		_schedule(this: RendererThis) {
			if (this._frame) return;
			this._frame = raf(() => {
				this._frame = 0;
				if (this._map && this._container) this._update();
			});
		},

		/**
		 * Repaints when the device pixel ratio changes (browser zoom, a move
		 * to another screen): a `(resolution: Xdppx)` query fires once when
		 * it stops matching, so it is re-armed for the new ratio each time.
		 */
		_watchDpr(this: RendererThis) {
			this._dprOff?.();
			this._dprOff = undefined;
			if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
			const mql = window.matchMedia(`(resolution: ${window.devicePixelRatio || 1}dppx)`);
			const onChange = () => {
				this._watchDpr();
				this._schedule();
			};
			if (typeof mql.addEventListener === 'function') {
				mql.addEventListener('change', onChange);
				this._dprOff = () => mql.removeEventListener('change', onChange);
			} else {
				// Safari < 14 (the same old iOS the blur fallback is for).
				mql.addListener(onChange);
				this._dprOff = () => mql.removeListener(onChange);
			}
		},

		_initContainer(this: RendererThis) {
			const c = document.createElement('canvas');
			c.className = 'fs-fog';
			this._container = c;
			this._ctx = c.getContext('2d');
			this._pattern = undefined;
			this._patternSide = undefined;
			// The skirt: an outer div clipped to the world disc, holding a
			// fog-filled div with the shrunken canvas cut out (two nested
			// clips, as one CSS clip can't intersect a circle and a frame).
			const skirt = document.createElement('div');
			skirt.className = 'fs-fog-skirt';
			skirt.hidden = true;
			skirt.style.position = 'absolute';
			skirt.style.pointerEvents = 'none';
			const fill = document.createElement('div');
			fill.style.position = 'absolute';
			fill.style.inset = '0';
			fill.style.backgroundColor = FOG_COLOR;
			const url = textureDataUrl();
			if (url) {
				fill.style.backgroundImage = `url(${url})`;
				fill.style.backgroundSize = `${PATTERN_SIZE}px ${PATTERN_SIZE}px`;
			}
			skirt.appendChild(fill);
			this._skirt = skirt;
			this._watchDpr();
		},

		onAdd(this: RendererThis, map: L.Map) {
			Renderer.prototype.onAdd.call(this, map);
			if (this._skirt) this.getPane()?.appendChild(this._skirt);
		},

		_hideSkirt(this: RendererThis) {
			if (this._skirt) this._skirt.hidden = true;
		},

		/**
		 * L.Renderer's zoom transform (zoom animation, pinch), plus the
		 * skirt over whatever the transformed canvas leaves bare. All in
		 * map-pane px, which hold still during a zoom animation.
		 */
		_updateTransform(this: RendererThis, center: L.LatLng, zoom: number) {
			Renderer.prototype._updateTransform.call(this, center, zoom);
			const skirt = this._skirt;
			if (!skirt || !this._bounds || this._center === undefined) return;
			const map = this._map as MapInternals;
			const scale = map.getZoomScale(zoom, this._zoom);
			const size = map.getSize();
			const origin = map._getNewPixelOrigin(center, zoom);
			// As L.Renderer places the canvas (its size is the padded view).
			const padded = size.multiplyBy(1 + 2 * this.options.padding).multiplyBy(scale);
			const tl = map.project(this._center, zoom).subtract(padded.divideBy(2)).subtract(origin);
			const pane = map._getMapPanePos();
			const view = { x: -pane.x, y: -pane.y, w: size.x, h: size.y };
			const c = map.project(toLatLng(0, 0), zoom).subtract(origin);
			const g = fogSkirt(view, { x: tl.x, y: tl.y, w: padded.x, h: padded.y }, { x: c.x, y: c.y, r: WORLD_RADIUS / metresPerPixel(zoom) });
			if (!g) {
				skirt.hidden = true;
				return;
			}
			const s = skirt.style;
			s.left = view.x + 'px';
			s.top = view.y + 'px';
			s.width = view.w + 'px';
			s.height = view.h + 'px';
			s.clipPath = `circle(${g.disc.r}px at ${g.disc.x}px ${g.disc.y}px)`;
			const { x, y, w, h } = g.hole;
			const fill = (skirt as unknown as { children: ArrayLike<HTMLElement> }).children[0];
			fill.style.clipPath =
				`polygon(evenodd, 0 0, 100% 0, 100% 100%, 0 100%, 0 0, ` +
				`${x}px ${y}px, ${x + w}px ${y}px, ${x + w}px ${y + h}px, ${x}px ${y + h}px, ${x}px ${y}px)`;
			// The texture anchored to world pixels, as on the canvas.
			const off = patternOffset({ x: view.x + origin.x, y: view.y + origin.y });
			fill.style.backgroundPosition = `${off.x}px ${off.y}px`;
			skirt.hidden = false;
		},

		_destroyContainer(this: RendererThis) {
			if (this._frame) caf(this._frame);
			this._frame = 0;
			this._dprOff?.();
			this._dprOff = undefined;
			if (this._container) L.DomUtil.remove(this._container);
			if (this._skirt) L.DomUtil.remove(this._skirt);
			delete this._skirt;
			delete this._ctx;
			delete this._pattern;
			delete this._patternSide;
			delete this._container;
			releaseScratch();
		},

		// L.Renderer's _reset re-applies _updateTransform after _update,
		// which leaves the canvas at a sub-pixel offset (it keeps the
		// unrounded centre): the texture would be resampled and the fog
		// shifted up to ½ px against the tiles. _update already places the
		// canvas on whole pixels, so that is all a view reset needs.
		// (viewreset itself is routed to _schedule in getEvents.)
		_reset(this: RendererThis) {
			this._update();
		},

		_update(this: RendererThis) {
			// Mid-animation the map still reports the old zoom; zoomend →
			// moveend repaints (as L.Canvas does).
			if (this._map._animatingZoom && this._bounds) return;
			Renderer.prototype._update.call(this);
			const c = this._container;
			if (!c) return;
			const size = this._bounds.getSize();
			const dpr = window.devicePixelRatio || 1;
			L.DomUtil.setPosition(c, this._bounds.min!);
			const w = Math.round(size.x * dpr);
			const h = Math.round(size.y * dpr);
			if (c.width !== w) c.width = w;
			if (c.height !== h) c.height = h;
			c.style.width = size.x + 'px';
			c.style.height = size.y + 'px';
			this._draw();
			// The canvas now covers the padded view again.
			this._hideSkirt();
		},

		_draw(this: RendererThis) {
			const ctx = this._ctx;
			const c = this._container;
			if (!ctx || !c) return;
			// Everything here is in device px, with an identity transform.
			ctx.setTransform(1, 0, 0, 1, 0, 0);
			ctx.globalCompositeOperation = 'source-over';
			const W = c.width;
			const H = c.height;
			const dpr = W / this._bounds.getSize().x || 1;
			const side = Math.round(PATTERN_SIZE * dpr);
			const mask = getMask();
			const tex = mask && texture(side);
			if (!mask || !tex) {
				ctx.clearRect(0, 0, W, H);
				return;
			}
			const zoom = this._zoom;
			// World pixel of the canvas's top-left (CSS px at this zoom).
			const origin = this._bounds.min!.add(this._map.getPixelOrigin());
			const disc = worldDisc(origin, zoom);
			// The opaque texture repaints every pixel inside the disc, so the
			// canvas needs clearing only when some of it lies outside.
			const r2 = (disc.r * dpr - 2) ** 2;
			const inside = (x: number, y: number) => (x - disc.x * dpr) ** 2 + (y - disc.y * dpr) ** 2 < r2;
			if (!(inside(0, 0) && inside(W, 0) && inside(0, H) && inside(W, H))) ctx.clearRect(0, 0, W, H);

			// 1. Texture, clipped to the world disc, anchored to world pixels.
			if (this._patternSide !== side) {
				this._pattern = ctx.createPattern(tex, 'repeat');
				this._patternSide = side;
			}
			const pattern = this._pattern;
			ctx.save();
			ctx.beginPath();
			ctx.arc(disc.x * dpr, disc.y * dpr, disc.r * dpr, 0, Math.PI * 2);
			ctx.clip();
			if (pattern) {
				const off = patternOffset({ x: Math.round(origin.x * dpr), y: Math.round(origin.y * dpr) }, side);
				pattern.setTransform(new DOMMatrix().translate(off.x, off.y));
				ctx.fillStyle = pattern;
			} else {
				ctx.fillStyle = FOG_COLOR;
			}
			ctx.fillRect(0, 0, W, H);
			ctx.restore();

			// 2. Punch out the explored zones. Where every zone within the
			// blur's reach is explored the fog is just cleared; only the soft
			// band gets the blurred mask, composited in ONE drawImage under a
			// clip of whole-pixel blocks (so no seams), from a scratch canvas
			// at 1/k resolution (the blur is smooth, so upscaling it is
			// lossless to the eye and far cheaper than blurring at full size).
			const sigma = fogBlur(zoom) * dpr;
			const reach = 3 * sigma;
			const m = maskDrawRect(origin, zoom);
			const rect = { x: m.x * dpr, y: m.y * dpr, size: m.size * dpr, cell: m.cell * dpr };
			const plan = punchPlan(maskSums(mask), rect, W, H, PUNCH_BLOCK, reach);
			for (const [x, y, w, h] of plan.clear) ctx.clearRect(x, y, w, h);
			const bb = plan.bandBounds;
			if (!bb) return;
			// The scratch grid (k device px per scratch px) is anchored to
			// world device pixels, like the texture, so a repaint after a pan
			// samples the band exactly as before.
			const k = blurScale(sigma);
			const ox = Math.round(origin.x * dpr);
			const oy = Math.round(origin.y * dpr);
			const lx0 = Math.floor((ox + bb.x0) / k);
			const ly0 = Math.floor((oy + bb.y0) / k);
			const lw = Math.ceil((ox + bb.x1) / k) - lx0;
			const lh = Math.ceil((oy + bb.y1) / k) - ly0;
			const pad = Math.ceil(reach / k) + 1;
			// The mask rect relative to the scratch's (unpadded) band box, in scratch px.
			const lrect = { x: (ox + rect.x) / k - lx0, y: (oy + rect.y) / k - ly0, size: rect.size / k, cell: rect.cell / k };
			const cells = maskCells(lrect, lw, lh, pad);
			if (!cells) return;
			const { c0, r0, c1, r1 } = cells;
			const punch = scratchCanvas(lw + 2 * pad, lh + 2 * pad);
			const pctx = punch.getContext('2d');
			if (!pctx) return;
			pctx.setTransform(1, 0, 0, 1, 0, 0);
			pctx.clearRect(0, 0, lw + 2 * pad, lh + 2 * pad);
			const useFilter = canvasFilterSupported();
			if (useFilter) pctx.filter = `blur(${sigma / k}px)`;
			pctx.imageSmoothingEnabled = true;
			pctx.drawImage(
				maskCanvas(mask),
				c0,
				r0,
				c1 - c0,
				r1 - r0,
				pad + lrect.x + c0 * lrect.cell,
				pad + lrect.y + r0 * lrect.cell,
				(c1 - c0) * lrect.cell,
				(r1 - r0) * lrect.cell
			);
			if (useFilter) pctx.filter = 'none';
			else blurScratchInJs(pctx, lw + 2 * pad, lh + 2 * pad, sigma / k);
			ctx.save();
			ctx.beginPath();
			for (const [x, y, w, h] of plan.band) ctx.rect(x, y, w, h);
			ctx.clip();
			ctx.globalCompositeOperation = 'destination-out';
			ctx.imageSmoothingEnabled = true;
			ctx.drawImage(punch, pad, pad, lw, lh, lx0 * k - ox, ly0 * k - oy, lw * k, lh * k);
			ctx.restore();
		},

		redraw(this: RendererThis) {
			if (this._map) this._schedule();
			return this;
		}
	});

	/**
	 * While dragging, repaint once the view leaves the padded canvas (a pan
	 * longer than the padding would otherwise show unfogged terrain until
	 * moveend). Synchronous, not coalesced: drag moves already run in an
	 * animation frame, and a deferred repaint would show one frame of the
	 * uncovered edge. Not during zoom (pinch or animation): the transform
	 * covers it.
	 */
	function onMove(this: RendererThis): void {
		const map = this._map;
		if (!this._bounds || map._animatingZoom || map.getZoom() !== this._zoom) return;
		const tl = map.containerPointToLayerPoint([0, 0]);
		const br = tl.add(map.getSize());
		if (!this._bounds.contains(tl) || !this._bounds.contains(br)) {
			if (this._frame) caf(this._frame);
			this._frame = 0;
			this._update();
		}
	}

	return new Fog();
}
