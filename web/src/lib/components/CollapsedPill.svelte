<!--
  Collapsed panel pill (DESIGN-NOTES §3.4): the 32 px app mark, a status dot,
  "{players} online" (or the status word when not online) and a chevron.
  Clicking it re-opens the panel. `#panel-pill` is DesktopShell's focus
  target when a view closes over the collapsed panel.
-->
<script lang="ts">
	import ChevronRight from 'lucide-svelte/icons/chevron-right';
	import { statusView } from '$lib/derive';
	import { app } from '$lib/state.svelte';
	import AppMark from './AppMark.svelte';

	let { onopen }: { onopen: () => void } = $props();

	const summary = $derived(app.servers.find((s) => s.id === app.currentId));
	const card = $derived(app.card?.id === app.currentId ? app.card : undefined);
	const status = $derived(card?.status ?? summary?.status ?? 'unknown');
	const players = $derived(card?.players ?? summary?.players ?? 0);
	const text = $derived(status === 'online' ? `${players} online` : statusView(status).label);
</script>

<button id="panel-pill" class="pill" type="button" aria-label="Expand panel · {text}" onclick={onopen}>
	<AppMark size={32} font={17} />
	<span class="dot" style:background={statusView(status).dot}></span>
	<span class="text">{text}</span>
	<ChevronRight size={16} strokeWidth={2.75} />
</button>

<style>
	.pill {
		position: absolute;
		left: 16px;
		top: 16px;
		height: 48px;
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 0 16px 0 8px;
		border-radius: 999px;
		border: 1px solid var(--color-divider);
		background: var(--glass);
		box-shadow: var(--shadow-md);
		color: var(--color-text);
		font: inherit;
		cursor: pointer;
	}
	.dot {
		width: 9px;
		height: 9px;
		flex: none;
		border-radius: 50%;
	}
	.text {
		font-weight: 700;
		font-size: 14px;
		white-space: nowrap;
	}
</style>
