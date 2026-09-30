<!--
  The first-run charting progress card (DESIGN-NOTES §3.22, §6.3), centred
  in the map area right of the panel (`padLeft`). "~{n} min left" only when
  the ETA is known (two tile samples with progress).
-->
<script lang="ts">
	import { fmtInt } from '$lib/format';

	let {
		pct,
		done,
		total,
		etaMin,
		padLeft
	}: {
		pct: number;
		done: number;
		total: number;
		etaMin?: number;
		padLeft: number;
	} = $props();

	const eta = $derived(etaMin === undefined ? undefined : `~${Math.max(1, etaMin)} min left`);
</script>

<div class="area" style:left="{padLeft}px">
	<!-- Only the static title is live: the progress numbers change every poll. -->
	<div class="card" data-testid="charting-card">
		<div class="title" role="status">Charting the world for the first time</div>
		<div
			class="track"
			role="progressbar"
			aria-label="Charting progress"
			aria-valuemin={0}
			aria-valuemax={100}
			aria-valuenow={pct}
		>
			<div class="fill" style:width="{pct}%"></div>
		</div>
		<div class="row">
			<b>{pct}% · {fmtInt(done)} of {fmtInt(total)} tiles</b>
			{#if eta}<span class="muted">{eta}</span>{/if}
		</div>
		<div class="body">Tiles sharpen as they finish. Markers appear once the first pass is done. The online list already works.</div>
	</div>
</div>

<style>
	.area {
		position: absolute;
		top: 0;
		right: 0;
		bottom: 0;
		display: grid;
		place-items: center;
		pointer-events: none;
	}
	.card {
		pointer-events: auto;
		width: 320px;
		box-sizing: border-box;
		padding: 20px 22px;
		border-radius: 28px;
		background: var(--color-surface);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-lg);
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	.title {
		font-family: var(--font-heading);
		font-size: 21px;
		line-height: 1.15;
	}
	.track {
		height: 8px;
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-text) 12%, transparent);
		overflow: hidden;
	}
	.fill {
		height: 100%;
		border-radius: 999px;
		background: var(--cold);
		transition: width 0.4s;
	}
	.row {
		display: flex;
		justify-content: space-between;
		gap: 8px;
		font-size: 13px;
		font-variant-numeric: tabular-nums;
	}
	b {
		white-space: nowrap;
	}
	.muted {
		color: var(--muted);
		white-space: nowrap;
	}
	.body {
		font-size: 12.5px;
		line-height: 1.45;
		color: var(--muted);
	}
</style>
