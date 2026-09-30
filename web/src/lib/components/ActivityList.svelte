<!--
  Recent activity (DESIGN-NOTES §3.8 and the plan ruling): log events only,
  the newest autosave row kept, up to `limit` rows (8 desktop, 3 mobile).
  No "Full timeline →" and rows are not clickable in the MVP. Times derive
  from `now`, so they tick. `mobile` is the pulled-sheet look (§3.18 item 5:
  32 px discs, 14.5 px text, 48 px rows, an 18 px heading).
-->
<script lang="ts">
	import LogIn from 'lucide-svelte/icons/log-in';
	import LogOut from 'lucide-svelte/icons/log-out';
	import MapIcon from 'lucide-svelte/icons/map';
	import Power from 'lucide-svelte/icons/power';
	import { activityRows, type ActivityRow } from '$lib/derive';
	import { fmtActivityTime } from '$lib/format';
	import type { Card } from '$lib/types';

	let {
		card,
		now,
		limit = 8,
		mobile = false
	}: { card: Card; now: Date; limit?: number; mobile?: boolean } = $props();

	const uid = $props.id();

	const rows = $derived(activityRows(card, limit));
	const ICONS = { 'log-in': LogIn, 'log-out': LogOut, power: Power, map: MapIcon } satisfies Record<
		ActivityRow['icon'],
		unknown
	>;
</script>

<section class="activity" class:mobile aria-labelledby="{uid}-activity-title">
	<h3 class="head" id="{uid}-activity-title">Recent activity</h3>
	{#if rows.length === 0}
		<p class="empty">No server activity yet.</p>
	{:else}
		<ul>
			{#each rows as a, i (i)}
				{@const Icon = ICONS[a.icon]}
				<li class="row">
					<span class="disc {a.tone}" aria-hidden="true"><Icon size={mobile ? 15 : 14} strokeWidth={2.75} /></span>
					<span class="text">{a.text}</span>
					<time class="time" datetime={a.at}>{fmtActivityTime(a.at, now)}</time>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<style>
	.head {
		margin: 0;
		padding: 18px 6px 6px;
		font-size: 17px;
		line-height: 1.55;
		letter-spacing: normal;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.row {
		display: flex;
		align-items: flex-start;
		gap: 10px;
		padding: 7px 8px;
		border-radius: 14px;
	}
	.row:hover {
		background: color-mix(in srgb, var(--color-text) 5%, transparent);
	}
	.disc {
		width: 28px;
		height: 28px;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
	}
	.disc.ember {
		background: var(--color-accent-100);
		color: var(--color-accent-600);
	}
	.disc.neutral {
		background: color-mix(in srgb, var(--color-text) 8%, transparent);
		color: var(--color-text);
	}
	.text {
		flex: 1;
		min-width: 0;
		font-size: 14px;
		line-height: 1.35;
		padding-top: 4px;
	}
	.time {
		font-size: 12px;
		color: var(--muted);
		white-space: nowrap;
		padding-top: 5px;
	}
	.mobile .head {
		padding: 14px 4px 4px;
		font-size: 18px;
	}
	.mobile .row {
		align-items: center;
		gap: 12px;
		min-height: 48px;
		padding: 0 4px;
	}
	.mobile .disc {
		width: 32px;
		height: 32px;
	}
	.mobile .text {
		font-size: 14.5px;
		line-height: 1.3;
		padding-top: 0;
	}
	.mobile .time {
		padding-top: 0;
	}
	.empty {
		margin: 0;
		padding: 4px 8px;
		font-size: 13px;
		color: var(--muted);
	}
</style>
