<!--
  A player's profile (Plan 7; design `:116-185` desktop, `:636-745`
  mobile), shared by ProfilePanel (desktop) and ProfileSheet (mobile):
  avatar initial with the online dot, name and status; three stat tiles;
  the 7-day chart (hours online per local day, today in the accent);
  first seen, last seen, beds; Bases; Portals placed; Tames they named;
  Deaths; the tracked-since note and the design's source footnote.

  Fetching and refreshing (fix round 1) live in AppState
  (state.svelte.ts's syncProfile), on the same poll cadence as the card —
  not here — so a player going offline updates the status line without
  reopening, and switching the open view's player never flashes the wrong
  profile. `app.profile`/`app.profileFailed` are only ever for the one
  open view, so this just renders them: a skeleton while loading, "not
  found" for a player the server doesn't know, and a retry button when
  the last fetch failed. `serverId`/`player` double as a defensive check
  that the shown profile really is the one this instance was asked for.
  "Map →" calls `onmap` with the item's position and marker id.
-->
<script lang="ts">
	import { profileView } from '$lib/profile';
	import { app } from '$lib/state.svelte';
	import MarkerIcon from './MarkerIcon.svelte';

	let {
		serverId,
		player,
		mobile = false,
		onmap
	}: {
		serverId: string;
		player: string;
		mobile?: boolean;
		onmap: (x: number, z: number, id: string) => void;
	} = $props();

	const uid = $props.id();

	const matches = $derived(app.view?.kind === 'profile' && app.view.player === player && app.currentId === serverId);
	const profile = $derived(matches ? app.profile : undefined);
	const failed = $derived(matches && app.profileFailed);
	const v = $derived(profile ? profileView(profile, app.now) : undefined);
</script>

<div class="profile" class:mobile aria-busy={profile === undefined && !failed}>
	{#if failed}
		<p class="state">Couldn’t load this profile. Try again in a moment.</p>
		<button class="btn btn-secondary retry" type="button" onclick={() => app.retryProfile()}>Retry</button>
	{:else if profile === null}
		<p class="state">This player hasn’t been seen on this server.</p>
	{:else if !profile || !v}
		<div class="skeleton" data-testid="profile-skeleton">
			<span class="sk disc"></span>
			<span class="sk line"></span>
			<span class="sk block"></span>
			<span class="sk block tall"></span>
		</div>
	{:else}
		<div class="who">
			<span class="avatar" aria-hidden="true">{v.initial}<span class="dot" class:on={profile.online}></span></span>
			<div class="who-text">
				<h2 class="name" id="{uid}-name">{profile.name}</h2>
				<div class="status" class:on={profile.online}>{v.status}</div>
			</div>
		</div>

		<dl class="stats">
			{#each v.stats as s (s.k)}
				<div class="stat"><dt>{s.k}</dt><dd>{s.v}</dd></div>
			{/each}
		</dl>

		<section class="chart" aria-labelledby="{uid}-week">
			<div class="row-head"><h3 id="{uid}-week">Last 7 days</h3><span class="aside">hours online</span></div>
			<ol class="bars" aria-label="Hours online per day">
				{#each v.days as d, i (i)}
					<li class="bar-col" aria-label={d.aria}>
						<span class="bar-label">{d.label}</span>
						<span class="bar" class:today={d.today} style:height="{d.pct}%"></span>
					</li>
				{/each}
			</ol>
			<div class="bar-days" aria-hidden="true">
				{#each v.days as d, i (i)}<span class:today={d.today}>{d.d}</span>{/each}
			</div>
		</section>

		<dl class="facts">
			<dt>First seen</dt><dd>{v.first}</dd>
			<dt>Last seen</dt><dd>{v.last}</dd>
			<dt>Beds</dt><dd>{v.beds}</dd>
		</dl>
		<p class="note">{v.tracked}</p>

		<section aria-labelledby="{uid}-bases">
			<h3 id="{uid}-bases">Bases</h3>
			{#each v.bases as b (b.id)}
				<button class="item" type="button" onclick={() => onmap(b.x, b.z, b.id)}>
					<span class="base-disc" aria-hidden="true"><MarkerIcon name="home" size={15} color="#26231f" /></span>
					<span class="item-text"><b>{b.name}</b><span class="sub">{b.sub}</span></span>
					<span class="map">Map →</span>
				</button>
			{:else}
				<p class="none">No bases yet.</p>
			{/each}
		</section>

		<section aria-labelledby="{uid}-portals">
			<div class="row-head"><h3 id="{uid}-portals">Portals placed</h3><span class="aside">{v.portalCount}</span></div>
			{#if profile.portals.length}
				<div class="chips">
					{#each profile.portals as p (p.id)}
						<button class="portal" class:unpaired={!p.paired} type="button" onclick={() => onmap(p.x, p.z, p.id)}>
							<span class="portal-disc" aria-hidden="true"><MarkerIcon name="portal" size={11} color="#f5ead8" /></span>{p.tag || 'untagged'}
						</button>
					{/each}
				</div>
			{:else}
				<p class="none">None yet.</p>
			{/if}
		</section>

		<section aria-labelledby="{uid}-tames">
			<div class="row-head"><h3 id="{uid}-tames">Tames they named</h3><span class="aside">{v.tameCount}</span></div>
			{#if profile.tames.length}
				<div class="chips">
					{#each profile.tames as t (t.id)}<span class="tag tag-accent-2">{t.name} · {t.species}</span>{/each}
				</div>
			{:else}
				<p class="none">None yet.</p>
			{/if}
		</section>

		<section aria-labelledby="{uid}-deaths">
			<div class="row-head"><h3 id="{uid}-deaths">Deaths</h3><span class="aside">{v.deathLine}</span></div>
			{#each v.tombs as t (t.id)}
				<button class="item tomb" type="button" onclick={() => onmap(t.x, t.z, t.id)}>
					<span class="tomb-disc" aria-hidden="true"><MarkerIcon name="skull" size={14} color="#f5ead8" /></span>
					<span class="item-text tomb-text">{t.text}</span>
					<span class="map">Map →</span>
				</button>
			{/each}
		</section>

		<p class="foot">
			Playtime comes from the server log. Bases, beds, portals and tames come from who placed or named them in the world
			save. Deaths count tombstones seen in saves, so a death recovered between two saves is missed.
		</p>
	{/if}
</div>

<style>
	.profile {
		display: flex;
		flex-direction: column;
		gap: 18px;
		padding: 4px 18px 20px;
	}
	.profile.mobile {
		padding: 4px 16px var(--sheet-bottom, 20px);
	}
	.state {
		margin: 24px 0 8px;
		font-size: 14px;
		color: var(--muted);
	}
	.retry {
		align-self: flex-start;
	}
	.skeleton {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.sk {
		display: block;
		border-radius: 18px;
		background: color-mix(in srgb, var(--color-text) 8%, transparent);
	}
	.sk.disc {
		width: 60px;
		height: 60px;
		border-radius: 50%;
	}
	.sk.line {
		width: 60%;
		height: 22px;
	}
	.sk.block {
		height: 64px;
	}
	.sk.tall {
		height: 120px;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 14px;
	}
	.avatar {
		position: relative;
		width: 60px;
		height: 60px;
		flex: none;
		border-radius: 50%;
		background: var(--color-accent-200);
		color: var(--color-accent-800);
		display: grid;
		place-items: center;
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 26px;
	}
	.dot {
		position: absolute;
		right: 0;
		bottom: 0;
		width: 15px;
		height: 15px;
		box-sizing: border-box;
		border-radius: 50%;
		background: var(--color-neutral-400);
		border: 3px solid var(--color-surface);
	}
	.dot.on {
		background: var(--color-accent);
	}
	.who-text {
		flex: 1;
		min-width: 0;
	}
	.name {
		margin: 0;
		font-size: 26px;
		line-height: 1.1;
		letter-spacing: normal;
		overflow-wrap: anywhere;
	}
	.status {
		font-size: 13px;
		font-weight: 700;
		color: var(--muted);
	}
	.status.on {
		color: var(--color-accent-700);
	}
	.stats {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 8px;
		margin: 0;
	}
	.stat {
		padding: 10px;
		min-width: 0;
		border-radius: 18px;
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
	}
	.stat dt {
		font-size: 11.5px;
		color: var(--muted);
	}
	.stat dd {
		margin: 0;
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 17px;
		line-height: 1.15;
		white-space: nowrap;
	}
	section {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.chart {
		gap: 8px;
	}
	.row-head {
		display: flex;
		justify-content: space-between;
		align-items: baseline;
		gap: 8px;
	}
	h3 {
		margin: 0;
		font-size: 16px;
		line-height: 1.3;
		letter-spacing: normal;
	}
	.aside {
		font-size: 12px;
		color: var(--muted);
	}
	.bars {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(7, minmax(0, 1fr));
		gap: 6px;
		align-items: end;
		height: 92px;
	}
	.bar-col {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: flex-end;
		gap: 4px;
		height: 100%;
	}
	.bar-label {
		font-size: 10.5px;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}
	.bar {
		width: 100%;
		min-height: 3px;
		border-radius: 8px;
		background: var(--color-neutral-400);
	}
	.bar.today {
		background: var(--color-accent);
	}
	.bar-days {
		display: grid;
		grid-template-columns: repeat(7, minmax(0, 1fr));
		gap: 6px;
		font-size: 11px;
		text-align: center;
		color: var(--muted);
	}
	.bar-days .today {
		font-weight: 700;
		color: var(--color-text);
	}
	.facts {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		column-gap: 14px;
		row-gap: 5px;
		margin: 0;
		font-size: 13.5px;
	}
	.facts dt {
		color: var(--muted);
	}
	.facts dd {
		margin: 0;
		font-weight: 700;
	}
	.note,
	.foot {
		margin: -8px 0 0;
		font-size: 11.5px;
		line-height: 1.45;
		color: var(--muted);
	}
	.foot {
		margin: 0;
	}
	.none {
		margin: 0;
		font-size: 13px;
		color: var(--muted);
	}
	.item {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 48px;
		padding: 6px 10px;
		border: 0;
		border-radius: 16px;
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
		color: var(--color-text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.item:hover {
		background: color-mix(in srgb, var(--color-text) 10%, transparent);
	}
	.item-text {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		font-size: 14px;
	}
	.sub {
		font-size: 12px;
		color: var(--muted);
	}
	.map {
		font-size: 12px;
		font-weight: 700;
		color: var(--cold-ink);
		white-space: nowrap;
	}
	.base-disc,
	.tomb-disc {
		width: 30px;
		height: 30px;
		flex: none;
		box-sizing: border-box;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: #f5ead8;
		border: 2px solid #26231f;
	}
	.tomb {
		min-height: 44px;
		background: var(--color-accent-100);
		color: var(--color-accent-800);
	}
	.tomb:hover {
		background: var(--color-accent-200);
	}
	.tomb-disc {
		width: 28px;
		height: 28px;
		background: #c67139;
		border-color: #f5ead8;
	}
	.tomb-text {
		font-size: 13.5px;
		font-weight: 600;
	}
	.tomb .map {
		color: inherit;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.chips .tag {
		font-size: 13px;
	}
	.portal {
		display: flex;
		align-items: center;
		gap: 6px;
		height: 32px;
		padding: 0 12px 0 6px;
		border-radius: 999px;
		border: 1.5px solid var(--color-divider);
		background: transparent;
		color: var(--color-text);
		font: inherit;
		font-size: 13px;
		font-weight: 600;
		cursor: pointer;
	}
	.mobile .portal {
		height: 40px;
	}
	.portal.unpaired {
		border-color: var(--color-accent);
	}
	.portal-disc {
		width: 20px;
		height: 20px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: #3d7eab;
	}
</style>
