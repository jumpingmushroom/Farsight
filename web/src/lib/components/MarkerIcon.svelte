<!--
  A marker icon as real SVG (DESIGN-NOTES §4.3), drawn from the same table as
  the pin HTML (`$lib/icons/paths`): Lucide geometry at stroke-width 2.75, or
  the custom portal, arch and legacy home shapes. Colour defaults to
  currentColor.
-->
<script lang="ts">
	import { ICON_PATHS, STROKE_WIDTH, type IconName } from '$lib/icons/paths';

	let {
		name,
		size = 24,
		color = 'currentColor'
	}: {
		name: IconName;
		size?: number;
		color?: string;
	} = $props();

	const inner = $derived(ICON_PATHS[name].join(''));
</script>

<svg
	xmlns="http://www.w3.org/2000/svg"
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke={color}
	stroke-width={STROKE_WIDTH}
	stroke-linecap="round"
	stroke-linejoin="round"
	aria-hidden="true"
	class="marker-icon"
	data-icon={name}>{@html inner}</svg
>

<style>
	.marker-icon {
		display: block;
		flex: none;
	}
</style>
