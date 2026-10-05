<!--
  The activity timeline (Plan 7; design `:837-908`), shared by
  ActivityPanel (desktop) and ActivitySheet (mobile): who was on today on a
  00–24 axis in the server's zone; the eight category chips with counts
  and the people chips (filters shared with the side panel's short list);
  the events grouped by day, newest first; "Show earlier" (three more
  days) until tracking began; and the "where each entry comes from"
  footnote. `days` reports how many days are loaded, for the header.

  Fix round 1: fetching and refreshing live in AppState (state.svelte.ts's
  syncActivity), on the same poll cadence as the card — not here — so a
  session ending, a new event or "today" rolling over at local midnight
  land without reopening, the same pattern as the profile (ProfileContent).
  `app.activityPages`/`app.today`/`app.activityFailed` are only ever for
  the one open view, so this just renders them (a retry button when the
  initial fetch failed) and asks AppState to load an earlier page or retry.

  Server review: the server only collapses a run of adjacent autosaves
  within one page, so a page loaded by "Show earlier" can still have its
  newest autosave sit right next to the previous page's oldest one;
  `collapseAutosaves` (timeline.ts) merges that seam once the pages are
  flattened, the same rule the server applies inside a page.
-->
<script lang="ts">
	import { filters } from '$lib/filters.svelte';
	import { app } from '$lib/state.svelte';
	import {
		CATEGORIES,
		chipCounts,
		collapseAutosaves,
		eventIcon,
		eventText,
		eventTone,
		groupByDay,
		passes,
		sourceText,
		todayRowLabel,
		todayRows
	} from '$lib/timeline';
	import { zClock, zDayMonth } from '$lib/zoned';
	import ActivityIcon from './ActivityIcon.svelte';

	let {
		serverId,
		gameDay,
		mobile = false,
		days = $bindable(3),
		onmap
	}: {
		serverId: string;
		gameDay?: number;
		mobile?: boolean;
		days?: number;
		onmap: (x: number, z: number) => void;
	} = $props();

	const uid = $props.id();

	// A defensive check (as ProfileContent's serverId/player): the shown
	// data really is for this instance's server, not a stale one from a
	// fetch that resolved after a switch (AppState already guards this
	// itself, but a component re-render could still land one tick late).
	const matches = $derived(app.currentId === serverId && app.view?.kind === 'activity');
	const pages = $derived(matches ? app.activityPages : []);
	const today = $derived(matches ? app.today : undefined);
	const failed = $derived(matches && app.activityFailed);
	const loadingMore = $derived(matches && app.activityLoadingMore);

	const events = $derived(collapseAutosaves(pages.flatMap((p) => p.events)));
	const tz = $derived(pages[0]?.timeZone ?? 'UTC');
	const people = $derived(pages[0]?.people ?? []);
	const oldest = $derived(pages.at(-1));
	const more = $derived(!!oldest?.earliest && new Date(oldest.from).getTime() > new Date(oldest.earliest).getTime());
	const counts = $derived(chipCounts(events, filters.people));
	const groups = $derived(groupByDay(events.filter((e) => passes(e, filters.off, filters.people)), tz, app.now, gameDay));
	const whoToday = $derived(today ? todayRows(today, app.now, filters.people) : undefined);

	$effect(() => {
		days = Math.max(1, pages.length) * 3;
	});
</script>

<div class="activity" class:mobile>
	<div class="filters">
		<h3 class="label" id="{uid}-today">Who was on today</h3>
		<div class="today" role="group" aria-labelledby="{uid}-today">
			{#if whoToday && whoToday.rows.length}
				<ul class="today-rows">
					{#each whoToday.rows as r (r.id + r.name)}
						<li class="today-row" aria-label={todayRowLabel(r)}>
							<span class="today-name" aria-hidden="true">{r.name}</span>
							<span class="track" aria-hidden="true">
								{#each r.bars as b, i (i)}
									<span class="span" class:live={b.live} title={b.title} style:left="{b.left}%" style:width="{b.width}%"></span>
								{/each}
								<span class="now" style:left="{whoToday.nowPct}%"></span>
							</span>
						</li>
					{/each}
				</ul>
				<div class="axis" aria-hidden="true"><span>00</span><span>06</span><span>12</span><span>18</span><span>24</span></div>
				<div class="legend">
					<span><i class="key live"></i>Online now</span>
					<span><i class="key"></i>Earlier session</span>
					<span><i class="key now-key"></i>Now · {whoToday.nowClock}</span>
				</div>
			{:else if today}
				<p class="muted">Nobody has played today yet.</p>
			{/if}
		</div>

		<h3 class="label" id="{uid}-show">Show</h3>
		<div class="chips" role="group" aria-labelledby="{uid}-show">
			{#each CATEGORIES as c (c.key)}
				{@const on = !filters.off.includes(c.key)}
				<button class="chip" class:on type="button" aria-pressed={on} onclick={() => filters.toggle(c.key)}>
					<span class="chip-icon"><ActivityIcon name={c.icon} size={14} /></span>{c.label}<span class="count">{counts[c.key]}</span>
				</button>
			{/each}
		</div>
		<div class="chips" role="group" aria-label="People">
			<button class="person" class:on={filters.people.length === 0} type="button" aria-pressed={filters.people.length === 0} onclick={() => filters.everyone()}>
				<span class="initial">★</span>Everyone
			</button>
			{#each people as p (p.id)}
				{@const on = filters.people.includes(p.id)}
				<button class="person" class:on type="button" aria-pressed={on} onclick={() => filters.togglePerson(p.id)}>
					<span class="initial">{Array.from(p.name)[0]?.toUpperCase() ?? '?'}</span>{p.name}
				</button>
			{/each}
		</div>
	</div>

	<div class="list">
		{#if failed}
			<p class="end">Couldn’t load the activity. Try again in a moment.</p>
			<button class="btn btn-secondary retry" type="button" onclick={() => app.retryActivity()}>Retry</button>
		{:else if pages.length === 0}
			<p class="end" aria-busy="true">Loading…</p>
		{:else}
			{#each groups as g (g.key)}
				<section class="day" aria-label={g.label}>
					<h4 class="day-head"><span class="day-label">{g.label}</span><span class="day-sub">{g.sub}</span></h4>
					<ol class="entries">
						{#each g.items as e (e.id)}
							<li class="entry">
								<time class="clock" datetime={e.at}>{zClock(e.at, tz)}</time>
								<span class="rail"><span class="disc {eventTone(e)}" aria-hidden="true"><ActivityIcon name={eventIcon(e)} /></span><span class="line"></span></span>
								<span class="body">
									<span class="text">{eventText(e)}</span>
									<span class="src">
										<span class="src-dot" class:save={e.source === 'save'}></span>{sourceText(e, tz)}
										{#if e.x !== undefined && e.z !== undefined}
											{@const x = e.x}
											{@const z = e.z}
											<button class="on-map" type="button" onclick={() => onmap(x, z)}>Show on map →</button>
										{/if}
									</span>
								</span>
							</li>
						{/each}
					</ol>
				</section>
			{/each}
			{#if events.length > 0 && groups.length === 0}
				<p class="end">Nothing matches these filters.</p>
			{:else if events.length === 0}
				<p class="end">Nothing happened in these days.</p>
			{/if}
			{#if more}
				<button class="btn btn-secondary earlier" type="button" disabled={loadingMore} onclick={() => app.loadEarlierActivity()}>Show earlier</button>
			{:else if oldest?.earliest}
				<p class="end">Tracking began {zDayMonth(oldest.earliest, tz)}</p>
			{/if}
		{/if}
		<div class="foot">
			<b>Where each entry comes from.</b>
			<span><i class="src-dot"></i><b>Server log</b> (live, exact time): joins, leaves, restarts, saves and raids.</span>
			<span
				><i class="src-dot save"></i><b>World save</b> (spotted by comparing two saves): tombstones, new portals, tames, base growth
				and bosses. The time is the save it first appeared in, never the exact moment.</span
			>
			<span>“Show on map” only appears for places in explored areas.</span>
		</div>
	</div>
</div>

<style>
	.activity {
		display: flex;
		flex-direction: column;
	}
	.filters {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 6px 20px 16px;
	}
	.mobile .filters {
		gap: 12px;
		padding: 4px 16px 14px;
	}
	.label {
		margin: 0;
		font-family: var(--font-body);
		font-size: 12px;
		font-weight: 700;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}
	/* Fidelity (design `:850-875`): the mobile sheet has no section labels.
	   Kept in the DOM (visually hidden, not display:none) since each is
	   still the accessible name for its group via aria-labelledby. */
	.mobile .label {
		position: absolute;
		width: 1px;
		height: 1px;
		margin: -1px;
		padding: 0;
		overflow: hidden;
		clip: rect(0, 0, 0, 0);
		white-space: nowrap;
		border: 0;
	}
	.muted {
		margin: 0;
		font-size: 13px;
		color: var(--muted);
	}
	.today {
		display: flex;
		flex-direction: column;
		gap: 7px;
	}
	.today-rows {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 7px;
	}
	.today-row {
		display: grid;
		grid-template-columns: 52px minmax(0, 1fr);
		gap: 8px;
		align-items: center;
	}
	.today-name {
		font-size: 12.5px;
		font-weight: 700;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.track {
		position: relative;
		height: 14px;
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
	}
	.span {
		position: absolute;
		top: 0;
		bottom: 0;
		min-width: 4px;
		border-radius: 999px;
		background: var(--color-neutral-400);
	}
	.span.live {
		background: var(--color-accent);
	}
	.now {
		position: absolute;
		top: -3px;
		bottom: -3px;
		width: 2px;
		background: var(--cold);
	}
	.axis {
		display: flex;
		justify-content: space-between;
		padding-left: 60px;
		font-size: 11px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}
	.legend {
		display: flex;
		gap: 14px;
		flex-wrap: wrap;
		font-size: 12px;
		color: var(--muted);
	}
	.legend span {
		display: flex;
		align-items: center;
		gap: 6px;
	}
	.key {
		width: 14px;
		height: 8px;
		border-radius: 999px;
		background: var(--color-neutral-400);
	}
	.key.live {
		background: var(--color-accent);
	}
	.key.now-key {
		width: 2px;
		height: 12px;
		border-radius: 0;
		background: var(--cold);
	}
	.chips {
		display: flex;
		gap: 6px;
		flex-wrap: wrap;
	}
	.mobile .chips {
		flex-wrap: nowrap;
		overflow-x: auto;
		scrollbar-width: none;
		margin-right: -16px;
		padding-right: 16px;
	}
	.chip {
		flex: none;
		display: flex;
		align-items: center;
		gap: 6px;
		height: 32px;
		padding: 0 12px 0 8px;
		border-radius: 999px;
		border: 1.5px solid var(--color-divider);
		background: transparent;
		color: var(--muted);
		font: inherit;
		font-size: 13px;
		font-weight: 600;
		cursor: pointer;
	}
	.chip.on {
		border-color: var(--cold);
		background: color-mix(in srgb, var(--cold) 16%, transparent);
		color: var(--color-text);
	}
	.chip-icon {
		display: grid;
		opacity: 0.6;
	}
	.chip.on .chip-icon {
		opacity: 1;
	}
	.count {
		font-size: 11.5px;
		opacity: 0.7;
		font-variant-numeric: tabular-nums;
	}
	.person {
		flex: none;
		display: flex;
		align-items: center;
		gap: 6px;
		height: 30px;
		padding: 0 12px 0 4px;
		border: 0;
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-text) 7%, transparent);
		color: var(--color-text);
		font: inherit;
		font-size: 13px;
		font-weight: 600;
		cursor: pointer;
	}
	.mobile .chip,
	.mobile .person {
		height: 40px;
	}
	.initial {
		width: 22px;
		height: 22px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: var(--color-accent-200);
		color: var(--color-accent-800);
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 11px;
	}
	.person.on {
		background: var(--color-text);
		color: var(--color-bg);
	}
	.person.on .initial {
		background: var(--color-bg);
		color: var(--color-text);
	}
	.list {
		border-top: 1px solid var(--color-divider);
		padding-bottom: 16px;
	}
	.mobile .list {
		padding-bottom: var(--sheet-bottom, 30px);
	}
	.day-head {
		position: sticky;
		top: 0;
		z-index: 1;
		display: flex;
		align-items: baseline;
		gap: 8px;
		margin: 0;
		padding: 10px 20px 8px;
		background: var(--color-surface);
		font-size: 16px;
		line-height: 1.3;
		letter-spacing: normal;
	}
	.mobile .day-head {
		padding: 10px 12px 8px;
	}
	.day-sub {
		font-family: var(--font-body);
		font-size: 12px;
		color: var(--muted);
	}
	.entries {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.entry {
		display: grid;
		grid-template-columns: 44px 30px minmax(0, 1fr);
		column-gap: 10px;
		padding: 0 20px;
	}
	.mobile .entry {
		padding: 0 12px;
	}
	.clock {
		font-size: 12.5px;
		font-variant-numeric: tabular-nums;
		color: var(--muted);
		padding-top: 7px;
		text-align: right;
	}
	.rail {
		display: flex;
		flex-direction: column;
		align-items: center;
	}
	.disc {
		width: 30px;
		height: 30px;
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
	.line {
		flex: 1;
		width: 2px;
		min-height: 10px;
		background: var(--color-divider);
	}
	.body {
		display: flex;
		flex-direction: column;
		padding: 5px 0 14px;
		min-width: 0;
	}
	.text {
		font-size: 14px;
		line-height: 1.35;
		text-wrap: pretty;
	}
	.src {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
		margin-top: 3px;
		font-size: 12px;
		color: var(--muted);
	}
	.src-dot {
		display: inline-block;
		width: 6px;
		height: 6px;
		margin-right: -3px;
		border-radius: 50%;
		background: var(--color-accent);
	}
	.src-dot.save {
		background: var(--cold);
	}
	.on-map {
		position: relative;
		border: 0;
		background: transparent;
		padding: 0;
		font: inherit;
		font-size: 12px;
		font-weight: 700;
		color: var(--cold-ink);
		cursor: pointer;
	}
	/* A ≥44 px touch target (fix round 1) without stretching the source
	   line's own box — the sibling text/dot stay at their natural height,
	   an invisible overlay just enlarges the hit area. */
	.mobile .on-map::after {
		content: '';
		position: absolute;
		inset: -14px -8px;
	}
	.end {
		margin: 0;
		padding: 28px 20px;
		font-size: 14px;
		color: var(--muted);
		text-align: center;
	}
	.earlier,
	.retry {
		display: block;
		margin: 16px auto;
		font-family: var(--font-body);
		font-weight: 700;
	}
	.foot {
		display: flex;
		flex-direction: column;
		gap: 6px;
		padding: 12px 20px 0;
		font-size: 11.5px;
		line-height: 1.45;
		color: var(--muted);
	}
	.foot span {
		display: block;
	}
	.foot .src-dot {
		margin-right: 6px;
	}
</style>
