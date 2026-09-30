<!--
  World rules card (DESIGN-NOTES §3.10): the preset name, a 2-column grid of
  rule tiles (orange when changed from Normal) and the footnote. Shared by
  the World tab and the join dialog / sheet.
-->
<script lang="ts">
	import { worldRules } from '$lib/derive';
	import type { WorldCard } from '$lib/types';

	let { world }: { world: WorldCard } = $props();
	const rules = $derived(worldRules(world));
</script>

<section class="rules" aria-label="World rules">
	<div class="head"><span class="title">World rules</span><span class="preset">{rules.preset}</span></div>
	<dl class="grid">
		{#each rules.tiles as t (t.key)}
			<div class="tile">
				<dt>{t.key}</dt>
				<dd class:changed={t.changed}>{t.value}</dd>
			</div>
		{/each}
	</dl>
	<p class="foot">Orange = changed from Normal. Read from the world’s modifier keys.</p>
</section>

<style>
	.rules {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.head {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 8px;
	}
	.title {
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 16px;
	}
	.preset {
		font-size: 12px;
		color: var(--muted);
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 6px;
		margin: 0;
	}
	.tile {
		padding: 8px 12px;
		border-radius: 14px;
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
	}
	dt {
		font-size: 11.5px;
		color: var(--muted);
	}
	dd {
		margin: 0;
		font-size: 13.5px;
		font-weight: 700;
		color: var(--color-text);
	}
	dd.changed {
		color: var(--color-accent-700);
	}
	.foot {
		margin: 0;
		font-size: 11.5px;
		color: var(--muted);
	}
</style>
