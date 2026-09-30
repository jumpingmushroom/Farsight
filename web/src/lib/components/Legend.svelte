<!--
  The layers-panel legend (DESIGN-NOTES §3.13): a 3-column grid of the tile
  renderer's biome colours plus Unexplored, each a 14 px swatch and a name.
-->
<script lang="ts">
	import { BIOME_LEGEND } from '$lib/markers';
</script>

<ul class="legend" aria-label="Map colours">
	{#each BIOME_LEGEND as b (b.name)}
		<li><span class="swatch" style:background={b.hex}></span>{b.name}</li>
	{/each}
</ul>

<style>
	.legend {
		list-style: none;
		margin: 6px 0 0;
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 6px 8px;
		padding: 12px 10px 4px;
		border-top: 1px solid var(--color-divider);
	}
	li {
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		min-width: 0;
		white-space: nowrap;
	}
	.swatch {
		width: 14px;
		height: 14px;
		flex: none;
		box-sizing: border-box;
		border-radius: 50%;
		border: 1px solid rgba(0, 0, 0, 0.2);
	}
</style>
