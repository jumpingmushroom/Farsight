<!--
  An activity icon (Plan 7): Lucide log-in, log-out, power, map and
  triangle-alert, or a marker icon (skull, flame, home, portal, paw-print)
  from the shared table, all at stroke-width 2.75 in currentColor.
-->
<script lang="ts">
	import LogIn from 'lucide-svelte/icons/log-in';
	import LogOut from 'lucide-svelte/icons/log-out';
	import MapIcon from 'lucide-svelte/icons/map';
	import Power from 'lucide-svelte/icons/power';
	import TriangleAlert from 'lucide-svelte/icons/triangle-alert';
	import type { ActivityIcon } from '$lib/timeline';
	import MarkerIcon from './MarkerIcon.svelte';

	let { name, size = 14 }: { name: ActivityIcon; size?: number } = $props();

	const LUCIDE = { 'log-in': LogIn, 'log-out': LogOut, power: Power, map: MapIcon, alert: TriangleAlert };
</script>

{#if name === 'log-in' || name === 'log-out' || name === 'power' || name === 'map' || name === 'alert'}
	{@const Icon = LUCIDE[name]}
	<Icon {size} strokeWidth={2.75} />
{:else}
	<MarkerIcon {name} {size} />
{/if}
