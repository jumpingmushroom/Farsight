<!--
  Mobile "How to join" sheet (DESIGN-NOTES §1.6, §3.19 mobile): a full-height
  surface sheet from `top: env(safe-area-inset-top) + 8px` over the dimmed
  map, with the grab handle (drag down to close) and a scroll area: the
  header ("Join {server}" at 24 px, the status line, a 48 px round close
  button), then JoinContent at touch sizes. A modal dialog: focus is
  trapped inside and returns to the opener; Esc (the shell) closes it.
-->
<script lang="ts">
	import X from 'lucide-svelte/icons/x';
	import { focusTrap } from '$lib/actions/focusTrap';
	import { countText, statusView } from '$lib/derive';
	import type { Card } from '$lib/types';
	import BottomSheet from './BottomSheet.svelte';
	import JoinContent from './JoinContent.svelte';

	let {
		card,
		onclose,
		oncopy
	}: { card: Card; onclose: () => void; oncopy: (title: string, sub: string) => void } = $props();

	const uid = $props.id();
	const view = $derived(statusView(card.status));
</script>

<BottomSheet snap="full" surface="surface" {onclose}>
	<div
		class="join"
		role="dialog"
		aria-modal="true"
		aria-labelledby="{uid}-title"
		tabindex="-1"
		use:focusTrap={{ initial: '.close' }}
	>
		<div class="head" data-sheet-drag>
			<div class="title-block">
				<h2 class="title" id="{uid}-title">Join {card.name}</h2>
				<div class="status">
					{view.label} · {countText(card.players, card.maxPlayers, card.status)} · crossplay {card.crossplay ? 'on' : 'off'}
				</div>
			</div>
			<button class="close" type="button" aria-label="Close" onclick={onclose}>
				<X size={18} strokeWidth={2.75} />
			</button>
		</div>
		<JoinContent {card} variant="mobile" {oncopy} />
	</div>
</BottomSheet>

<style>
	.join {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 10px 16px var(--sheet-bottom);
		outline: none;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 10px;
	}
	.title-block {
		flex: 1;
		min-width: 0;
	}
	.title {
		margin: 0;
		font-size: 24px;
		line-height: 1.1;
		letter-spacing: normal;
	}
	.status {
		font-size: 12.5px;
		color: var(--muted);
	}
	.close {
		width: 48px;
		height: 48px;
		flex: none;
		display: grid;
		place-items: center;
		padding: 0;
		border: 0;
		border-radius: 50%;
		background: color-mix(in srgb, var(--color-text) 8%, transparent);
		color: var(--color-text);
		cursor: pointer;
	}
</style>
