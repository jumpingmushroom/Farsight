<!--
  Search results (DESIGN-NOTES §3.12, §5.2): the "Try" hint chips for an
  empty query, the result rows (a 32 px pin disc with the marker icon, the
  title over the sub-line), or "Nothing matches “{q}” in the last save.".
  Reused by the mobile menu sheet (`mobile`: larger hint chips, §3.18).

  Rows and chips act on mousedown with preventDefault, so the input keeps
  focus (its blur would otherwise close the panel before the click lands).
  They also act on a click with `detail === 0` (keyboard or assistive tech;
  a real click already acted on mousedown). The rows are a listbox;
  `active` marks the keyboard-highlighted option, whose id is
  `{listId}-opt-{i}` for the input's aria-activedescendant. On desktop the
  input keeps focus (rows tabindex −1); in the mobile menu the rows are in
  the Tab order and Enter/Space pick the focused one.
-->
<script lang="ts">
	import type { SearchResult } from '$lib/search';
	import MarkerIcon from './MarkerIcon.svelte';

	let {
		results,
		hints,
		q,
		onpick,
		onhint,
		active = -1,
		listId = 'search-results',
		mobile = false
	}: {
		results: SearchResult[];
		hints: string[];
		q: string;
		onpick: (id: string) => void;
		onhint: (h: string) => void;
		active?: number;
		listId?: string;
		mobile?: boolean;
	} = $props();

	const query = $derived(q.trim());

	function down(e: MouseEvent, act: () => void): void {
		if (e.button !== 0) return;
		e.preventDefault();
		act();
	}

	function key(e: KeyboardEvent, act: () => void): void {
		if (e.key !== 'Enter' && e.key !== ' ') return;
		e.preventDefault();
		act();
	}
</script>

{#if query === ''}
	{#if hints.length}
		<div class="try" class:mobile>
			{#if !mobile}<div class="try-label">Try</div>{/if}
			<div class="chips">
				{#each hints as h (h)}
					<button
						class="tag tag-neutral chip"
						type="button"
						onmousedown={(e) => down(e, () => onhint(h))}
						onclick={(e) => e.detail === 0 && onhint(h)}>{h}</button
					>
				{/each}
			</div>
		</div>
	{/if}
{:else if results.length}
	<div class="list" id="{listId}-listbox" role="listbox" aria-label="Search results">
		{#each results as r, i (r.id)}
			<div
				class="row"
				class:active={i === active}
				id="{listId}-opt-{i}"
				role="option"
				aria-selected={i === active}
				tabindex={mobile ? 0 : -1}
				onmousedown={(e) => down(e, () => onpick(r.id))}
				onclick={(e) => e.detail === 0 && onpick(r.id)}
				onkeydown={mobile ? (e) => key(e, () => onpick(r.id)) : undefined}
			>
				<span class="disc" style:background={r.pin.bg}><MarkerIcon name={r.icon} size={16} color={r.pin.iconColor} /></span>
				<span class="text">
					<span class="title">{r.title}</span>
					<span class="sub">{r.sub}</span>
				</span>
			</div>
		{/each}
	</div>
{:else}
	<div class="none" role="status">Nothing matches “{query}” in the last save.</div>
{/if}

<style>
	.try-label {
		padding: 6px 8px 4px;
		font-size: 12px;
		color: var(--muted);
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		padding: 0 6px 6px;
	}
	.chip {
		border: 0;
		cursor: pointer;
		font: inherit;
		font-size: 13px;
		padding: 5px 12px;
	}
	.mobile .chips {
		gap: 8px;
		padding: 0;
	}
	.mobile .chip {
		font-size: 14px;
		padding: 8px 14px;
	}
	.list {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 8px;
		border-radius: 16px;
		cursor: pointer;
	}
	.row:hover,
	.row.active {
		background: color-mix(in srgb, var(--cold) 16%, transparent);
	}
	.disc {
		width: 32px;
		height: 32px;
		flex: none;
		box-sizing: border-box;
		border-radius: 50%;
		display: grid;
		place-items: center;
		border: 2px solid #f5ead8;
	}
	.text {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.title {
		font-weight: 700;
		font-size: 14px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.sub {
		font-size: 12px;
		color: var(--muted);
	}
	.none {
		padding: 10px 8px;
		font-size: 14px;
		color: var(--muted);
	}
</style>
