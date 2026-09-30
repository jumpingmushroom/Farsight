<!--
  Page gate. Starts the app state, then shows: a blank "Loading…" screen until
  the first /api/servers call completes; the full-screen unlock dialog when no
  server is unlocked; otherwise the desktop shell (viewport ≥ 768 px) or the
  mobile shell. The unlock dialog also opens over
  the shell from "Add a server…" or a failed share link.
-->
<script lang="ts">
	import { onMount } from 'svelte';
	import DesktopShell from '$lib/components/DesktopShell.svelte';
	import MobileShell from '$lib/components/MobileShell.svelte';
	import UnlockDialog from '$lib/components/UnlockDialog.svelte';
	import { parseHash } from '$lib/share';
	import { app } from '$lib/state.svelte';

	const DESKTOP_QUERY = '(min-width: 768px)';

	let desktop = $state(matchMedia(DESKTOP_QUERY).matches);

	onMount(() => {
		const mq = matchMedia(DESKTOP_QUERY);
		desktop = mq.matches;
		const onchange = (e: MediaQueryListEvent) => (desktop = e.matches);
		mq.addEventListener('change', onchange);
		const stop = app.start();
		return () => {
			stop();
			mq.removeEventListener('change', onchange);
		};
	});

	const prefill = $derived(app.unlockPrompt?.server ?? parseHash(location.hash).server ?? '');
</script>

{#if !app.loaded}
	<div class="screen loading"><p>Loading…</p></div>
{:else if app.servers.length === 0}
	<div class="screen"></div>
	<UnlockDialog server={prefill} error={app.unlockPrompt?.error} canClose={false} />
{:else}
	{#if desktop}
		<DesktopShell />
	{:else}
		<MobileShell />
	{/if}
	{#if app.unlockPrompt}
		<UnlockDialog
			server={app.unlockPrompt.server ?? ''}
			error={app.unlockPrompt.error}
			canClose={true}
			returnTo={app.unlockPrompt.returnTo}
			onclose={() => app.closeUnlock()}
		/>
	{/if}
{/if}

<style>
	.screen {
		position: fixed;
		inset: 0;
		background: var(--color-bg);
	}
	.loading {
		display: grid;
		place-items: center;
		color: var(--muted);
	}
</style>
