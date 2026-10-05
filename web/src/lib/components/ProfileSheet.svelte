<!--
  Mobile profile (Plan 7; design `:636-745`): a full-height surface sheet
  over the dimmed map with the grab handle (drag down to close), a 48 px
  "‹" back button and the "PLAYER" kicker, then ProfileContent at touch
  sizes. A modal dialog: focus is trapped and returns to the opener; Esc
  (the shell) closes it.
-->
<script lang="ts">
	import ChevronLeft from 'lucide-svelte/icons/chevron-left';
	import { focusTrap } from '$lib/actions/focusTrap';
	import BottomSheet from './BottomSheet.svelte';
	import ProfileContent from './ProfileContent.svelte';

	let {
		serverId,
		player,
		onclose,
		onmap
	}: {
		serverId: string;
		player: string;
		onclose: () => void;
		onmap: (x: number, z: number, id: string) => void;
	} = $props();
</script>

<BottomSheet snap="full" surface="surface" {onclose}>
	<div class="sheet" role="dialog" aria-modal="true" aria-label="Player profile" tabindex="-1" use:focusTrap={{ initial: '.back' }}>
		<div class="head" data-sheet-drag>
			<button class="back" type="button" aria-label="Back" onclick={onclose}>
				<ChevronLeft size={20} strokeWidth={2.75} />
			</button>
			<span class="kicker">Player</span>
		</div>
		<ProfileContent {serverId} {player} {onmap} mobile />
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
		gap: 6px;
		padding: 0 12px 4px 4px;
	}
	.back {
		width: 48px;
		height: 48px;
		display: grid;
		place-items: center;
		padding: 0;
		border: 0;
		border-radius: 50%;
		background: transparent;
		color: var(--color-text);
		cursor: pointer;
	}
	.kicker {
		font-size: 12px;
		font-weight: 700;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}
</style>
