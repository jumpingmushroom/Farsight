<!--
  The Layers button (DESIGN-NOTES §3.13): a 46 px glass pill with the layers
  icon, "Layers" and the count of enabled main layers. Its border turns
  --cold while the panel is open.
-->
<script lang="ts">
	import LayersIcon from 'lucide-svelte/icons/layers';

	let {
		count,
		open,
		controls,
		onclick
	}: {
		count: number;
		open: boolean;
		controls: string;
		onclick: () => void;
	} = $props();

	let el: HTMLButtonElement;

	export function focus(): void {
		el?.focus();
	}
</script>

<button
	bind:this={el}
	class="layers-btn"
	class:open
	type="button"
	aria-expanded={open}
	aria-controls={open ? controls : undefined}
	aria-label="Layers · {count} on"
	{onclick}
>
	<LayersIcon size={18} strokeWidth={2.75} />
	Layers
	<span class="badge" aria-hidden="true">{count}</span>
</button>

<style>
	.layers-btn {
		height: 46px;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 0 18px;
		border-radius: 999px;
		border: 1px solid var(--color-divider);
		background: var(--glass);
		box-shadow: var(--shadow-md);
		color: var(--color-text);
		font: inherit;
		font-size: 14px;
		font-weight: 700;
		cursor: pointer;
		white-space: nowrap;
	}
	.layers-btn.open {
		border-color: var(--cold);
	}
	.badge {
		box-sizing: border-box;
		min-width: 22px;
		height: 22px;
		padding: 0 7px;
		border-radius: 999px;
		background: var(--cold);
		color: var(--color-bg);
		font-size: 12px;
		display: grid;
		place-items: center;
		font-variant-numeric: tabular-nums;
	}
</style>
