<!--
  Mobile bottom sheet (DESIGN-NOTES §1.2, §3.18, §5.11 and the Mobile ruling).

  Snaps:
  - `peek`: auto height (≈161 px in the design); `peekHeight` reports it.
  - `pulled`: from `top: env(safe-area-inset-top) + 130px` (design 176 with
    the top bar at 54) to the bottom.
  - `full`: from `top: env(safe-area-inset-top) + 8px` (the join sheet).
  - `auto`: content height, capped below the top bar (menu, server sheets).

  Dragging: a pointer (mouse, touch or pen) pressed on the handle or any
  element marked `data-sheet-drag` (not on its buttons) starts a drag once
  it moves TAP_SLOP px (followed on the window until then); the pointer is
  then captured. Peek/pulled sheets
  follow the pointer between the two heights, report the nearer snap while
  dragging, and settle on release with a 150 ms ease (a flick goes in its
  direction; mobile.ts `releaseSnap`). Full/auto sheets can be dragged down
  and close (`onclose`) past a threshold or on a downward flick, otherwise
  spring back. A tap on the handle toggles peek ↔ pulled. External snap
  changes (Esc, a tap) animate the same way.

  The handle and drag zones set `touch-action: none`; the bottom padding
  respects `env(safe-area-inset-bottom)` via `--sheet-bottom`.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { onMount, tick, untrack } from 'svelte';
	import { dragHeight, isTap, releaseDismiss, releaseSnap, velocity, type Snap } from '$lib/mobile';

	type SheetSnap = Snap | 'full' | 'auto';

	let {
		snap,
		onsnap,
		onclose,
		surface,
		handle = true,
		hidden = false,
		peekHeight = $bindable(0),
		children
	}: {
		snap: SheetSnap;
		onsnap?: (s: Snap) => void;
		/** Full/auto sheets: dragged down far enough. */
		onclose?: () => void;
		surface: 'glass' | 'surface';
		/** Show the grab handle (the menu sheet has none). */
		handle?: boolean;
		/** Slid out of view (the peek sheet while a marker card is docked). */
		hidden?: boolean;
		peekHeight?: number;
		children: Snippet;
	} = $props();

	const SETTLE_MS = 150;

	let el: HTMLDivElement;
	let probe = $state<HTMLDivElement>();
	/** Explicit height while dragging or settling (peek/pulled); undefined = CSS decides. */
	let h = $state<number>();
	/** Downward offset while dragging a full/auto sheet. */
	let ty = $state(0);
	let anim = $state(false);

	const snappy = $derived(snap === 'peek' || snap === 'pulled');

	/** The snap currently drawn; set before calling onsnap so the prop change doesn't re-animate. */
	let shown: SheetSnap = untrack(() => snap);
	let timer: ReturnType<typeof setTimeout> | undefined;

	/**
	 * The peek height is read from the element only while it rests at peek
	 * (auto height, no drag, no settle in flight): on mount, when a settle to
	 * peek finishes, and when the peek content resizes. It is never taken
	 * from a height measured during or before a snap change.
	 */
	function atRestAtPeek(): boolean {
		return shown === 'peek' && snap === 'peek' && h === undefined && !anim && !drag;
	}
	function measurePeek(): void {
		if (el && atRestAtPeek() && el.offsetHeight > 0) peekHeight = el.offsetHeight;
	}
	onMount(() => {
		measurePeek();
		const ro = new ResizeObserver(measurePeek);
		ro.observe(el);
		return () => ro.disconnect();
	});

	function pulledHeight(): number {
		if (!probe) return el.offsetHeight;
		return el.getBoundingClientRect().bottom - probe.getBoundingClientRect().top;
	}

	function heightOf(s: Snap): number {
		return s === 'peek' ? peekHeight : pulledHeight();
	}

	function settle(target: Snap): void {
		clearTimeout(timer);
		anim = true;
		h = heightOf(target);
		if (target !== shown) {
			shown = target;
			onsnap?.(target);
		}
		timer = setTimeout(async () => {
			anim = false;
			h = undefined;
			await tick();
			measurePeek();
		}, SETTLE_MS + 20);
	}

	async function animateFrom(from: number, target: Snap): Promise<void> {
		clearTimeout(timer);
		anim = false;
		h = from;
		await tick();
		void el.offsetHeight; // commit the start height before transitioning
		settle(target);
	}

	// External snap changes (Esc, the scrim, the shell) between peek and
	// pulled. A pre-effect: the start height is read before the DOM takes the
	// new snap's layout, then pinned (`h`) so the change animates.
	$effect.pre(() => {
		const s = snap;
		if (s === shown) return;
		const prev = shown;
		shown = s;
		if ((prev === 'peek' || prev === 'pulled') && (s === 'peek' || s === 'pulled') && el) {
			void animateFrom(el.offsetHeight, s);
		}
	});

	// --- Pointer drag ---------------------------------------------------------

	type Drag = {
		id: number;
		y0: number;
		startH: number;
		pulledH: number;
		samples: { t: number; y: number }[];
		active: boolean;
	};
	let drag: Drag | undefined;
	let suppressClick = false;

	const INTERACTIVE = 'button, a, input, select, textarea, [role="option"]';

	function onpointerdown(e: PointerEvent): void {
		if (e.pointerType === 'mouse' && e.button !== 0) return;
		const t = e.target as Element;
		const zone = t.closest('[data-sheet-drag]');
		if (!zone || !el.contains(zone)) return;
		if (!t.closest('[data-sheet-handle]') && t.closest(INTERACTIVE)) return;
		drag = {
			id: e.pointerId,
			y0: e.clientY,
			startH: el.offsetHeight,
			pulledH: snappy ? pulledHeight() : 0,
			samples: [{ t: e.timeStamp, y: e.clientY }],
			active: false
		};
		// Until the drag starts (and captures the pointer) a mouse can leave
		// the handle, so follow the pointer on the window.
		window.addEventListener('pointermove', onpointermove, { passive: false });
		window.addEventListener('pointerup', onpointerup);
		window.addEventListener('pointercancel', onpointercancel);
	}

	function unlisten(): void {
		window.removeEventListener('pointermove', onpointermove);
		window.removeEventListener('pointerup', onpointerup);
		window.removeEventListener('pointercancel', onpointercancel);
	}

	const onpointerup = (e: PointerEvent) => end(e, false);
	const onpointercancel = (e: PointerEvent) => end(e, true);

	function onpointermove(e: PointerEvent): void {
		const d = drag;
		if (!d || e.pointerId !== d.id) return;
		const dy = e.clientY - d.y0;
		if (!d.active) {
			if (isTap(dy)) return;
			d.active = true;
			clearTimeout(timer);
			anim = false;
			try {
				el.setPointerCapture(e.pointerId);
			} catch {
				// The pointer is already gone; the drag still follows moves until up.
			}
		}
		e.preventDefault();
		d.samples.push({ t: e.timeStamp, y: e.clientY });
		if (d.samples.length > 12) d.samples.shift();
		if (snappy) {
			const peek = peekHeight;
			h = dragHeight(d.startH, dy, peek, d.pulledH);
			const nearer = releaseSnap(h, peek, d.pulledH, 0);
			if (nearer !== shown) {
				shown = nearer;
				onsnap?.(nearer);
			}
		} else {
			ty = Math.max(0, dy);
		}
	}

	function end(e: PointerEvent, cancelled: boolean): void {
		const d = drag;
		if (!d || e.pointerId !== d.id) return;
		drag = undefined;
		unlisten();
		if (!d.active) return;
		suppressClick = true;
		setTimeout(() => (suppressClick = false), 0);
		const vy = cancelled ? 0 : velocity(d.samples);
		if (snappy) {
			const peek = peekHeight;
			settle(releaseSnap(h ?? d.startH, peek, d.pulledH, vy));
			return;
		}
		const dismiss = !cancelled && releaseDismiss(ty, el.offsetHeight, vy);
		anim = true;
		if (dismiss) {
			onclose?.();
			return;
		}
		ty = 0;
		clearTimeout(timer);
		timer = setTimeout(() => (anim = false), SETTLE_MS + 20);
	}

	function ontap(): void {
		if (suppressClick || !snappy) return;
		void animateFrom(el.offsetHeight, snap === 'peek' ? 'pulled' : 'peek');
	}

	$effect(() => () => {
		clearTimeout(timer);
		unlisten();
	});
</script>

{#if snappy}
	<div class="probe" bind:this={probe} aria-hidden="true"></div>
{/if}
<!-- svelte-ignore a11y_no_static_element_interactions (pointer drag on the handle and header; the handle is a button for keyboards) -->
<div
	class="sheet {snap} {surface}"
	class:anim
	class:hidden
	class:sized={h !== undefined}
	style:height={h === undefined ? undefined : `${h}px`}
	style:transform={ty ? `translateY(${ty}px)` : undefined}
	bind:this={el}
	{onpointerdown}
	inert={hidden}
	data-snap={snap}
	data-testid="sheet-{snap}"
>
	{#if handle}
		{#if snappy}
			<button
				class="handle"
				type="button"
				data-sheet-handle
				data-sheet-drag
				aria-label={snap === 'peek' ? 'Expand sheet' : 'Collapse sheet'}
				aria-expanded={snap === 'pulled'}
				onclick={ontap}><span class="bar"></span></button
			>
		{:else}
			<div class="handle" data-sheet-handle data-sheet-drag aria-hidden="true"><span class="bar"></span></div>
		{/if}
	{/if}
	{@render children()}
</div>

<style>
	.probe {
		position: absolute;
		left: 0;
		top: calc(env(safe-area-inset-top) + 130px);
		width: 0;
		height: 0;
		visibility: hidden;
		pointer-events: none;
	}
	.sheet {
		--sheet-bottom: max(30px, calc(env(safe-area-inset-bottom) + 16px));
		position: absolute;
		left: 0;
		right: 0;
		bottom: 0;
		z-index: 30;
		box-sizing: border-box;
		display: flex;
		flex-direction: column;
		border-radius: 30px 30px 0 0;
		box-shadow: var(--shadow-lg);
		color: var(--color-text);
		overflow: hidden;
		padding-top: 10px;
	}
	.sheet.anim {
		transition:
			height 0.15s ease,
			transform 0.15s ease;
	}
	.sheet.glass {
		background: var(--glass);
		backdrop-filter: blur(14px);
		-webkit-backdrop-filter: blur(14px);
	}
	.sheet.surface {
		background: var(--color-surface);
	}
	.sheet.peek,
	.sheet.pulled {
		border-top: 1px solid var(--color-divider);
	}
	.sheet.pulled:not(.sized) {
		top: calc(env(safe-area-inset-top) + 130px);
	}
	.sheet.full {
		z-index: 50;
		top: calc(env(safe-area-inset-top) + 8px);
	}
	.sheet.auto {
		z-index: 50;
		max-height: calc(100% - env(safe-area-inset-top) - 84px);
	}
	.sheet.hidden {
		transform: translateY(110%);
		transition: transform 0.15s ease;
		pointer-events: none;
	}
	.handle {
		flex: none;
		align-self: center;
		display: grid;
		place-items: center;
		width: 88px;
		height: 25px;
		margin: -10px 0 -10px;
		padding: 0;
		border: 0;
		background: transparent;
		cursor: grab;
		touch-action: none;
		border-radius: 999px;
	}
	.handle:focus-visible {
		outline-offset: -2px;
	}
	.bar {
		width: 44px;
		height: 5px;
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-text) 28%, transparent);
	}
	.sheet :global([data-sheet-drag]) {
		touch-action: none;
	}
</style>
