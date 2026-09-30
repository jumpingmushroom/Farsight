<!--
  The docked marker card on mobile (DESIGN-NOTES §1.2 frame 4 and the Mobile
  ruling): MarkerCard at `left/right 12px; bottom: 28px + safe-area`, with a
  close button (the ruling adds it). It scrolls when taller than the space
  under the top bar.
-->
<script lang="ts">
	import type { CardModel } from '$lib/markers';
	import MarkerCard from './MarkerCard.svelte';

	let { m, onclose, onjump }: { m: CardModel; onclose: () => void; onjump: (id: string) => void } = $props();
</script>

<div class="dock" data-testid="mobile-marker-card">
	<MarkerCard {m} {onclose} {onjump} />
</div>

<style>
	.dock {
		position: absolute;
		left: 12px;
		right: 12px;
		bottom: calc(28px + env(safe-area-inset-bottom));
		z-index: 35;
		max-height: calc(100% - env(safe-area-inset-top) - env(safe-area-inset-bottom) - 110px);
		overflow-y: auto;
		overscroll-behavior: contain;
		border-radius: 28px;
	}
</style>
