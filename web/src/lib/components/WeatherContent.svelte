<!--
  The time-and-weather body (design `:315-334`, spec "Dropdown"), shared by
  the desktop dropdown (WeatherPill) and the mobile sheet (WeatherSheet):
  "Day {d} · {clock}" with the next text, the day-progress bar (the
  design's gradient, a 4 px marker at the raw day fraction, the 00/06/12/
  18/24 ticks placed where the rescaled clock reaches those hours), the
  paused notice, the Biome | Now | Next | Then table of explored biomes
  with legend swatches, and the footer. All copy comes from worldtime.ts
  `timeView`.
-->
<script lang="ts">
	import type { TimeView } from '$lib/worldtime';

	let { view }: { view: TimeView } = $props();

	/** Raw day fraction (%) where the rescaled clock reads 00, 06, 12, 18 and 24. */
	const TICKS: [string, number][] = [
		['00', 0],
		['06', 15],
		['12', 50],
		['18', 85],
		['24', 100]
	];
</script>

<div class="content">
	<div>
		<div class="head"><b>Day {view.day} · {view.clock}</b><span class="muted">{view.next}</span></div>
		<div class="bar" aria-hidden="true">
			<div class="marker" style:left="{view.pct}%"></div>
		</div>
		<div class="ticks" aria-hidden="true">
			{#each TICKS as [text, at] (text)}
				<span class:first={at === 0} class:last={at === 100} style:left="{at}%">{text}</span>
			{/each}
		</div>
	</div>
	{#if view.pausedText}
		<div class="paused">{view.pausedText}</div>
	{/if}
	{#if view.rows.length > 0}
		<div class="table" role="table" aria-label="Weather by biome">
			<div class="grid th" role="row">
				<span role="columnheader">Biome</span>
				<span role="columnheader">Now</span>
				<span role="columnheader">Next<small>{view.heads.next}</small></span>
				<span role="columnheader">Then<small>{view.heads.then}</small></span>
			</div>
			{#each view.rows as r (r.name)}
				<div class="grid tr" role="row">
					<span class="biome" role="cell"
						><span class="swatch" style:background={r.hex}></span><span class="name">{r.name}</span></span
					>
					<b role="cell">{r.now}</b>
					<span class="muted" role="cell">{r.next}</span>
					<span class="muted" role="cell">{r.then}</span>
				</div>
			{/each}
		</div>
	{/if}
	<div class="foot">{view.footer}</div>
</div>

<style>
	.content {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.head {
		display: flex;
		justify-content: space-between;
		gap: 8px;
		font-size: 13px;
	}
	.muted {
		color: var(--muted);
	}
	.bar {
		position: relative;
		height: 10px;
		margin-top: 8px;
		border-radius: 999px;
		background: linear-gradient(
			90deg,
			#2b3752 0%,
			#2b3752 19%,
			#c98a5a 24%,
			#e6d6a4 31%,
			#e6d6a4 70%,
			#c98a5a 77%,
			#2b3752 83%,
			#2b3752 100%
		);
	}
	.marker {
		position: absolute;
		top: -4px;
		width: 4px;
		height: 18px;
		margin-left: -2px;
		border-radius: 2px;
		background: var(--color-text);
		box-shadow: 0 0 0 2px var(--color-surface);
	}
	.ticks {
		position: relative;
		height: 1.4em;
		margin-top: 4px;
		font-size: 11px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}
	.ticks span {
		position: absolute;
		top: 0;
		transform: translateX(-50%);
	}
	.ticks span.first {
		transform: none;
	}
	.ticks span.last {
		transform: translateX(-100%);
	}
	.paused {
		font-size: 13px;
		font-weight: 700;
	}
	.table {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.grid {
		display: grid;
		grid-template-columns: minmax(0, 1.3fr) repeat(3, minmax(0, 1fr));
		gap: 6px;
	}
	.th {
		align-items: end;
		font-size: 11px;
		color: var(--muted);
		text-transform: uppercase;
		letter-spacing: 0.06em;
		font-weight: 700;
		padding: 0 0 4px;
	}
	.th small {
		display: block;
		font-size: 11px;
		font-weight: 400;
		letter-spacing: normal;
		text-transform: none;
		white-space: nowrap;
	}
	.tr {
		align-items: center;
		padding: 6px 0;
		border-top: 1px solid var(--color-divider);
		font-size: 13px;
	}
	.biome {
		display: flex;
		align-items: center;
		gap: 7px;
		min-width: 0;
	}
	.swatch {
		width: 11px;
		height: 11px;
		flex: none;
		border-radius: 50%;
		border: 1px solid rgba(0, 0, 0, 0.2);
	}
	.name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.foot {
		font-size: 11.5px;
		line-height: 1.45;
		color: var(--muted);
	}
</style>
