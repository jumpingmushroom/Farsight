<!--
  "Map updated" pill (DESIGN-NOTES §3.11 and the plan ruling): the map age
  and next-save estimate, the save-cycle bar, and the snapshot line. Derived
  from `now`, so it re-renders on the app's 30 s tick. Without a save
  interval the next-save clause and the bar are dropped.
-->
<script lang="ts">
	import MapIcon from 'lucide-svelte/icons/map';
	import { mapPill } from '$lib/derive';
	import type { WorldCard } from '$lib/types';

	let {
		world,
		timeZone,
		now,
		left,
		top = 16,
		width = $bindable(0),
		height = $bindable(0)
	}: {
		world: WorldCard;
		/** The server's zone (the card's `timeZone`), for the autosave's clock. */
		timeZone: string | undefined;
		now: Date;
		left: number;
		/** 16, or below the search/layers cluster when they'd collide (the shell decides). */
		top?: number;
		/** The rendered width, for the shell's slot logic. */
		width?: number;
		/** The rendered height, for the time pill under it. */
		height?: number;
	} = $props();

	const pill = $derived(mapPill(world, now, timeZone ?? 'UTC'));
	const next = $derived(
		pill.next === undefined ? '' : pill.next === 'any moment' ? ' · next save any moment' : ` · next save in ${pill.next}`
	);
</script>

<div class="pill" style:left="{left}px" style:top="{top}px" bind:clientWidth={width} bind:clientHeight={height}>
	<div class="row">
		<span class="icon" aria-hidden="true"><MapIcon size={17} strokeWidth={2.75} /></span>
		<span class="text"><b>Map updated {pill.age}</b>{#if next}<span class="muted">{next}</span>{/if}</span>
	</div>
	{#if pill.progress !== undefined}
		<div class="track" aria-hidden="true"><div class="fill" style:width="{pill.progress * 100}%"></div></div>
	{/if}
	<div class="line">Snapshot from the {pill.savedClock} autosave · markers don’t move between saves</div>
</div>

<style>
	.pill {
		position: absolute;
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 10px 16px 12px;
		border-radius: 24px;
		background: var(--glass);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-md);
		min-width: 360px;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 10px;
		font-size: 14px;
	}
	.icon {
		display: grid;
		color: var(--cold);
	}
	.text {
		white-space: nowrap;
	}
	.muted {
		color: var(--muted);
	}
	.track {
		height: 5px;
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-text) 12%, transparent);
		overflow: hidden;
	}
	.fill {
		height: 100%;
		border-radius: 999px;
		background: var(--cold);
	}
	.line {
		font-size: 11.5px;
		color: var(--muted);
	}
</style>
