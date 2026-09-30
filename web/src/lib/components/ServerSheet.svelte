<!--
  Mobile server switcher sheet (DESIGN-NOTES §1.7, §3.5 mobile rows and the
  switcher ruling): an auto-height surface sheet opened by tapping the
  server name in the top bar. The title "Servers", a listbox of 68 px rows
  (the current one with a cold tint and border; dot, name over the sub-line,
  the count), "Add a server…" (the unlock dialog), and the ruled footer
  "Only servers you’ve unlocked are listed." (the design's archive footer
  is [OUT]). Picking a server switches to it and closes the sheet; ↑/↓ move
  between rows. Not aria-modal: the top-bar server name toggles it and must
  stay reachable (Esc and the scrim close it too).
-->
<script lang="ts">
	import Plus from 'lucide-svelte/icons/plus';
	import { onMount } from 'svelte';
	import { isOfflineLike, statusView, switcherSub } from '$lib/derive';
	import { app } from '$lib/state.svelte';
	import BottomSheet from './BottomSheet.svelte';

	let { onclose }: { onclose: (restoreFocus?: boolean) => void } = $props();

	const uid = $props.id();
	let root: HTMLDivElement;

	function pick(id: string): void {
		onclose();
		if (id !== app.currentId) app.select(id);
	}

	function addServer(): void {
		// The unlock dialog takes focus (its trap); don't pull it back to the top bar.
		// Focus returns to the top-bar server button when it closes (this row is gone by then).
		onclose(false);
		app.openUnlock(undefined, () =>
			document.querySelector<HTMLElement>('[data-server-button]')
		);
	}

	function onkeydown(e: KeyboardEvent): void {
		if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
		const rows = Array.from(root.querySelectorAll<HTMLElement>('[data-row]'));
		const i = rows.indexOf(document.activeElement as HTMLElement);
		e.preventDefault();
		const n = rows.length;
		rows[e.key === 'ArrowDown' ? (i + 1) % n : (i - 1 + n) % n]?.focus();
	}

	onMount(() => {
		root.querySelector<HTMLElement>('[aria-selected="true"]')?.focus({ preventScroll: true });
	});
</script>

<BottomSheet snap="auto" surface="surface" onclose={() => onclose()}>
	<!-- svelte-ignore a11y_no_noninteractive_element_interactions (arrow keys move between the rows inside) -->
	<div class="servers" role="dialog" aria-labelledby="{uid}-title" tabindex="-1" bind:this={root} {onkeydown}>
		<h2 class="title" id="{uid}-title" data-sheet-drag>Servers</h2>
		<div class="list" role="listbox" aria-labelledby="{uid}-title">
			{#each app.servers as s (s.id)}
				{@const current = s.id === app.currentId}
				{@const card = current && app.card?.id === s.id ? app.card : undefined}
				{@const status = card?.status ?? s.status}
				<button class="row" class:current type="button" role="option" aria-selected={current} data-row onclick={() => pick(s.id)}>
					<span class="dot" style:background={statusView(status).dot}></span>
					<span class="text">
						<span class="name">{s.name}</span>
						<span class="sub">{switcherSub({ ...s, status }, card)}</span>
					</span>
					<span class="count"
						>{isOfflineLike(status) ? '—' : `${card?.players ?? s.players}/${card?.maxPlayers ?? s.maxPlayers}`}</span
					>
				</button>
			{/each}
		</div>
		<button class="row add" type="button" data-row onclick={addServer}>
			<span class="add-icon" aria-hidden="true"><Plus size={16} strokeWidth={2.75} /></span>
			<span class="text"><span class="name">Add a server…</span></span>
		</button>
		<div class="foot">Only servers you’ve unlocked are listed.</div>
	</div>
</BottomSheet>

<style>
	.servers {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 16px 16px var(--sheet-bottom);
		outline: none;
	}
	.title {
		margin: 0;
		padding: 0 4px 4px;
		font-size: 22px;
		line-height: 1.3;
		letter-spacing: normal;
	}
	.list {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 12px;
		width: 100%;
		min-height: 68px;
		padding: 0 14px;
		box-sizing: border-box;
		border-radius: 22px;
		border: 1.5px solid var(--color-divider);
		background: transparent;
		color: var(--color-text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.row.current {
		background: color-mix(in srgb, var(--cold) 14%, transparent);
		border-color: var(--cold);
	}
	.row:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}
	.dot {
		width: 11px;
		height: 11px;
		flex: none;
		border-radius: 50%;
	}
	.text {
		flex: 1;
		min-width: 0;
	}
	.name {
		display: block;
		font-weight: 700;
		font-size: 16px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.sub {
		display: block;
		font-size: 13px;
		color: var(--muted);
	}
	.count {
		font-size: 14px;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
	}
	.add {
		min-height: 56px;
		border-style: dashed;
	}
	.add .name {
		font-weight: 600;
	}
	.add-icon {
		width: 11px;
		flex: none;
		display: grid;
		place-items: center;
		color: var(--muted);
	}
	.foot {
		font-size: 12.5px;
		color: var(--muted);
		padding: 6px 4px 0;
		line-height: 1.45;
	}
</style>
