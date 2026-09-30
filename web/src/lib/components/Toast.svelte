<!-- Toast (DESIGN-NOTES §3.20): auto-hides after 2600 ms via app.showToast. -->
<script lang="ts">
	import { app } from '$lib/state.svelte';
</script>

<div class="toast-region" aria-live="polite">
	{#if app.toast}
		<div class="toast">
			<b>{app.toast.title}</b>
			{#if app.toast.sub}<span>{app.toast.sub}</span>{/if}
		</div>
	{/if}
</div>

<style>
	.toast-region {
		position: fixed;
		left: 50%;
		bottom: 84px;
		transform: translateX(-50%);
		z-index: 1100;
		pointer-events: none;
	}
	.toast {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 2px;
		padding: 12px 20px;
		border-radius: 22px;
		background: var(--color-text);
		color: var(--color-bg);
		box-shadow: var(--shadow-lg);
		font-size: 14px;
		white-space: nowrap;
	}
	.toast span {
		font-size: 12px;
		opacity: 0.75;
		font-variant-numeric: tabular-nums;
	}
</style>
