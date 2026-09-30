<!--
  No world save yet (the States ruling): the design's "Charting…" placeholder
  pill (DESIGN-NOTES §1.1 item 2) with "Waiting for the first world save…",
  centred in the map area right of the panel (`padLeft`).
-->
<script lang="ts">
	let { padLeft }: { padLeft: number } = $props();
</script>

<div class="area" style:left="{padLeft}px">
	<div class="pill" role="status" data-testid="waiting-pill">Waiting for the first world save…</div>
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
	.pill {
		padding: 18px 24px;
		border-radius: 28px;
		background: var(--glass);
		border: 1px solid var(--color-divider);
		font-size: 14px;
	}
</style>
