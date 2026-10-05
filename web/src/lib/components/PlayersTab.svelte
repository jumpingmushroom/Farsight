<!--
  Players tab (DESIGN-NOTES §3.7, §3.8, §3.22): the intro, who's online now
  (API order, longest session first), "Recently online" (deduped, capped at
  5, label hidden when empty), then recent activity. Offline and nobody-
  online replace the intro and online list with the §3.22 empty states.
  Each row with a platform ID has "Profile →" (Plan 7), which opens the
  player's profile in place of the panel. Relative times derive from `now`.
-->
<script lang="ts">
	import Moon from 'lucide-svelte/icons/moon';
	import { playersEmpty, recentList, sessionSeconds } from '$lib/derive';
	import { fmtLastSeen, fmtSession } from '$lib/format';
	import { app } from '$lib/state.svelte';
	import type { Card } from '$lib/types';
	import ActivityList from './ActivityList.svelte';
	import Avatar from './Avatar.svelte';

	let { card, now }: { card: Card; now: Date } = $props();

	const empty = $derived(playersEmpty(card, now));
	const recent = $derived(recentList(card));
	const uid = $props.id();
</script>

<div class="players">
	{#if empty}
		<div class="empty">
			<span class="empty-disc" aria-hidden="true"><Moon size={24} strokeWidth={2.75} /></span>
			<h3 class="empty-title">{empty.title}</h3>
			<p class="empty-body">{empty.body}</p>
		</div>
	{:else}
		<p class="intro">Live from the server. Player positions aren’t tracked.</p>
		<ul aria-label="Online now">
			{#each card.online as p (p.platform + p.platformId + p.name)}
				<li class="row">
					<Avatar name={p.name} online />
					<div class="who">
						<div class="name">{p.name}</div>
						<div class="sub">online {fmtSession(sessionSeconds(p.since, now))}</div>
					</div>
					{#if p.platformId}
						<button class="btn btn-ghost profile" type="button" aria-label="Profile of {p.name}" onclick={() => app.openView({ kind: 'profile', player: p.platformId }, p.name)}>Profile →</button>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
	{#if recent.length > 0}
		<div class="label" id="{uid}-recent-label">Recently online</div>
		<ul aria-labelledby="{uid}-recent-label">
			{#each recent as r (r.name)}
				<li class="row">
					<Avatar name={r.name} />
					<div class="who">
						<div class="name">{r.name}</div>
						<div class="sub">{fmtLastSeen(r.until, now)}</div>
					</div>
					{#if r.platformId}
						<button class="btn btn-ghost profile" type="button" aria-label="Profile of {r.name}" onclick={() => app.openView({ kind: 'profile', player: r.platformId }, r.name)}>Profile →</button>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
	<ActivityList {card} {now} limit={8} />
</div>

<style>
	.players {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.intro {
		margin: 0;
		font-size: 12px;
		color: var(--muted);
		padding: 4px 6px 8px;
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
		align-items: center;
		gap: 12px;
		padding: 8px;
		border-radius: 18px;
	}
	.row:hover {
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
	}
	.who {
		flex: 1;
		min-width: 0;
	}
	.name {
		font-weight: 700;
		font-size: 15px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.sub {
		font-size: 13px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}
	.profile {
		flex: none;
		font-family: var(--font-body);
		font-size: 12px;
		font-weight: 700;
		color: var(--cold-ink);
		white-space: nowrap;
	}
	.label {
		font-size: 12px;
		color: var(--muted);
		padding: 12px 6px 4px;
	}
	.empty {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 10px;
		padding: 18px 10px 10px;
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
