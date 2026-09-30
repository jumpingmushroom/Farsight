<!-- The custom "home" icon (DESIGN-NOTES §4.3), for use beside Lucide components. -->
<script lang="ts">
	import MarkerIcon from '$lib/components/MarkerIcon.svelte';

	let { size = 24, color = 'currentColor' }: { size?: number; color?: string } = $props();
</script>

<MarkerIcon name="home" {size} {color} />
