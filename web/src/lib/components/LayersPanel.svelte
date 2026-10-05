<!--
  Map layers (DESIGN-NOTES §3.13, §3.18, §5.3 and the Layers ruling).

  - `desktop`: the 318 px dropdown under the Layers button. "Map layers",
    8 switch rows (a 28 px ink disc with the icon, dimmed when off; the
    label with its sub-line; the count; a 38×22 switch), the portals
    "Show connections" checkbox sub-row (disabled while Portals is off) and
    the biome legend.
  - `mobile`: the menu-sheet block. "Layers", a 2-column grid of tiles with
    the short labels and no counts, and the "Show portal connections" row.

  Counts are of the markers the server sent, already filtered to the
  explored mask (`layerCounts`).
-->
<script lang="ts">
	import Check from 'lucide-svelte/icons/check';
	import { LAYERS, type LayerKey } from '$lib/markers';
	import Legend from './Legend.svelte';
	import MarkerIcon from './MarkerIcon.svelte';

	const CREAM = '#f5ead8';

	let {
		layers,
		portalLinks,
		counts,
		variant,
		ontoggle,
		onlinks,
		id
	}: {
		layers: Record<LayerKey, boolean>;
		portalLinks: boolean;
		counts: Record<LayerKey, number> & { unpaired: number; pairs: number };
		variant: 'desktop' | 'mobile';
		ontoggle: (key: LayerKey) => void;
		onlinks: () => void;
		id?: string;
	} = $props();

	const linksOn = $derived(layers.portals && portalLinks);
	const pairsText = $derived(`${counts.pairs} ${counts.pairs === 1 ? 'pair' : 'pairs'}`);

	function sub(key: LayerKey): { text: string; accent: boolean } | undefined {
		if (key === 'portals' && counts.unpaired > 0) return { text: `${counts.unpaired} unpaired`, accent: true };
		if (key === 'landmarks' || key === 'dungeons' || key === 'minor')
			return { text: 'Only where someone has been', accent: false };
		return undefined;
	}
</script>

{#snippet checkbox(size: number, icon: number)}
	<span class="cb" class:on={portalLinks} style:width="{size}px" style:height="{size}px" aria-hidden="true">
		{#if portalLinks}<Check size={icon} strokeWidth={2.75} />{/if}
	</span>
{/snippet}

{#if variant === 'desktop'}
	<div class="panel" {id} role="group" aria-labelledby="{id ?? 'layers'}-title">
		<div class="title" id="{id ?? 'layers'}-title">Map layers</div>
		{#each LAYERS as l (l.key)}
			{@const on = layers[l.key]}
			{@const s = sub(l.key)}
			<button class="row" type="button" role="switch" aria-checked={on} onclick={() => ontoggle(l.key)}>
				<span class="disc" class:off={!on}><MarkerIcon name={l.icon} size={14} color={CREAM} /></span>
				<span class="label">
					{l.label}
					{#if s}<span class="sub" class:accent={s.accent}>{s.text}</span>{/if}
				</span>
				<span class="count">{counts[l.key]}</span>
				<span class="track" class:on aria-hidden="true"><span class="knob"></span></span>
			</button>
			{#if l.key === 'portals'}
				<button
					class="links"
					type="button"
					role="checkbox"
					aria-checked={portalLinks}
					disabled={!layers.portals}
					onclick={onlinks}
				>
					{@render checkbox(18, 12)}
					<span class="links-label">Show connections</span>
					<span class="count">{pairsText}</span>
				</button>
			{/if}
		{/each}
		<Legend />
	</div>
{:else}
	<div class="m" {id} role="group" aria-labelledby="{id ?? 'layers'}-title">
		<div class="m-title" id="{id ?? 'layers'}-title">Layers</div>
		<div class="grid">
			{#each LAYERS as l (l.key)}
				{@const on = layers[l.key]}
				<button class="tile" class:on type="button" role="switch" aria-checked={on} onclick={() => ontoggle(l.key)}>
					<span class="disc m-disc" class:off={!on}><MarkerIcon name={l.icon} size={15} color={CREAM} /></span>
					<span class="short">{l.short}</span>
				</button>
			{/each}
		</div>
		<button
			class="m-links"
			type="button"
			role="checkbox"
			aria-checked={portalLinks}
			disabled={!layers.portals}
			onclick={onlinks}
		>
			{@render checkbox(22, 14)}
			<span class="m-links-label">Show portal connections</span>
			<span class="count">{pairsText}</span>
		</button>
	</div>
{/if}

<style>
	button {
		font: inherit;
		color: var(--color-text);
		cursor: pointer;
		text-align: left;
	}
	button:disabled {
		opacity: 0.4;
		cursor: not-allowed;
	}
	button:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: -2px;
	}

	/* Desktop dropdown. */
	.panel {
		position: absolute;
		top: 54px;
		right: 0;
		width: 318px;
		box-sizing: border-box;
		padding: 12px;
		border-radius: 26px;
		background: var(--color-surface);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-lg);
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.title {
		font-family: var(--font-heading);
		font-size: 17px;
		padding: 4px 10px 8px;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 12px;
		width: 100%;
		padding: 7px 10px;
		border: 0;
		background: transparent;
		border-radius: 16px;
	}
	.row:hover,
	.links:not(:disabled):hover {
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
	}
	.disc {
		width: 28px;
		height: 28px;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: #26231f;
	}
	.disc.off {
		opacity: 0.35;
	}
	.label {
		flex: 1;
		min-width: 0;
		font-size: 14px;
		line-height: 1.2;
	}
	.sub {
		display: block;
		font-size: 11.5px;
		color: var(--muted);
	}
	.sub.accent {
		color: var(--color-accent-700);
	}
	.count {
		font-size: 12px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}
	.track {
		position: relative;
		width: 38px;
		height: 22px;
		flex: none;
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-text) 22%, transparent);
		transition: background 0.15s;
	}
	.track.on {
		background: var(--cold);
	}
	.knob {
		position: absolute;
		top: 3px;
		left: 3px;
		width: 16px;
		height: 16px;
		border-radius: 50%;
		background: #f5ead8;
		box-shadow: 0 1px 2px rgba(0, 0, 0, 0.3);
		transition: left 0.15s;
	}
	.track.on .knob {
		left: 19px;
	}
	.links {
		display: flex;
		align-items: center;
		gap: 10px;
		margin: -2px 0 4px 40px;
		padding: 6px 10px;
		border: 0;
		background: transparent;
		border-radius: 14px;
		font-size: 13px;
	}
	.links-label {
		flex: 1;
	}
	.cb {
		flex: none;
		box-sizing: border-box;
		border-radius: 6px;
		border: 2px solid color-mix(in srgb, var(--color-text) 40%, transparent);
		background: transparent;
		display: grid;
		place-items: center;
		color: var(--color-bg);
	}
	.cb.on {
		border-color: var(--cold);
		background: var(--cold);
	}

	/* Mobile menu-sheet block. */
	.m {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	.m-title {
		font-family: var(--font-heading);
		font-size: 18px;
		padding-top: 6px;
	}
	.grid {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 8px;
	}
	.tile {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 52px;
		padding: 0 12px 0 8px;
		border-radius: 18px;
		background: transparent;
		border: 1.5px solid var(--color-divider);
	}
	.tile.on {
		background: color-mix(in srgb, var(--cold) 16%, transparent);
		border-color: var(--cold);
	}
	.m-disc {
		width: 32px;
		height: 32px;
	}
	.short {
		flex: 1;
		font-size: 13px;
		line-height: 1.2;
		font-weight: 600;
	}
	.m-links {
		display: flex;
		align-items: center;
		gap: 12px;
		min-height: 48px;
		padding: 0 12px;
		border-radius: 18px;
		border: 1.5px solid var(--color-divider);
		background: transparent;
	}
	.m-links .cb {
		border-radius: 7px;
	}
	.m-links-label {
		flex: 1;
		font-size: 14px;
		font-weight: 600;
	}
</style>
