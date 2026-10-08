<!--
  "Reconnecting…" (review fix): shown while `app.disconnected`, i.e. once
  refreshes have failed for RECONNECT_AFTER_MS, until the next one
  succeeds. Styled like StateBanner's offline tone (text-coloured, wifi-off
  icon) and placed in the same slot, which it takes over: a server state
  read before contact was lost is no longer news. Position comes from the
  shell (`left`, `right`, `top`); the `mobile` variant is compact and
  unpositioned, under the top bar.
-->
<script lang="ts">
	import WifiOff from 'lucide-svelte/icons/wifi-off';

	let {
		left,
		right,
		top = 16,
		mobile = false,
		height = $bindable(0)
	}: {
		left?: number;
		right?: number;
		top?: number;
		mobile?: boolean;
		/** The rendered height, for the time pill under it. */
		height?: number;
	} = $props();
</script>

<div
	class="banner"
	class:mobile
	style:left={mobile ? undefined : `${left ?? 16}px`}
	style:right={mobile ? undefined : `${right ?? 16}px`}
	style:top={mobile ? undefined : `${top}px`}
	data-testid="connection-banner"
	bind:clientHeight={height}
>
	<span class="sr-only" role="status">Lost contact with Farsight. Reconnecting.</span>
	<span class="icon" aria-hidden="true"><WifiOff size={mobile ? 18 : 20} strokeWidth={2.75} /></span>
	<div class="text">
		<b>Reconnecting…</b>
		<span class="body">Can’t reach Farsight right now. What’s shown may be out of date until it answers.</span>
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
		background: var(--color-text);
		color: var(--color-bg);
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
