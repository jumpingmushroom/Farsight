<!--
  Server switcher (DESIGN-NOTES §3.5 and the plan ruling): the server name
  and an "N servers" pill; the dropdown lists the unlocked servers
  (role=listbox), then "Add a server…" (opens the unlock dialog), then the
  ruled footer. Keyboard: ↓/↑ move between rows (↓ on the trigger opens),
  Enter picks, Esc closes; a click outside closes too.
-->
<script lang="ts">
	import ChevronDown from 'lucide-svelte/icons/chevron-down';
	import Plus from 'lucide-svelte/icons/plus';
	import { statusView, switcherSub, isOfflineLike } from '$lib/derive';
	import { app } from '$lib/state.svelte';

	let { name }: { name: string } = $props();

	let open = $state(false);
	let root: HTMLDivElement | undefined = $state();
	let trigger: HTMLButtonElement | undefined = $state();
	let menu: HTMLDivElement | undefined = $state();

	const count = $derived(app.servers.length);
	const pill = $derived(count === 1 ? '1 server' : `${count} servers`);

	function rows(): HTMLElement[] {
		return menu ? Array.from(menu.querySelectorAll<HTMLElement>('[data-row]')) : [];
	}

	function show(focusCurrent = true): void {
		open = true;
		if (!focusCurrent) return;
		requestAnimationFrame(() => {
			const list = rows();
			(list.find((r) => r.getAttribute('aria-selected') === 'true') ?? list[0])?.focus();
		});
	}

	function hide(refocus: boolean): void {
		open = false;
		if (refocus) trigger?.focus();
	}

	function pick(id: string): void {
		hide(true);
		if (id !== app.currentId) app.select(id);
	}

	function addServer(): void {
		// Focus the trigger first so the unlock dialog returns focus there.
		hide(true);
		app.openUnlock(undefined, () => trigger);
	}

	function ontriggerkey(e: KeyboardEvent): void {
		if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && !open) {
			e.preventDefault();
			show();
		}
	}

	function onmenukey(e: KeyboardEvent): void {
		const list = rows();
		const i = list.indexOf(document.activeElement as HTMLElement);
		if (e.key === 'ArrowDown') {
			e.preventDefault();
			list[(i + 1) % list.length]?.focus();
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			list[(i - 1 + list.length) % list.length]?.focus();
		} else if (e.key === 'Home') {
			e.preventDefault();
			list[0]?.focus();
		} else if (e.key === 'End') {
			e.preventDefault();
			list[list.length - 1]?.focus();
		} else if (e.key === 'Escape') {
			e.preventDefault();
			e.stopPropagation();
			hide(true);
		} else if (e.key === 'Tab') {
			hide(false);
		}
	}

	function onpointerdown(e: PointerEvent): void {
		if (open && root && !root.contains(e.target as Node)) hide(false);
	}
</script>

<svelte:window {onpointerdown} />

<div class="switcher" bind:this={root}>
	<button
		class="trigger"
		type="button"
		aria-haspopup="listbox"
		aria-expanded={open}
		aria-controls={open ? 'server-list' : undefined}
		aria-label="Switch server: {name}"
		bind:this={trigger}
		onclick={() => (open ? hide(false) : show())}
		onkeydown={ontriggerkey}
	>
		<span class="name">{name}</span>
		<span class="pill">{pill}<ChevronDown size={14} strokeWidth={2.75} /></span>
	</button>
	{#if open}
		<!-- svelte-ignore a11y_no_static_element_interactions (keyboard handling for the rows inside) -->
		<div class="menu" id="server-menu" bind:this={menu} onkeydown={onmenukey}>
			<div class="list" id="server-list" role="listbox" aria-label="Servers">
				{#each app.servers as s (s.id)}
					{@const current = s.id === app.currentId}
					{@const card = current && app.card?.id === s.id ? app.card : undefined}
					{@const status = card?.status ?? s.status}
					<button
						class="row"
						class:current
						type="button"
						role="option"
						aria-selected={current}
						data-row
						onclick={() => pick(s.id)}
					>
						<span class="dot" style:background={statusView(status).dot}></span>
						<span class="text">
							<span class="row-name">{s.name}</span>
							<span class="sub">{switcherSub({ ...s, status }, card)}</span>
						</span>
						<span class="count"
							>{isOfflineLike(status) ? '—' : `${card?.players ?? s.players}/${card?.maxPlayers ?? s.maxPlayers}`}</span
						>
					</button>
				{/each}
			</div>
			<button class="row add" type="button" data-row onclick={addServer}>
				<span class="add-icon" aria-hidden="true"><Plus size={14} strokeWidth={2.75} /></span>
				<span class="row-name">Add a server…</span>
			</button>
			<div class="foot">Only servers you’ve unlocked are listed.</div>
		</div>
	{/if}
</div>

<style>
	.switcher {
		position: relative;
	}
	.trigger {
		display: flex;
		align-items: center;
		gap: 8px;
		width: 100%;
		border: 0;
		background: transparent;
		padding: 0;
		color: var(--color-text);
		font: inherit;
		cursor: pointer;
		text-align: left;
		border-radius: 8px;
	}
	.name {
		flex: 1;
		min-width: 0;
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 22px;
		line-height: 1.1;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.pill {
		flex: none;
		height: 28px;
		padding: 0 8px 0 11px;
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-text) 9%, transparent);
		display: flex;
		align-items: center;
		gap: 4px;
		font-size: 12px;
		font-weight: 700;
		white-space: nowrap;
	}
	.menu {
		position: absolute;
		z-index: 5;
		top: 38px;
		left: -10px;
		right: -10px;
		padding: 8px;
		border-radius: 22px;
		background: var(--color-surface);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-lg);
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.list {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		padding: 9px 10px;
		border: 0;
		border-radius: 16px;
		background: transparent;
		color: var(--color-text);
		font: inherit;
		cursor: pointer;
		text-align: left;
	}
	.row:hover {
		background: color-mix(in srgb, var(--color-text) 7%, transparent);
	}
	.row.current {
		background: color-mix(in srgb, var(--cold) 14%, transparent);
	}
	.dot {
		width: 9px;
		height: 9px;
		flex: none;
		border-radius: 50%;
	}
	.text {
		flex: 1;
		min-width: 0;
	}
	.row-name {
		display: block;
		font-weight: 700;
		font-size: 14px;
	}
	.sub {
		display: block;
		font-size: 12px;
		color: var(--muted);
	}
	.count {
		font-size: 12px;
		font-weight: 700;
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}
	.add-icon {
		width: 9px;
		flex: none;
		display: grid;
		place-items: center;
		color: var(--muted);
	}
	.add .row-name {
		font-weight: 600;
	}
	.foot {
		padding: 8px 10px 4px;
		font-size: 12px;
		color: var(--muted);
		border-top: 1px solid var(--color-divider);
		margin-top: 4px;
	}
</style>
