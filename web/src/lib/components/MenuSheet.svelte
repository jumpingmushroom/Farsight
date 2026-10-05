<!--
  Mobile menu sheet (DESIGN-NOTES §1.2 frame 3, §3.18 and the Mobile ruling):
  an auto-height surface sheet without a handle, opened from the top-bar
  menu button. It holds:
  - the search field (52 px, 16 px text so iOS doesn't zoom);
  - with an empty query, the hint tags, then Layers (the 2-column tile grid
    and "Show portal connections"), then the theme row "Theme · Dark/Light";
  - with a query, the result rows (or "Nothing matches…") in place of the
    hints and layers. Picking a result calls `onpick` (the shell closes the
    menu and selects the marker at zoom 4.25). Enter picks the first result.
  Disabled search (no save yet, charting) shows `disabledPlaceholder`.
  No drag zone (the design gives this sheet no handle): the whole sheet
  scrolls normally; Esc, the scrim or the top-bar x close it. Not
  aria-modal, because its toggle in the top bar must stay reachable.
-->
<script lang="ts">
	import SearchIcon from 'lucide-svelte/icons/search';
	import { onMount } from 'svelte';
	import type { LayerKey, MapMarker } from '$lib/markers';
	import { hints as hintsFor, search } from '$lib/search';
	import { app } from '$lib/state.svelte';
	import BottomSheet from './BottomSheet.svelte';
	import LayersPanel from './LayersPanel.svelte';
	import SearchResults from './SearchResults.svelte';
	import ThemeToggle from './ThemeToggle.svelte';

	let {
		all,
		disabled,
		disabledPlaceholder,
		layers,
		portalLinks,
		counts,
		ontoggle,
		onlinks,
		onpick,
		onclose
	}: {
		all: MapMarker[];
		disabled: boolean;
		disabledPlaceholder: string;
		layers: Record<LayerKey, boolean>;
		portalLinks: boolean;
		counts: Record<LayerKey, number> & { unpaired: number; pairs: number };
		ontoggle: (key: LayerKey) => void;
		onlinks: () => void;
		onpick: (m: MapMarker) => void;
		onclose: () => void;
	} = $props();

	const uid = $props.id();
	const listId = `${uid}-search`;

	let q = $state('');
	let input: HTMLInputElement;
	let root: HTMLDivElement;

	const query = $derived(disabled ? '' : q.trim());
	const results = $derived(query ? search(all, q) : []);
	const hints = $derived(disabled ? [] : hintsFor(all));

	function pick(id: string): void {
		const m = all.find((x) => x.id === id);
		if (m) onpick(m);
	}

	function hint(h: string): void {
		q = h;
		input.focus();
	}

	function onkeydown(e: KeyboardEvent): void {
		if (e.key === 'Enter' && results[0]) {
			e.preventDefault();
			pick(results[0].id);
		} else if (e.key === 'Escape' && q !== '') {
			// Esc clears the query first; a second Esc (the shell) closes the menu.
			e.preventDefault();
			q = '';
		}
	}

	// Keyboard users land in the sheet; the field isn't focused so the
	// on-screen keyboard doesn't cover the layers.
	onMount(() => root.focus({ preventScroll: true }));
</script>

<BottomSheet snap="auto" surface="surface" handle={false} {onclose}>
	<div class="menu" role="dialog" aria-label="Search and layers" tabindex="-1" bind:this={root}>
		<div class="search">
			<span class="icon" aria-hidden="true"><SearchIcon size={20} strokeWidth={2.75} /></span>
			<input
				bind:this={input}
				bind:value={q}
				class="input"
				type="search"
				enterkeyhint="search"
				autocomplete="off"
				spellcheck="false"
				aria-label="Search the map"
				aria-controls={query ? `${listId}-listbox` : undefined}
				placeholder={disabled ? disabledPlaceholder : 'Portals, signs, tames, builders…'}
				{disabled}
				{onkeydown}
			/>
		</div>
		{#if query}
			<div class="results">
				<SearchResults {results} {hints} q={query} {listId} onpick={pick} onhint={hint} mobile />
			</div>
		{:else}
			{#if hints.length}
				<SearchResults results={[]} {hints} q="" {listId} onpick={pick} onhint={hint} mobile />
			{/if}
			<LayersPanel id="{uid}-layers" variant="mobile" {layers} {portalLinks} {counts} {ontoggle} {onlinks} />
			<div class="theme">
				<span class="theme-label">Theme · {app.theme === 'dark' ? 'Dark' : 'Light'}</span>
				<ThemeToggle />
			</div>
		{/if}
	</div>
</BottomSheet>

<style>
	.menu {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 8px 16px var(--sheet-bottom);
		outline: none;
	}
	.search {
		position: relative;
		flex: none;
	}
	.icon {
		position: absolute;
		left: 18px;
		top: 16px;
		width: 20px;
		height: 20px;
		display: grid;
		opacity: 0.7;
		pointer-events: none;
	}
	.input {
		box-sizing: border-box;
		width: 100%;
		height: 52px;
		padding-left: 48px;
		font-size: 16px;
	}
	.input::-webkit-search-cancel-button {
		display: none;
	}
	.input::placeholder {
		color: var(--muted);
	}
	.input:disabled {
		cursor: not-allowed;
	}
	.results {
		margin: 0 -8px;
	}
	.results :global(.row) {
		min-height: 52px;
		box-sizing: border-box;
	}
	.results :global(.title) {
		font-size: 15px;
	}
	.results :global(.sub) {
		font-size: 12.5px;
	}
	.theme {
		display: flex;
		align-items: center;
		gap: 12px;
		min-height: 48px;
		padding: 0 6px 0 12px;
		border-radius: 18px;
		border: 1.5px solid var(--color-divider);
	}
	.theme-label {
		flex: 1;
		font-size: 14px;
		font-weight: 600;
	}
</style>
