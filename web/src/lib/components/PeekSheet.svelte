<!--
  The mobile players sheet (DESIGN-NOTES §1.2 frames 1–2, §3.18 and the
  Mobile ruling), in a BottomSheet that drags between peek and pulled.

  - Peek (glass): "Online now", the "4 / 10" pill and the Join pill, then a
    horizontal scroller of player chips (avatar, name, session).
  - Pulled (surface, from top 176): "Online now" with the pill, the
    "{server} · live · positions aren’t tracked" line, then a scrolling body:
    the online rows (44 px avatars), "Recently online" (ruling), and three
    recent-activity rows. No "Full timeline →" (§1.5) and no per-player
    base counts ([GAP] §3.18 item 4: needs name matching; omitted).
  - Offline or nobody online: the §3.22 empty state replaces the online
    list (peek shows its title in place of the chips).
-->
<script lang="ts">
	import Moon from 'lucide-svelte/icons/moon';
	import { countText, playersEmpty, recentList, sessionSeconds } from '$lib/derive';
	import { fmtLastSeen, fmtSession } from '$lib/format';
	import type { Snap } from '$lib/mobile';
	import type { Card } from '$lib/types';
	import ActivityList from './ActivityList.svelte';
	import Avatar from './Avatar.svelte';
	import BottomSheet from './BottomSheet.svelte';

	let {
		card,
		now,
		snap,
		onsnap,
		onjoin,
		hidden = false,
		peekHeight = $bindable(0)
	}: {
		card: Card | undefined;
		now: Date;
		snap: Snap;
		onsnap: (s: Snap) => void;
		onjoin: () => void;
		hidden?: boolean;
		peekHeight?: number;
	} = $props();

	const uid = $props.id();
	const empty = $derived(card ? playersEmpty(card, now) : undefined);
	const recent = $derived(card ? recentList(card) : []);
	const count = $derived(card ? countText(card.players, card.maxPlayers, card.status, ' / ') : '');
	const live = $derived(card?.status === 'online' ? 'live' : (card?.status ?? ''));
</script>

<BottomSheet {snap} {onsnap} {hidden} surface={snap === 'peek' ? 'glass' : 'surface'} bind:peekHeight>
	<section class="players {snap}" aria-labelledby="{uid}-title">
		<div class="head" data-sheet-drag>
			<h2 class="title" id="{uid}-title">Online now</h2>
			{#if card}<span class="count" data-testid="online-count">{count}</span>{/if}
			{#if snap === 'peek'}
				<button class="join" type="button" disabled={!card} onclick={onjoin}>Join</button>
			{/if}
		</div>
		{#if snap === 'peek'}
			{#if card && !empty}
				<ul class="chips" aria-label="Online players">
					{#each card.online as p (p.platform + p.platformId + p.name)}
						<li class="chip">
							<Avatar name={p.name} online size={32} dot={false} />
							<span class="chip-text"
								><b>{p.name}</b><span class="chip-sub">{fmtSession(sessionSeconds(p.since, now))}</span></span
							>
						</li>
					{/each}
				</ul>
			{:else}
				<p class="peek-empty">{empty?.title ?? ''}</p>
			{/if}
		{:else}
			{#if card}
				<p class="sub" data-sheet-drag>{card.name} · {live} · positions aren’t tracked</p>
			{/if}
			<div class="body">
				{#if card}
					{#if empty}
						<div class="empty">
							<span class="empty-disc" aria-hidden="true"><Moon size={24} strokeWidth={2.75} /></span>
							<h3 class="empty-title">{empty.title}</h3>
							<p class="empty-body">{empty.body}</p>
						</div>
					{:else}
						<ul aria-label="Online now">
							{#each card.online as p (p.platform + p.platformId + p.name)}
								<li class="row">
									<Avatar name={p.name} online size={44} />
									<div class="who">
										<div class="name">{p.name}</div>
										<div class="row-sub">online {fmtSession(sessionSeconds(p.since, now))}</div>
									</div>
								</li>
							{/each}
						</ul>
					{/if}
					{#if recent.length > 0}
						<h3 class="label" id="{uid}-recent">Recently online</h3>
						<ul aria-labelledby="{uid}-recent">
							{#each recent as r (r.name)}
								<li class="row">
									<Avatar name={r.name} size={44} />
									<div class="who">
										<div class="name">{r.name}</div>
										<div class="row-sub">{fmtLastSeen(r.until, now)}</div>
									</div>
								</li>
							{/each}
						</ul>
					{/if}
					<ActivityList {card} {now} limit={3} mobile />
				{/if}
			</div>
		{/if}
	</section>
</BottomSheet>

<style>
	.players {
		display: flex;
		flex-direction: column;
		min-height: 0;
		flex: 1;
		padding: 0 16px var(--sheet-bottom);
	}
	.players.peek {
		gap: 14px;
		padding-top: 14px;
	}
	.players.pulled {
		gap: 6px;
		padding: 14px 16px 0;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 10px;
		cursor: grab;
	}
	.pulled .head {
		padding: 0 4px;
	}
	.title {
		margin: 0;
		font-size: 21px;
		line-height: 1.55;
		letter-spacing: normal;
	}
	.pulled .title {
		font-size: 22px;
	}
	.count {
		height: 28px;
		padding: 0 11px;
		border-radius: 999px;
		background: var(--color-accent);
		color: var(--color-bg);
		font-weight: 700;
		font-size: 14px;
		display: grid;
		place-items: center;
		white-space: nowrap;
		font-variant-numeric: tabular-nums;
	}
	.join {
		margin-left: auto;
		height: 44px;
		padding: 0 16px;
		border: 0;
		border-radius: 999px;
		background: var(--color-accent);
		color: var(--color-bg);
		font: inherit;
		font-weight: 700;
		font-size: 14px;
		cursor: pointer;
	}
	.join:disabled {
		opacity: 0.5;
		cursor: default;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.chips {
		display: flex;
		gap: 8px;
		overflow-x: auto;
		scrollbar-width: none;
		margin-right: -16px;
		padding-right: 16px;
	}
	.chips::-webkit-scrollbar {
		display: none;
	}
	.chip {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 12px 6px 6px;
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-text) 7%, transparent);
		flex: none;
	}
	.chip-text {
		display: flex;
		flex-direction: column;
		line-height: 1.15;
	}
	.chip-text b {
		font-size: 14px;
	}
	.chip-sub {
		font-size: 11.5px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}
	.peek-empty {
		margin: 0;
		min-height: 44px;
		display: flex;
		align-items: center;
		font-size: 14px;
		color: var(--muted);
	}
	.sub {
		margin: 0;
		font-size: 12.5px;
		color: var(--muted);
		padding: 0 4px 6px;
	}
	.body {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		margin: 0 -16px;
		padding: 0 16px var(--sheet-bottom);
	}
	.row {
		display: flex;
		align-items: center;
		gap: 12px;
		min-height: 60px;
		padding: 0 4px;
	}
	.who {
		flex: 1;
		min-width: 0;
	}
	.name {
		font-weight: 700;
		font-size: 16px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.row-sub {
		font-size: 13.5px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}
	.label {
		margin: 0;
		padding: 14px 4px 4px;
		font-size: 18px;
		line-height: 1.55;
		letter-spacing: normal;
	}
	.empty {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 10px;
		padding: 12px 4px 4px;
	}
	.empty-disc {
		width: 56px;
		height: 56px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: var(--color-accent-2-100);
		color: var(--color-accent-2-700);
	}
	.empty-title {
		margin: 0;
		font-size: 20px;
		line-height: 1.15;
		letter-spacing: normal;
	}
	.empty-body {
		margin: 0;
		font-size: 13px;
		line-height: 1.45;
		color: var(--muted);
		text-wrap: pretty;
	}
</style>
