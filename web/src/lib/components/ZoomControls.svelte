<!--
  Zoom controls (DESIGN-NOTES §3.15): a glass column pill of three 46 px
  buttons (zoom in, zoom out, reset view) at the bottom right. The `mobile`
  variant (§1.2 frame 1) is a column of three separate 52 px glass circles,
  `bottom` px from the bottom (the shell keeps it 47 px above the peek sheet).
-->
<script lang="ts">
	import Minus from 'lucide-svelte/icons/minus';
	import Plus from 'lucide-svelte/icons/plus';
	import RotateCcw from 'lucide-svelte/icons/rotate-ccw';

	let {
		onzoomin,
		onzoomout,
		onreset,
		mobile = false,
		bottom
	}: {
		onzoomin: () => void;
		onzoomout: () => void;
		onreset: () => void;
		mobile?: boolean;
		bottom?: number;
	} = $props();

	const size = $derived(mobile ? 20 : 18);
</script>

<div class="zoom" class:mobile role="group" aria-label="Map zoom" style:bottom={bottom === undefined ? undefined : `${bottom}px`}>
	<button class="btn btn-icon" type="button" aria-label="Zoom in" title="Zoom in" onclick={onzoomin}>
		<Plus {size} strokeWidth={2.75} />
	</button>
	<button class="btn btn-icon" type="button" aria-label="Zoom out" title="Zoom out" onclick={onzoomout}>
		<Minus {size} strokeWidth={2.75} />
	</button>
	<button class="btn btn-icon" type="button" aria-label="Reset view" title="Reset view" onclick={onreset}>
		<RotateCcw {size} strokeWidth={2.75} />
	</button>
</div>

<style>
	.zoom {
		position: absolute;
		right: 16px;
		bottom: 16px;
		display: flex;
		flex-direction: column;
		border-radius: 999px;
		background: var(--glass);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-md);
		overflow: hidden;
	}
	.zoom .btn {
		width: 46px;
		height: 46px;
		border-radius: 0;
		color: var(--color-text);
	}
	.zoom .btn + .btn {
		border-top: 1px solid var(--color-divider);
	}
	.zoom .btn:hover {
		background: color-mix(in srgb, var(--color-text) 8%, transparent);
	}
	.zoom.mobile {
		right: 14px;
		bottom: 208px;
		gap: 10px;
		border-radius: 0;
		background: none;
		border: 0;
		box-shadow: none;
		overflow: visible;
	}
	.zoom.mobile .btn {
		width: 52px;
		height: 52px;
		border-radius: 50%;
		background: var(--glass);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-md);
	}
	.zoom.mobile .btn + .btn {
		border-top: 1px solid var(--color-divider);
	}
</style>
