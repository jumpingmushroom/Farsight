<!--
  World tab (DESIGN-NOTES §3.9): day and boss tiles, "Forsaken defeated"
  with the boss grid, "Next up" with "Show altar" (only when the snapshot
  has that boss's altar; hidden once every boss is defeated), the world
  rules, the explored bar and the footnote. Without a world save it shows
  "No world save read yet."
-->
<script lang="ts">
	import { nearestAltar, nextBoss } from '$lib/derive';
	import type { Marker, SnapshotView, WorldCard } from '$lib/types';
	import BossGrid from './BossGrid.svelte';
	import ExploredBar from './ExploredBar.svelte';
	import WorldRules from './WorldRules.svelte';

	let {
		world,
		snapshot,
		onshowaltar
	}: {
		world: WorldCard | undefined;
		snapshot: SnapshotView | null | undefined;
		onshowaltar: (altar: Marker) => void;
	} = $props();

	const defeated = $derived(world ? world.bosses.filter((b) => b.defeated).length : 0);
	const next = $derived(world ? nextBoss(world) : undefined);
	const altar = $derived(next && snapshot ? nearestAltar(snapshot.locations, next.name) : undefined);
</script>

{#if !world}
	<p class="empty">No world save read yet.</p>
{:else}
	<div class="world">
		<div class="tiles">
			<div class="tile">
				<div class="label">In-game day</div>
				<div class="big">{world.day}</div>
			</div>
			<div class="tile">
				<div class="label">Bosses</div>
				<div class="big">{defeated}<span class="of">{` / ${world.bosses.length}`}</span></div>
			</div>
		</div>
		<div>
			<h3 class="h">Forsaken defeated</h3>
			<BossGrid bosses={world.bosses} />
		</div>
		{#if next}
			<div class="next">
				<p>
					Next up: <b>{next.name}</b>{#if next.biome}, whose altar is in the {next.biome}{/if}.
				</p>
				{#if altar}
					<button class="btn btn-secondary" type="button" onclick={() => onshowaltar(altar)}>Show altar</button>
				{/if}
			</div>
		{/if}
		<WorldRules {world} />
		<ExploredBar pct={world.exploredPct} />
		<p class="foot">
			Bosses come from the world’s progress keys. Exploration comes from the cartography tables’
			shared map, plus the area around anything built.
		</p>
	</div>
{/if}

<style>
	.empty {
		margin: 0;
		padding: 24px 6px;
		font-size: 13px;
		color: var(--muted);
	}
	.world {
		display: flex;
		flex-direction: column;
		gap: 18px;
		padding: 8px 6px;
	}
	.tiles {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 10px;
	}
	.tile {
		padding: 14px 16px;
		border-radius: 22px;
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
	}
	.label {
		font-size: 12px;
		color: var(--muted);
	}
	.big {
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 34px;
		line-height: 1.1;
	}
	.of {
		font-size: 20px;
		color: var(--muted);
	}
	.h {
		margin: 0 0 10px;
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 17px;
		line-height: 1.55;
		letter-spacing: normal;
	}
	.next {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 12px 14px;
		border-radius: 20px;
		border: 1px dashed var(--color-divider);
	}
	.next p {
		flex: 1;
		margin: 0;
		font-size: 13px;
		line-height: 1.4;
	}
	.next .btn {
		font-size: 13px;
		flex: none;
	}
	.foot {
		margin: 0;
		font-size: 12px;
		color: var(--muted);
		line-height: 1.45;
	}
</style>
