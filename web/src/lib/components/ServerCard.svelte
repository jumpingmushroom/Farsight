<!--
  Server card (DESIGN-NOTES §3.3): the status row (plan status table), the
  server switcher, the Players / Version / Uptime stats and "How to join".
  While a switch loads (no card yet) it falls back to the server summary.
-->
<script lang="ts">
	import { isOfflineLike, sessionSeconds, statusView } from '$lib/derive';
	import { fmtUptime } from '$lib/format';
	import { app } from '$lib/state.svelte';
	import ServerSwitcher from './ServerSwitcher.svelte';

	let { onjoin }: { onjoin: () => void } = $props();

	const summary = $derived(app.servers.find((s) => s.id === app.currentId));
	const card = $derived(app.card?.id === app.currentId ? app.card : undefined);
	const status = $derived(card?.status ?? summary?.status ?? 'unknown');
	const view = $derived(statusView(status));
	const offline = $derived(isOfflineLike(status));
	const players = $derived(card?.players ?? summary?.players ?? 0);
	const max = $derived(card?.maxPlayers ?? summary?.maxPlayers ?? 0);
	const uptime = $derived(
		card?.upSince && !offline ? fmtUptime(sessionSeconds(card.upSince, app.now)) : '—'
	);
</script>

<section class="server-card" aria-label="Server">
	<div class="status">
		<span class="dot" class:ring={view.ring} style:background={view.dot}></span>
		<span class="label {view.tone}">{view.label}</span>
		<span class="live">Live status</span>
	</div>
	<ServerSwitcher name={card?.name ?? summary?.name ?? ''} />
	<dl class="stats">
		<div>
			<dt>Players</dt>
			<dd class="players">{offline ? '—' : players}<span>/{max}</span></dd>
		</div>
		<div>
			<dt>Version</dt>
			<dd class="small">{card?.version ?? '—'}</dd>
		</div>
		<div>
			<dt>Uptime</dt>
			<dd class="small">{uptime}</dd>
		</div>
	</dl>
	<button class="btn btn-primary btn-block" type="button" disabled={!card} onclick={onjoin}>How to join</button>
</section>

<style>
	.server-card {
		margin: 0 14px;
		padding: 16px;
		border-radius: 24px;
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	.status {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.dot {
		width: 10px;
		height: 10px;
		flex: none;
		border-radius: 50%;
	}
	.dot.ring {
		box-shadow: 0 0 0 4px color-mix(in srgb, var(--color-accent) 25%, transparent);
	}
	.label {
		font-size: 13px;
		font-weight: 700;
		color: var(--muted);
	}
	.label.accent {
		color: var(--color-accent-700);
	}
	.live {
		margin-left: auto;
		font-size: 12px;
		color: var(--muted);
	}
	.stats {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 8px;
		margin: 0;
	}
	dt {
		font-size: 11px;
		color: var(--muted);
		text-transform: uppercase;
		letter-spacing: 0.08em;
	}
	dd {
		margin: 0;
		font-weight: 700;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.players {
		font-size: 18px;
		font-variant-numeric: tabular-nums;
	}
	.players span {
		color: var(--muted);
		font-weight: 400;
	}
	.small {
		font-size: 15px;
		padding-top: 3px;
	}
	.btn-block {
		white-space: nowrap;
	}
</style>
