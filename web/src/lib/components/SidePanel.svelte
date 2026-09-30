<!--
  Desktop side panel (DESIGN-NOTES §1.1 item 3, §3.2, §3.6): header with the
  app mark, theme toggle and collapse; the server card; the "Online · N" /
  "World" tabs (role=tablist, ←/→ switch) over a scrolling tab body.
-->
<script lang="ts">
	import ChevronLeft from 'lucide-svelte/icons/chevron-left';
	import { app } from '$lib/state.svelte';
	import type { Marker } from '$lib/types';
	import AppMark from './AppMark.svelte';
	import PlayersTab from './PlayersTab.svelte';
	import ServerCard from './ServerCard.svelte';
	import ThemeToggle from './ThemeToggle.svelte';
	import WorldTab from './WorldTab.svelte';

	type PanelTab = 'players' | 'world';

	let {
		tab = $bindable('players'),
		oncollapse,
		onjoin,
		onshowaltar
	}: {
		tab?: PanelTab;
		oncollapse: () => void;
		onjoin: () => void;
		onshowaltar: (altar: Marker) => void;
	} = $props();

	const card = $derived(app.card?.id === app.currentId ? app.card : undefined);
	const players = $derived(card?.players ?? app.servers.find((s) => s.id === app.currentId)?.players ?? 0);

	function ontabkey(e: KeyboardEvent): void {
		if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
		e.preventDefault();
		tab = tab === 'players' ? 'world' : 'players';
		document.getElementById(`tab-${tab}`)?.focus();
	}
</script>

<aside class="panel" aria-label="Server panel">
	<div class="head">
		<AppMark />
		<div class="title">
			<div class="app">Farsight</div>
			<div class="tagline">Valheim server atlas</div>
		</div>
		<ThemeToggle />
		<button class="btn btn-secondary btn-icon" type="button" aria-label="Collapse panel" onclick={oncollapse}>
			<ChevronLeft size={17} strokeWidth={2.75} />
		</button>
	</div>
	<ServerCard {onjoin} />
	<div class="tabs" role="tablist" aria-label="Panel">
		<button
			id="tab-players"
			class="tab"
			type="button"
			role="tab"
			aria-selected={tab === 'players'}
			aria-controls="tabpanel"
			tabindex={tab === 'players' ? 0 : -1}
			onclick={() => (tab = 'players')}
			onkeydown={ontabkey}>Online · {players}</button
		>
		<button
			id="tab-world"
			class="tab"
			type="button"
			role="tab"
			aria-selected={tab === 'world'}
			aria-controls="tabpanel"
			tabindex={tab === 'world' ? 0 : -1}
			onclick={() => (tab = 'world')}
			onkeydown={ontabkey}>World</button
		>
	</div>
	<div class="body" id="tabpanel" role="tabpanel" aria-labelledby="tab-{tab}" tabindex="0">
		{#if tab === 'players'}
			{#if card}
				<PlayersTab {card} now={app.now} />
			{/if}
		{:else if card}
			<WorldTab world={card.world} snapshot={app.snapshot} {onshowaltar} />
		{/if}
	</div>
</aside>

<style>
	.panel {
		position: absolute;
		left: 16px;
		top: 16px;
		bottom: 16px;
		width: 344px;
		display: flex;
		flex-direction: column;
		border-radius: 30px;
		background: var(--glass);
		backdrop-filter: blur(14px);
		-webkit-backdrop-filter: blur(14px);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-lg);
		overflow: hidden;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 18px 16px 14px 18px;
	}
	.title {
		flex: 1;
		min-width: 0;
	}
	.app {
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 20px;
		line-height: 1.1;
	}
	.tagline {
		font-size: 12px;
		color: var(--muted);
	}
	.tabs {
		display: flex;
		gap: 6px;
		margin: 14px 14px 4px;
		padding: 4px;
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
	}
	.tab {
		flex: 1;
		border: 0;
		border-radius: 999px;
		padding: 8px 0;
		font: inherit;
		font-size: 14px;
		font-weight: 700;
		cursor: pointer;
		background: transparent;
		color: var(--muted);
	}
	.tab[aria-selected='true'] {
		background: var(--color-surface);
		color: var(--color-text);
	}
	.body {
		flex: 1;
		min-height: 0;
		overflow: auto;
		padding: 10px 14px 18px;
	}
	.body:focus-visible {
		outline-offset: -2px;
	}
</style>
