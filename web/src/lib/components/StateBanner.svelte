<!--
  The map-state banner (DESIGN-NOTES §3.22 and the States ruling), in the
  map-updated pill's slot: offline (text-coloured, power icon), stale,
  "Can’t draw this world’s map yet" and "The map isn’t drawn yet"
  (accent-100, triangle-alert icon).
  Position comes from the shell (`left`, `right`, `top`). The `mobile`
  variant is compact and unpositioned: MobileShell places it under the top
  bar.
-->
<script lang="ts">
	import Power from 'lucide-svelte/icons/power';
	import TriangleAlert from 'lucide-svelte/icons/triangle-alert';
	import type { BannerTone } from '$lib/derive';
	// Static per-tone announcements: the visible title changes with the clock, so it isn't live.
	import { BANNER_SHORT as ANNOUNCE } from '$lib/mobile';

	let {
		tone,
		title,
		body,
		left,
		right,
		top = 16,
		mobile = false,
		height = $bindable(0)
	}: {
		tone: BannerTone;
		title: string;
		body: string;
		left?: number;
		right?: number;
		top?: number;
		mobile?: boolean;
		/** The rendered height, for the time pill under it. */
		height?: number;
	} = $props();
</script>

<div
	class="banner {tone}"
	class:mobile
	style:left={mobile ? undefined : `${left ?? 16}px`}
	style:right={mobile ? undefined : `${right ?? 16}px`}
	style:top={mobile ? undefined : `${top}px`}
	data-testid="state-banner"
	bind:clientHeight={height}
>
	<span class="sr-only" role="status">{ANNOUNCE[tone]}</span>
	<span class="icon" aria-hidden="true">
		{#if tone === 'offline'}<Power size={mobile ? 18 : 20} strokeWidth={2.75} />{:else}<TriangleAlert size={mobile ? 18 : 20} strokeWidth={2.75} />{/if}
	</span>
	<div class="text">
		<b>{title}</b>
		<span class="body">{body}</span>
	</div>
</div>

<style>
	.banner {
		position: absolute;
		display: flex;
		align-items: flex-start;
		gap: 12px;
		padding: 12px 16px;
		border-radius: 22px;
		box-shadow: var(--shadow-md);
		background: var(--color-accent-100);
		color: var(--color-accent-800);
	}
	.banner.mobile {
		position: static;
		gap: 10px;
		padding: 10px 14px;
		border-radius: 20px;
	}
	.mobile b {
		font-size: 13.5px;
		line-height: 1.3;
	}
	.mobile .body {
		font-size: 12px;
		line-height: 1.35;
	}
	.banner.offline {
		background: var(--color-text);
		color: var(--color-bg);
	}
	.icon {
		display: grid;
		flex: none;
		margin-top: 1px;
	}
	.text {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	b {
		font-size: 14px;
	}
	.body {
		font-size: 12.5px;
		line-height: 1.4;
		opacity: 0.9;
	}
</style>
