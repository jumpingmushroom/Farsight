<!--
  Desktop search (DESIGN-NOTES §3.12, §5.2): the 340 px glass input with
  the results panel under it. Results are derived from the query, markers
  and fog mask; hints from the markers.

  - Focus opens the panel (and calls `onopen`, which closes the layers
    panel); leaving the box closes it after 120 ms.
  - ↑/↓ move the active option (aria-activedescendant), Enter picks it (or
    the first result), Esc clears the query, then blurs.
  - Picking calls `onpick` with the marker and blurs the input; the shell
    centres on it and selects it.
  - `disabled` (no save yet, or charting) shows `disabledPlaceholder`.
-->
<script lang="ts">
	import SearchIcon from 'lucide-svelte/icons/search';
	import { onDestroy } from 'svelte';
	import type { MapMarker } from '$lib/markers';
	import { hints as hintsFor, search } from '$lib/search';
	import SearchResults from './SearchResults.svelte';

	let {
		all,
		mask,
		fog,
		disabled = false,
		disabledPlaceholder = 'Waiting for the first save…',
		width = 340,
		onpick,
		onopen
	}: {
		all: MapMarker[];
		mask: Uint8Array | undefined;
		fog: boolean;
		disabled?: boolean;
		disabledPlaceholder?: string;
		/** Box width in px (340 per §3.12; the shell narrows it on small desktops). */
		width?: number;
		onpick: (m: MapMarker) => void;
		onopen?: () => void;
	} = $props();

	const LIST_ID = 'desktop-search-results';
	const BLUR_CLOSE_MS = 120;

	let q = $state('');
	let open = $state(false);
	let active = $state(-1);
	let input: HTMLInputElement;
	let wrap: HTMLDivElement;
	let timer: ReturnType<typeof setTimeout> | undefined;

	const results = $derived(search(all, q, mask, fog));
	const hints = $derived(hintsFor(all));
	const hasQuery = $derived(q.trim() !== '');
	const showPanel = $derived(open && !disabled && (hasQuery || hints.length > 0));

	// A new query resets the keyboard highlight; a refreshed result set
	// (a new save) keeps it, clamped to the results that remain.
	let lastQ = '';
	$effect(() => {
		if (q !== lastQ) {
			lastQ = q;
			active = -1;
		}
	});
	$effect(() => {
		const n = results.length;
		if (active > n - 1) active = n - 1;
	});

	function clearTimer(): void {
		if (timer) clearTimeout(timer);
		timer = undefined;
	}

	function onfocusin(): void {
		clearTimer();
		if (!open) {
			open = true;
			onopen?.();
		}
	}

	function onfocusout(e: FocusEvent): void {
		if (e.relatedTarget instanceof Node && wrap.contains(e.relatedTarget)) return;
		clearTimer();
		timer = setTimeout(() => {
			timer = undefined;
			open = false;
			active = -1;
		}, BLUR_CLOSE_MS);
	}

	/** Closes the panel and blurs the input (a marker was selected, join opened, the map clicked). */
	export function close(): void {
		clearTimer();
		open = false;
		active = -1;
		if (wrap?.contains(document.activeElement)) (document.activeElement as HTMLElement).blur();
	}

	/** Empties the query and closes the panel (a server switch). */
	export function clear(): void {
		q = '';
		close();
	}

	function pick(id: string): void {
		const m = all.find((x) => x.id === id);
		if (!m) return;
		close();
		onpick(m);
	}

	function hint(h: string): void {
		q = h;
		input.focus();
	}

	function onkeydown(e: KeyboardEvent): void {
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
			if (!hasQuery || results.length === 0) return;
			e.preventDefault();
			if (!open) onfocusin();
			const n = results.length;
			active = e.key === 'ArrowDown' ? (active + 1) % n : active <= 0 ? n - 1 : active - 1;
			document.getElementById(`${LIST_ID}-opt-${active}`)?.scrollIntoView({ block: 'nearest' });
		} else if (e.key === 'Enter') {
			const r = results[active] ?? results[0];
			if (!hasQuery || !r) return;
			e.preventDefault();
			pick(r.id);
		} else if (e.key === 'Escape') {
			e.preventDefault();
			if (q !== '') {
				q = '';
			} else {
				close();
			}
		}
	}

	onDestroy(clearTimer);
</script>

<div class="search" style:width="{width}px" bind:this={wrap} {onfocusin} {onfocusout}>
	<span class="icon" aria-hidden="true"><SearchIcon size={18} strokeWidth={2.75} /></span>
	<input
		bind:this={input}
		bind:value={q}
		class="input"
		type="search"
		autocomplete="off"
		spellcheck="false"
		aria-label="Search the map"
		role="combobox"
		aria-expanded={showPanel}
		aria-controls={LIST_ID}
		aria-autocomplete="list"
		aria-activedescendant={showPanel && active >= 0 ? `${LIST_ID}-opt-${active}` : undefined}
		placeholder={disabled ? disabledPlaceholder : 'Portals, signs, tames, builders…'}
		{disabled}
		{onkeydown}
	/>
	<!-- Always present (aria-controls target); mousedown on its padding or
	     scrollbar must not blur the input. -->
	<div class="panel" id={LIST_ID} hidden={!showPanel} role="presentation" onmousedown={(e) => e.preventDefault()}>
		{#if showPanel}
			<SearchResults {results} {hints} {q} {active} listId={LIST_ID} onpick={pick} onhint={hint} />
		{/if}
	</div>
</div>

<style>
	.search {
		position: relative;
	}
	.icon {
		position: absolute;
		left: 16px;
		top: 14px;
		width: 18px;
		height: 18px;
		display: grid;
		opacity: 0.7;
		pointer-events: none;
		z-index: 1;
	}
	.input {
		box-sizing: border-box;
		height: 46px;
		padding-left: 44px;
		background: var(--glass);
		box-shadow: var(--shadow-md);
		font-size: 14px;
	}
	.input:disabled {
		cursor: not-allowed;
	}
	.input::-webkit-search-cancel-button {
		display: none;
	}
	.input::placeholder {
		color: var(--muted);
	}
	.panel {
		position: absolute;
		top: 54px;
		left: 0;
		right: 0;
		padding: 10px;
		border-radius: 24px;
		background: var(--color-surface);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-lg);
		display: flex;
		flex-direction: column;
		gap: 2px;
		max-height: 420px;
		overflow: auto;
	}
	.panel[hidden] {
		display: none;
	}
</style>
