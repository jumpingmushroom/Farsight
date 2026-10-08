<!--
  Recent activity (DESIGN-NOTES §3.8, Plan 7): the card's activity through
  the timeline's filters (shared with the Activity view), the newest
  autosave row kept, up to `limit` rows (8 desktop, 3 mobile). Log rows
  show how long ago; world-save rows "save HH:MM" in the server's zone.
  With `full`, the heading carries "Full timeline →", which opens the
  Activity view. `mobile` is the pulled-sheet look (§3.18 item 5: 32 px
  discs, 14.5 px text, 48 px rows, an 18 px heading).
-->
<script lang="ts">
	import { activityRows } from '$lib/derive';
	import { filters } from '$lib/filters.svelte';
	import { fmtActivityTime } from '$lib/format';
	import { app } from '$lib/state.svelte';
	import type { Card } from '$lib/types';
	import { cardZone, zClock } from '$lib/zoned';
	import ActivityIcon from './ActivityIcon.svelte';

	let {
		card,
		now,
		limit = 8,
		mobile = false,
		full = false
	}: { card: Card; now: Date; limit?: number; mobile?: boolean; full?: boolean } = $props();

	const uid = $props.id();

	const rows = $derived(activityRows(card, limit, filters.off, filters.people));
	const tz = $derived(cardZone(card));
</script>

<section class="activity" class:mobile aria-labelledby="{uid}-activity-title">
	<div class="head">
		<h3 class="title" id="{uid}-activity-title">Recent activity</h3>
		{#if full}
			<button class="btn btn-ghost full" type="button" onclick={() => app.openView({ kind: 'activity' })}>Full timeline →</button>
		{/if}
	</div>
	{#if rows.length === 0}
		<p class="empty">{filters.active ? 'Nothing matches these filters.' : 'No server activity yet.'}</p>
	{:else}
		<ul>
			{#each rows as a, i (i)}
				<li class="row">
					<span class="disc {a.tone}" aria-hidden="true"><ActivityIcon name={a.icon} size={mobile ? 15 : 14} /></span>
					<span class="text">{a.text}</span>
					<time class="time" datetime={a.at}>{a.source === 'save' ? `save ${zClock(a.at, tz)}` : fmtActivityTime(a.at, now, tz)}</time>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<style>
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		padding: 18px 6px 6px;
	}
	.title {
		margin: 0;
		font-size: 17px;
		line-height: 1.55;
		letter-spacing: normal;
	}
	.full {
		font-size: 13px;
		font-weight: 700;
		color: var(--cold-ink);
		white-space: nowrap;
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
	.disc.sage {
		background: var(--color-accent-2-100);
		color: var(--color-accent-2-700);
	}
	.disc.cold {
		background: color-mix(in srgb, var(--cold) 22%, transparent);
		color: var(--cold);
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
	}
	.mobile .title {
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
