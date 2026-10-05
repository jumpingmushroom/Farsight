<!--
  Mobile Activity view (Plan 7; design `:842-904`): a full-height surface
  sheet with the grab handle (drag down to close), a 48 px back button,
  "Activity" and "Last N days · log + saves", over ActivityContent at touch
  sizes. A modal dialog: focus is trapped and returns to the opener; Esc
  (the shell) closes it.

  Fix round 1: this is opened from "Full timeline" inside the menu sheet,
  which unmounts as it closes (unlike a profile's row, which stays mounted
  under the players sheet) — so there is nothing left at mount time for
  focusTrap's own capture to return focus to. `returnTo` (the shell's
  captured menu-button opener) is passed straight through to focusTrap,
  which already prefers it over its own capture.
-->
<script lang="ts">
	import ChevronLeft from 'lucide-svelte/icons/chevron-left';
	import { focusTrap, type ReturnTo } from '$lib/actions/focusTrap';
	import ActivityContent from './ActivityContent.svelte';
	import BottomSheet from './BottomSheet.svelte';

	let {
		serverId,
		gameDay,
		returnTo,
		onclose,
		onmap
	}: {
		serverId: string;
		gameDay?: number;
		returnTo?: ReturnTo;
		onclose: () => void;
		onmap: (x: number, z: number) => void;
	} = $props();

	let days = $state(3);
</script>

<BottomSheet snap="full" surface="surface" {onclose}>
	<div class="sheet" role="dialog" aria-modal="true" aria-label="Activity" tabindex="-1" use:focusTrap={{ initial: '.back', returnTo }}>
		<div class="head" data-sheet-drag>
			<button class="back" type="button" aria-label="Back" onclick={onclose}>
				<ChevronLeft size={20} strokeWidth={2.75} />
			</button>
			<div class="title-block">
				<h2 class="title">Activity</h2>
				<div class="sub">Last {days} days · log + saves</div>
			</div>
		</div>
		<ActivityContent {serverId} {gameDay} {onmap} mobile bind:days />
	</div>
</BottomSheet>

<style>
	.sheet {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		display: flex;
		flex-direction: column;
		outline: none;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 0 12px 8px 8px;
	}
	.back {
		width: 48px;
		height: 48px;
		flex: none;
		display: grid;
		place-items: center;
		padding: 0;
		border: 0;
		border-radius: 50%;
		background: transparent;
		color: var(--color-text);
		cursor: pointer;
	}
	.title-block {
		flex: 1;
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
</style>
