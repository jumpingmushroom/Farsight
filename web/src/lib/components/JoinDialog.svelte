<!--
  How to join, desktop modal (DESIGN-NOTES §3.19): backdrop (click closes),
  the dialog (role=dialog, aria-modal, labelled by its title) with the
  header — "Join {server}", the status line, close — over JoinContent.
  Esc closes; focus is trapped inside and returns to the opener.
-->
<script lang="ts">
	import X from 'lucide-svelte/icons/x';
	import { focusTrap } from '$lib/actions/focusTrap';
	import { countText, statusView } from '$lib/derive';
	import type { Card } from '$lib/types';
	import JoinContent from './JoinContent.svelte';

	let {
		card,
		onclose,
		oncopy
	}: { card: Card; onclose: () => void; oncopy: (title: string, sub: string) => void } = $props();

	const view = $derived(statusView(card.status));

	function onkeydown(e: KeyboardEvent): void {
		if (e.key === 'Escape') {
			e.preventDefault();
			onclose();
		}
	}

	function onbackdrop(e: MouseEvent): void {
		if (e.target === e.currentTarget) onclose();
	}
</script>

<svelte:window {onkeydown} />

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions (Esc closes via the window handler) -->
<div class="backdrop" onclick={onbackdrop}>
	<div class="join-dialog" role="dialog" aria-modal="true" aria-labelledby="join-title" tabindex="-1" use:focusTrap={{ initial: '.close' }}>
		<div class="head">
			<div class="title-block">
				<h2 class="title" id="join-title">Join {card.name}</h2>
				<div class="status">
					<span class="dot" style:background={view.dot}></span>{view.label} · {countText(
						card.players,
						card.maxPlayers,
						card.status
					)} · crossplay {card.crossplay ? 'on' : 'off'}
				</div>
			</div>
			<button class="btn btn-secondary btn-icon close" type="button" aria-label="Close" onclick={onclose}>
				<X size={16} strokeWidth={2.75} />
			</button>
		</div>
		<JoinContent {card} variant="desktop" {oncopy} />
	</div>
</div>

<style>
	.backdrop {
		position: fixed;
		inset: 0;
		z-index: 1000;
		background: rgba(10, 9, 8, 0.55);
		display: grid;
		place-items: center;
		padding: 16px;
	}
	.join-dialog {
		width: 540px;
		max-width: 100%;
		max-height: min(840px, calc(100vh - 32px));
		max-height: min(840px, calc(100dvh - 32px));
		overflow: auto;
		display: flex;
		flex-direction: column;
		gap: 16px;
		padding: 24px;
		border-radius: 32px;
		background: var(--color-surface);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-lg);
	}
	.head {
		display: flex;
		align-items: flex-start;
		gap: 12px;
	}
	.title-block {
		flex: 1;
		min-width: 0;
	}
	.title {
		margin: 0;
		font-size: 26px;
		line-height: 1.1;
		letter-spacing: normal;
	}
	.status {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-top: 4px;
		font-size: 13px;
		color: var(--muted);
	}
	.dot {
		width: 8px;
		height: 8px;
		flex: none;
		border-radius: 50%;
	}
	.close {
		flex: none;
	}
</style>
