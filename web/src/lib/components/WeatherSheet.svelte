<!--
  Mobile time-and-weather sheet (spec "Pill": the chip opens the same
  content in a bottom sheet): an auto-height surface sheet titled "Time and
  weather" over WeatherContent (whose first line is "Day {d} · {clock}"). Not
  aria-modal, like the server sheet: the chip under the top bar stays
  reachable; Esc and the scrim (the shell) and a drag down close it.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import type { TimeView } from '$lib/worldtime';
	import BottomSheet from './BottomSheet.svelte';
	import WeatherContent from './WeatherContent.svelte';

	let { view, onclose }: { view: TimeView; onclose: () => void } = $props();

	const uid = $props.id();
	let root: HTMLDivElement;

	onMount(() => root.focus({ preventScroll: true }));
</script>

<BottomSheet snap="auto" surface="surface" {onclose}>
	<div class="weather" role="dialog" aria-labelledby="{uid}-title" tabindex="-1" bind:this={root}>
		<h2 class="title" id="{uid}-title" data-sheet-drag>Time and weather</h2>
		<WeatherContent {view} />
	</div>
</BottomSheet>

<style>
	.weather {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 16px 16px var(--sheet-bottom);
		outline: none;
	}
	.title {
		margin: 0;
		padding: 0 4px;
		font-size: 22px;
		line-height: 1.3;
		letter-spacing: normal;
	}
</style>
