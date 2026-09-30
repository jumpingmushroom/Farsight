<!-- Theme toggle (DESIGN-NOTES §2.4): sun while dark, moon while light. -->
<script lang="ts">
	import Moon from 'lucide-svelte/icons/moon';
	import Sun from 'lucide-svelte/icons/sun';
	import { app } from '$lib/state.svelte';
</script>

<button
	class="btn btn-secondary btn-icon"
	type="button"
	aria-label="Toggle theme"
	onclick={() => app.setTheme(app.theme === 'dark' ? 'light' : 'dark')}
>
	{#if app.theme === 'dark'}
		<Sun size={17} strokeWidth={2.75} />
	{:else}
		<Moon size={17} strokeWidth={2.75} />
	{/if}
</button>
