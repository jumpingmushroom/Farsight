<!--
  Scale and cursor readout (DESIGN-NOTES §3.14, §5.7, §5.8): the scale-bar
  bracket and label for the map's zoom, then the world coordinates under the
  pointer (rAF-throttled), then a status slot: "World edge" outside the disc,
  "Unexplored" when the fog is on and the 12 m cell isn't explored, otherwise
  empty (the client has no biome data, [GAP]). Hidden on coarse pointers.
-->
<script lang="ts">
	import type L from 'leaflet';
	import { isExplored } from '$lib/explored';
	import { fmtN } from '$lib/format';
	import { fromLatLng, insideWorld, scaleBar } from '$lib/geo';

	let {
		map,
		mask,
		fog,
		left = 16
	}: { map: L.Map | undefined; mask?: Uint8Array; fog: boolean; left?: number } = $props();

	let zoom = $state(1.75);
	let cursor = $state<{ x: number; z: number } | undefined>(undefined);

	const bar = $derived(scaleBar(zoom));
	const place = $derived.by(() => {
		if (!cursor) return 'Hover the map';
		if (!insideWorld(cursor.x, cursor.z)) return 'World edge';
		if (fog && mask && !isExplored(mask, cursor.x, cursor.z)) return 'Unexplored';
		return '';
	});

	$effect(() => {
		const m = map;
		if (!m) return;
		let frame = 0;
		let pending: L.LatLng | undefined;
		const onzoom = () => (zoom = m.getZoom());
		const onmove = (e: L.LeafletMouseEvent) => {
			pending = e.latlng;
			if (!frame)
				frame = requestAnimationFrame(() => {
					frame = 0;
					if (pending) cursor = fromLatLng(pending);
				});
		};
		const onleave = () => {
			pending = undefined;
			cursor = undefined;
		};
		onzoom();
		m.on('zoom', onzoom);
		m.on('mousemove', onmove);
		m.on('mouseout', onleave);
		return () => {
			cancelAnimationFrame(frame);
			m.off('zoom', onzoom);
			m.off('mousemove', onmove);
			m.off('mouseout', onleave);
		};
	});
</script>

<div class="readout" style:left="{left}px">
	<div class="scale" aria-label="Scale {bar.label}">
		<span class="bracket" style:width="{bar.px}px"></span>
		<span class="scale-label">{bar.label}</span>
	</div>
	<span class="divider" aria-hidden="true"></span>
	<b class="coords">
		{#if cursor}X {fmtN(cursor.x)} · Z {fmtN(cursor.z)}{:else}X — · Z —{/if}
	</b>
	<span class="place">{place}</span>
</div>

<style>
	.readout {
		position: absolute;
		bottom: 16px;
		display: flex;
		align-items: center;
		gap: 14px;
		padding: 9px 16px;
		border-radius: 999px;
		background: var(--glass);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-md);
		font-size: 13px;
		font-variant-numeric: tabular-nums;
		pointer-events: none;
	}
	.scale {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 2px;
	}
	.bracket {
		display: block;
		height: 6px;
		box-sizing: border-box;
		border: 2px solid var(--color-text);
		border-top: 0;
	}
	.scale-label {
		font-size: 11px;
		color: var(--muted);
		line-height: 1;
	}
	.divider {
		width: 1px;
		align-self: stretch;
		background: var(--color-divider);
	}
	.coords {
		min-width: 170px;
		font-weight: 700;
	}
	.place {
		min-width: 90px;
		color: var(--muted);
	}
	@media (pointer: coarse) {
		.readout {
			display: none;
		}
	}
</style>
