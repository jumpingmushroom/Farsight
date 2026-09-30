<!--
  Player avatar (DESIGN-NOTES §3.7): a heading-font initial on a disc. Online
  players get the ember disc and the online dot; recent players the neutral
  disc and no dot. `dot={false}` drops the dot (the mobile peek chips).
-->
<script lang="ts">
	let {
		name,
		online = false,
		size = 40,
		dot = true
	}: { name: string; online?: boolean; size?: number; dot?: boolean } = $props();
	const initial = $derived(Array.from(name.trim())[0]?.toUpperCase() ?? '?');
</script>

<span
	class="avatar"
	class:online
	aria-hidden="true"
	style:width="{size}px"
	style:height="{size}px"
	style:font-size="{Math.round(size * 0.45)}px"
	>{initial}{#if online && dot}<span class="dot"></span>{/if}</span
>

<style>
	.avatar {
		position: relative;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: var(--color-neutral-200);
		color: var(--color-neutral-800);
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		line-height: 1;
	}
	.avatar.online {
		background: var(--color-accent-200);
		color: var(--color-accent-800);
	}
	.dot {
		position: absolute;
		right: -1px;
		bottom: -1px;
		width: 13px;
		height: 13px;
		box-sizing: border-box;
		border-radius: 50%;
		background: var(--color-accent);
		border: 2.5px solid var(--color-surface);
	}
</style>
