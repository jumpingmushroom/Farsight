<!--
  Mobile top bar (DESIGN-NOTES §3.17, §1.2, §1.7): a glass pill at
  `top: env(safe-area-inset-top) + 8px` with the 40 px app mark, the server
  name (tap: the server sheet) over the sub-line (mobile.ts `topBarSub`: map
  age and next save, or a compact state text), and the 48 px menu button.

  - Menu open: the menu button turns cold with an x in the bg colour.
  - Switcher open: a cold border, chevron-up, "Choose a server", no menu button.
  - `flat` drops the shadow (the design's pulled, menu and switcher frames).
-->
<script lang="ts">
	import ChevronDown from 'lucide-svelte/icons/chevron-down';
	import ChevronUp from 'lucide-svelte/icons/chevron-up';
	import Menu from 'lucide-svelte/icons/menu';
	import X from 'lucide-svelte/icons/x';
	import type { TopBarSub } from '$lib/mobile';
	import AppMark from './AppMark.svelte';

	let {
		name,
		sub,
		menuOpen,
		switcherOpen,
		flat = false,
		onname,
		onmenu
	}: {
		name: string;
		sub: TopBarSub;
		menuOpen: boolean;
		switcherOpen: boolean;
		flat?: boolean;
		onname: () => void;
		onmenu: () => void;
	} = $props();
</script>

<header class="bar" class:flat class:switcher={switcherOpen} data-testid="mobile-top-bar">
	<AppMark size={40} font={20} />
	<button
		class="title"
		type="button"
		data-server-button
		aria-haspopup="dialog"
		aria-expanded={switcherOpen}
		aria-label="Switch server: {name}"
		onclick={onname}
	>
		<span class="name-row">
			<span class="name">{name}</span>
			{#if switcherOpen}<ChevronUp size={14} strokeWidth={2.75} />{:else}<ChevronDown size={14} strokeWidth={2.75} />{/if}
		</span>
		<span class="sub" data-testid="top-bar-sub">
			{#if sub.tone !== 'none'}<span class="dot {sub.tone}"></span>{/if}<span class="sub-text">{sub.text}</span>
		</span>
	</button>
	{#if !switcherOpen}
		<button
			class="menu"
			class:open={menuOpen}
			type="button"
			aria-label={menuOpen ? 'Close menu' : 'Search and layers'}
			aria-haspopup="dialog"
			aria-expanded={menuOpen}
			onclick={onmenu}
		>
			{#if menuOpen}<X size={20} strokeWidth={2.75} />{:else}<Menu size={20} strokeWidth={2.75} />{/if}
		</button>
	{/if}
</header>

<style>
	.bar {
		position: absolute;
		left: 12px;
		right: 12px;
		top: calc(env(safe-area-inset-top) + 8px);
		z-index: 40;
		box-sizing: border-box;
		min-height: 62px;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 6px 6px 8px;
		border-radius: 999px;
		background: var(--glass);
		backdrop-filter: blur(14px);
		-webkit-backdrop-filter: blur(14px);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-md);
		color: var(--color-text);
	}
	.bar.flat {
		box-shadow: none;
	}
	.bar.switcher {
		border-color: var(--cold);
	}
	.title {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		align-items: stretch;
		min-height: 48px;
		justify-content: center;
		padding: 0;
		border: 0;
		background: transparent;
		color: inherit;
		font: inherit;
		text-align: left;
		cursor: pointer;
		border-radius: 12px;
	}
	.name-row {
		display: flex;
		align-items: center;
		gap: 4px;
		min-width: 0;
	}
	.name-row :global(svg) {
		flex: none;
	}
	.name {
		min-width: 0;
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 17px;
		line-height: 1.1;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.sub {
		display: flex;
		align-items: center;
		gap: 6px;
		min-width: 0;
		font-size: 12px;
		line-height: 1.55;
		color: var(--muted);
	}
	.sub-text {
		min-width: 0;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		font-variant-numeric: tabular-nums;
	}
	.dot {
		width: 7px;
		height: 7px;
		flex: none;
		border-radius: 50%;
		background: var(--cold);
	}
	.dot.offline {
		background: var(--color-neutral-500);
	}
	.dot.warn {
		background: var(--color-accent);
	}
	.menu {
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
	.menu.open {
		background: var(--cold);
		color: var(--color-bg);
	}
</style>
