<script lang="ts">
	import type { Snippet } from 'svelte';
	import 'leaflet/dist/leaflet.css';
	import '../styles/organic.css';
	import '../styles/app.css';
	import Toast from '$lib/components/Toast.svelte';

	let { children }: { children: Snippet } = $props();
</script>

{@render children()}
<Toast />
