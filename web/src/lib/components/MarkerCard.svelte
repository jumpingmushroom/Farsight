<!--
  MarkerCard (design/MarkerCard.dc.html, DESIGN-NOTES §3.16): the disc,
  kicker and title header with an optional close button, then badges, facts,
  note, "Jump to partner →" and the where line. Content comes from
  `buildMarkers` (§4.2). Used at 312 px in the desktop popover.
-->
<script lang="ts">
	import X from 'lucide-svelte/icons/x';
	import MarkerIcon from './MarkerIcon.svelte';
	import type { CardModel } from '$lib/markers';

	let {
		m,
		onclose,
		onjump
	}: {
		m: CardModel;
		onclose?: () => void;
		onjump?: (id: string) => void;
	} = $props();
</script>

<div class="marker-card" data-testid="marker-card">
	<div class="head">
		<div class="disc" style:background={m.discBg}>
			<MarkerIcon name={m.icon} size={21} color={m.iconColor} />
		</div>
		<div class="text">
			<div class="kicker">{m.kicker}</div>
			<div class="title">{m.title}</div>
		</div>
		{#if onclose}
			<button class="btn btn-secondary btn-icon close" type="button" aria-label="Close" onclick={() => onclose?.()}>
				<X size={16} strokeWidth={2.75} />
			</button>
		{/if}
	</div>
	{#if m.badges.length > 0}
		<div class="badges">
			{#each m.badges as b (b.text)}
				<span class="tag badge tone-{b.tone}">{b.text}</span>
			{/each}
		</div>
	{/if}
	{#if m.facts.length > 0}
		<div class="facts">
			{#each m.facts as f (f.k)}
				<div class="k">{f.k}</div>
				<div class="v">{f.v}</div>
			{/each}
		</div>
	{/if}
	{#if m.note}
		<p class="note">{m.note}</p>
	{/if}
	{#if m.partnerId}
		<div class="jump">
			<button class="btn btn-primary" type="button" onclick={() => m.partnerId && onjump?.(m.partnerId)}
				>Jump to partner →</button
			>
		</div>
	{/if}
	<div class="where">{m.where}</div>
</div>

<style>
	.marker-card {
		width: 100%;
		box-sizing: border-box;
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 18px 18px 16px;
		border-radius: 28px;
		background: var(--color-surface);
		color: var(--color-text);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-lg);
		font-family: var(--font-body);
	}
	.head {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	.disc {
		width: 44px;
		height: 44px;
		flex: none;
		box-sizing: border-box;
		border-radius: 50%;
		display: grid;
		place-items: center;
		border: 2px solid #f5ead8;
		box-shadow: 0 2px 6px rgba(0, 0, 0, 0.25);
	}
	.text {
		flex: 1;
		min-width: 0;
	}
	.kicker {
		font-size: 11px;
		letter-spacing: 0.1em;
		text-transform: uppercase;
		font-weight: 700;
		color: var(--cold-ink, #1d4a69);
	}
	.title {
		font-family: var(--font-heading);
		font-size: 21px;
		line-height: 1.15;
		overflow-wrap: anywhere;
	}
	.close {
		width: 34px;
		height: 34px;
		flex: none;
		align-self: flex-start;
	}
	.badges {
		display: flex;
		gap: 6px;
		flex-wrap: wrap;
	}
	.badge {
		font-weight: 700;
		font-size: 12px;
	}
	.tone-cold {
		background: color-mix(in srgb, var(--cold) 22%, transparent);
		color: var(--cold-ink);
	}
	.tone-ember {
		background: var(--color-accent-100);
		color: var(--color-accent-800);
	}
	.tone-sage {
		background: var(--color-accent-2-100);
		color: var(--color-accent-2-800);
	}
	.tone-neutral {
		background: var(--color-neutral-100);
		color: var(--color-neutral-800);
	}
	.facts {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		column-gap: 16px;
		row-gap: 6px;
		font-size: 14px;
	}
	.k {
		color: var(--muted, rgba(32, 30, 29, 0.62));
	}
	.v {
		font-weight: 600;
		font-variant-numeric: tabular-nums;
		overflow-wrap: anywhere;
	}
	.note {
		margin: 0;
		font-size: 13.5px;
		line-height: 1.5;
		text-wrap: pretty;
		opacity: 0.88;
	}
	.jump {
		display: flex;
		padding-top: 2px;
	}
	.jump .btn {
		white-space: nowrap;
	}
	.where {
		font-size: 12px;
		color: var(--muted, rgba(32, 30, 29, 0.62));
		font-variant-numeric: tabular-nums;
	}
</style>
