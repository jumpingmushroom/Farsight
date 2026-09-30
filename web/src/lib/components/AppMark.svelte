<!--
  App mark (DESIGN-NOTES §3.1): the accent brand disc with the app initial.
  38 px / 20 px in the panel, 32 / 17 in the collapsed pill, 40 / 20 on the
  mobile top bar.
-->
<script lang="ts">
	let { size = 38, font = 20, name = 'Farsight' }: { size?: number; font?: number; name?: string } = $props();
	const initial = $derived(Array.from(name)[0]?.toUpperCase() ?? '');
</script>

<span class="mark" aria-hidden="true" style:width="{size}px" style:height="{size}px" style:font-size="{font}px"
	>{initial}</span
>

<style>
	.mark {
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: var(--color-accent);
		color: var(--color-bg);
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		line-height: 1;
	}
</style>
