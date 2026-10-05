<!--
  Desktop Activity view (Plan 7; design `:61-113`): a 520 px opaque aside in
  the side panel's place, with a back arrow, "Activity", the "Last N days ·
  from the server log and world saves" line and "Reset filters", over the
  scrolling ActivityContent.

  Fix round 1 parallel (adaptation; the brief predates it): like
  ProfilePanel, this is inline in the shell, not a modal, so opening it
  unmounts the SidePanel (and the "Full timeline →" row) underneath it —
  on mount it moves focus to its own Back button, matching ProfilePanel and
  the mobile sheet's focusTrap. DesktopShell restores focus once this
  unmounts again.
-->
<script lang="ts">
	import ChevronLeft from 'lucide-svelte/icons/chevron-left';
	import { onMount } from 'svelte';
	import { filters } from '$lib/filters.svelte';
	import ActivityContent from './ActivityContent.svelte';

	let {
		serverId,
		gameDay,
		onback,
		onmap
	}: {
		serverId: string;
		gameDay?: number;
		onback: () => void;
		onmap: (x: number, z: number) => void;
	} = $props();

	let days = $state(3);
	let backBtn: HTMLButtonElement | undefined;
	onMount(() => backBtn?.focus());
</script>

<aside class="panel" aria-label="Activity" data-testid="activity-panel">
	<div class="head">
		<button bind:this={backBtn} class="btn btn-secondary btn-icon" type="button" aria-label="Back" onclick={onback}>
			<ChevronLeft size={17} strokeWidth={2.75} />
		</button>
		<div class="title-block">
			<h2 class="title">Activity</h2>
			<div class="sub">Last {days} days · from the server log and world saves</div>
		</div>
		<button class="btn btn-ghost reset" type="button" onclick={() => filters.reset()}>Reset filters</button>
	</div>
	<div class="body">
		<ActivityContent {serverId} {gameDay} {onmap} bind:days />
	</div>
</aside>

<style>
	.panel {
		position: absolute;
		left: 16px;
		top: 16px;
		bottom: 16px;
		width: 520px;
		max-width: calc(100vw - 32px);
		display: flex;
		flex-direction: column;
		border-radius: 30px;
		background: var(--color-surface);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-lg);
		overflow: hidden;
		z-index: 550;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 16px 16px 10px 12px;
	}
	.title-block {
		flex: 1;
		min-width: 0;
	}
	.title {
		margin: 0;
		font-size: 22px;
		line-height: 1.1;
		letter-spacing: normal;
	}
	.sub {
		font-size: 12px;
		color: var(--muted);
	}
	.reset {
		flex: none;
		font-family: var(--font-body);
		font-size: 13px;
		font-weight: 700;
		white-space: nowrap;
	}
	.body {
		flex: 1;
		min-height: 0;
		overflow: auto;
	}
</style>
