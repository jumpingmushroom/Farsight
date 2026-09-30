<!-- Explored bar (DESIGN-NOTES §3.9 item 5): "Explored by the crew" and a sage fill. -->
<script lang="ts">
	import { fmtPct } from '$lib/format';

	let { pct }: { pct: number } = $props();
	const width = $derived(Math.max(0, Math.min(100, pct)));
</script>

<div class="explored">
	<div class="row"><span>Explored by the crew</span><b>{fmtPct(pct)}</b></div>
	<div
		class="track"
		role="progressbar"
		aria-label="Explored by the crew"
		aria-valuemin={0}
		aria-valuemax={100}
		aria-valuenow={width}
	>
		<div class="fill" style:width="{width}%"></div>
	</div>
</div>

<style>
	.explored {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.row {
		display: flex;
		justify-content: space-between;
		font-size: 13px;
	}
	.row b {
		font-variant-numeric: tabular-nums;
	}
	.track {
		height: 6px;
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-text) 12%, transparent);
		overflow: hidden;
	}
	.fill {
		height: 100%;
		border-radius: 999px;
		background: var(--color-accent-2-500);
	}
</style>
