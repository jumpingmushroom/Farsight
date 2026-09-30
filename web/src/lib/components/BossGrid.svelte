<!--
  Boss grid (DESIGN-NOTES §3.9 item 2): roman-numeral discs, filled when
  defeated and dashed otherwise, with the API names shortened (The Elder →
  Elder, The Queen → Queen).
-->
<script lang="ts">
	import { bossShort } from '$lib/derive';
	import { roman } from '$lib/format';
	import type { Boss } from '$lib/types';

	let { bosses }: { bosses: Boss[] } = $props();
</script>

<ol class="grid" style:grid-template-columns="repeat({Math.max(bosses.length, 1)}, auto)">
	{#each bosses as b, i (b.key)}
		<li class="boss" class:defeated={b.defeated} title="{b.name}{b.defeated ? ' · defeated' : ' · not yet defeated'}">
			<span class="disc" aria-hidden="true">{roman(i + 1)}</span>
			<span class="name">{bossShort(b.name)}<span class="sr">{b.defeated ? ', defeated' : ', not yet defeated'}</span></span>
		</li>
	{/each}
</ol>

<style>
	/* Columns size to their content (disc or name) and share the spare room,
	   so the longest names ("Bonemass") fit unclipped in the 304 px body. */
	.grid {
		display: grid;
		justify-content: space-between;
		gap: 2px;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.boss {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 6px;
		text-align: center;
	}
	.disc {
		/* 38 px in the design, where 8 discs overlap in the 304 px body. */
		width: 34px;
		height: 34px;
		box-sizing: border-box;
		border-radius: 50%;
		display: grid;
		place-items: center;
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 14px;
		background: transparent;
		color: var(--muted);
		border: 2px dashed color-mix(in srgb, var(--color-text) 35%, transparent);
	}
	.defeated .disc {
		background: var(--color-accent);
		color: var(--color-bg);
		border: 2px solid var(--color-accent);
	}
	.name {
		font-size: 9.5px;
		letter-spacing: -0.01em;
		line-height: 1.2;
		color: var(--muted);
		white-space: nowrap;
	}
	.defeated .name {
		color: var(--color-text);
	}
	.sr {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip: rect(0 0 0 0);
		white-space: nowrap;
	}
</style>
