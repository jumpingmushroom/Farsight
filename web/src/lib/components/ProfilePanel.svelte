<!--
  Desktop profile (Plan 7; design `:116-185`): replaces the side panel's
  content in the same 344 px box, opaque surface, with a back arrow (to the
  server card) and the "PLAYER" kicker over the scrolling ProfileContent.
-->
<script lang="ts">
	import ChevronLeft from 'lucide-svelte/icons/chevron-left';
	import ProfileContent from './ProfileContent.svelte';

	let {
		serverId,
		player,
		onback,
		onmap
	}: {
		serverId: string;
		player: string;
		onback: () => void;
		onmap: (x: number, z: number, id: string) => void;
	} = $props();
</script>

<aside class="panel" aria-label="Player profile" data-testid="profile-panel">
	<div class="head">
		<button class="btn btn-secondary btn-icon" type="button" aria-label="Back" onclick={onback}>
			<ChevronLeft size={17} strokeWidth={2.75} />
		</button>
		<span class="kicker">Player</span>
	</div>
	<div class="body">
		<ProfileContent {serverId} {player} {onmap} />
	</div>
</aside>

<style>
	.panel {
		position: absolute;
		left: 16px;
		top: 16px;
		bottom: 16px;
		width: 344px;
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
		padding: 14px 14px 8px;
	}
	.kicker {
		font-size: 12px;
		font-weight: 700;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.body {
		flex: 1;
		min-height: 0;
		overflow: auto;
	}
</style>
