<!--
  How to join, shared body (DESIGN-NOTES §3.19): the platform switch, the
  join-code box (state from joinCodeView), the address box (PC only, hidden
  without an address), the steps, the info box and the world rules. Used by
  the desktop JoinDialog and the mobile join sheet (Task 8, `variant`
  'mobile' = touch sizes).

  Steam-only servers (crossplay off, [GAP] proposal): no platform switch and
  no code box; the address box is the primary ("Join by address") with the
  PC steps.

  Copy: the raw code digits / the address go to the clipboard, with a
  select-the-text fallback (see clipboard.ts); `oncopy(title, sub)` shows
  the toast.
-->
<script lang="ts">
	import Copy from 'lucide-svelte/icons/copy';
	import { copyText } from '$lib/clipboard';
	import { joinCodeView } from '$lib/derive';
	import { fmtCode } from '$lib/format';
	import { app } from '$lib/state.svelte';
	import type { Card } from '$lib/types';
	import WorldRules from './WorldRules.svelte';

	let {
		card,
		variant,
		oncopy
	}: { card: Card; variant: 'desktop' | 'mobile'; oncopy: (title: string, sub: string) => void } = $props();

	type Platform = 'pc' | 'xbox' | 'ps5' | 'switch2';
	const PLATFORMS: { key: Platform; label: string }[] = [
		{ key: 'pc', label: 'PC (Steam)' },
		{ key: 'xbox', label: 'Xbox' },
		{ key: 'ps5', label: 'PS5' },
		{ key: 'switch2', label: 'Switch 2' }
	];
	const COPY_FAILED = 'Couldn’t copy — select and copy manually';

	let platform = $state<Platform>('pc');
	let codeEl: HTMLElement | undefined = $state();
	let addrEl: HTMLElement | undefined = $state();

	const steam = $derived(!card.crossplay);
	const pc = $derived(steam || platform === 'pc');
	const code = $derived(joinCodeView(card, app.now));
	const steps = $derived.by(() => {
		if (!pc) {
			return [
				'Start Game and pick your character.',
				'Open Join Game and add a server by join code.',
				'Type the 6-digit code.',
				'Enter the server password.'
			];
		}
		let paste = 'Paste the join code or the address below, then Connect.';
		if (steam) paste = card.address ? 'Paste the address above, then Connect.' : 'Ask the server admin for the address, paste it, then Connect.';
		else if (!card.address) paste = 'Paste the join code above, then Connect.';
		return [
			'Start Game, pick your character, then Start.',
			'Open the Join Game tab and choose Join IP.',
			paste,
			'Enter the server password.'
		];
	});
	const hint = $derived(card.discordHint?.trim().replace(/[.!]+$/, ''));

	async function copyCode(): Promise<void> {
		if (!code.code) return;
		const ok = await copyText(code.code, codeEl);
		if (ok) oncopy('Join code copied', fmtCode(code.code));
		else oncopy(COPY_FAILED, '');
	}

	async function copyAddress(): Promise<void> {
		if (!card.address) return;
		const ok = await copyText(card.address, addrEl);
		if (ok) oncopy('Address copied', card.address);
		else oncopy(COPY_FAILED, '');
	}
</script>

<div class="join" class:mobile={variant === 'mobile'}>
	{#if !steam}
		<div class="platforms" role="group" aria-label="Platform">
			{#each PLATFORMS as p (p.key)}
				<button type="button" aria-pressed={platform === p.key} onclick={() => (platform = p.key)}>{p.label}</button>
			{/each}
		</div>
		<div class="code-box {code.state}">
			<div class="kicker-row">
				<span class="kicker">Join code</span>
				<span class="code-status"
					><span class="dot"></span>{code.state === 'live' ? 'Live from server' : code.status}</span
				>
			</div>
			<div class="code-row">
				{#if code.state === 'live' && code.code}
					<div class="code" bind:this={codeEl}>{fmtCode(code.code)}</div>
					<button class="btn btn-primary copy-code" type="button" onclick={copyCode}>
						<Copy size={16} strokeWidth={2.75} />Copy code
					</button>
				{:else if code.state === 'restarting'}
					<div class="code muted" aria-label="No code yet">— — —</div>
				{:else}
					<div class="code muted">No code</div>
				{/if}
			</div>
			<p class="note">{code.note}</p>
		</div>
	{/if}
	{#if pc && card.address}
		<div class="addr-box" class:primary={steam}>
			<div class="addr-text">
				<div class="kicker cold">{steam ? 'Join by address' : 'Or join by address (PC)'}</div>
				<div class="addr" bind:this={addrEl}>{card.address}</div>
				<div class="addr-note">{steam ? 'Stays the same across restarts.' : 'PC only. Stays the same across restarts.'}</div>
			</div>
			<button class="btn {steam ? 'btn-primary' : 'btn-secondary'} copy-addr" type="button" onclick={copyAddress}>
				<Copy size={15} strokeWidth={2.75} />Copy
			</button>
		</div>
	{/if}
	<ol class="steps">
		{#each steps as s, i (i)}
			<li><span class="n">{i + 1}</span><span class="step">{s}</span></li>
		{/each}
	</ol>
	<div class="info">
		<div>
			<b>Password:</b>
			{#if hint}{hint}.{:else}ask the server admin.{/if} It’s never shown here.
		</div>
		{#if card.version}
			<div><b>Version:</b> your game must be on <b>{card.version}</b>, the same as the server.</div>
		{/if}
		{#if !pc}
			<div class="muted">Crossplay must be allowed in your console’s privacy settings.</div>
		{/if}
	</div>
	{#if card.world}
		<WorldRules world={card.world} />
	{/if}
</div>

<style>
	.join {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.join.mobile {
		gap: 14px;
	}
	.platforms {
		display: flex;
		gap: 4px;
		padding: 4px;
		border-radius: 999px;
		background: color-mix(in srgb, var(--color-text) 7%, transparent);
	}
	.platforms button {
		flex: 1;
		border: 0;
		border-radius: 999px;
		min-height: 34px;
		padding: 0 6px;
		font: inherit;
		font-size: 13px;
		font-weight: 700;
		cursor: pointer;
		white-space: nowrap;
		background: transparent;
		color: var(--muted);
	}
	.mobile .platforms button {
		min-height: 44px;
	}
	.platforms button[aria-pressed='true'] {
		background: var(--color-surface);
		color: var(--color-text);
	}
	.code-box {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 18px;
		border-radius: 24px;
		background: color-mix(in srgb, var(--color-accent) 12%, transparent);
		border: 1.5px solid color-mix(in srgb, var(--color-accent) 40%, transparent);
	}
	.code-box.restarting {
		background: color-mix(in srgb, var(--cold) 8%, transparent);
		border-color: var(--cold);
	}
	.code-box.offline,
	.code-box.none {
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
		border-color: var(--color-divider);
	}
	.kicker-row {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.kicker {
		font-size: 11px;
		letter-spacing: 0.1em;
		text-transform: uppercase;
		font-weight: 700;
		color: var(--color-accent-700);
	}
	.kicker.cold {
		color: var(--cold-ink);
	}
	.code-status {
		margin-left: auto;
		display: flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		color: var(--muted);
	}
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--color-neutral-500);
	}
	.live .dot {
		background: var(--color-accent);
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--color-accent) 25%, transparent);
	}
	.restarting .dot {
		background: var(--cold);
	}
	.code-row {
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
	}
	.code {
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 52px;
		line-height: 1;
		letter-spacing: 0.06em;
		font-variant-numeric: tabular-nums;
	}
	.mobile .code:not(.muted) {
		font-size: 44px;
	}
	/* No live code: the reference join-code state size (§3.19 codeStates). */
	.code.muted {
		font-size: 34px;
		letter-spacing: 0.05em;
		color: var(--muted);
	}
	.copy-code {
		margin-left: auto;
		white-space: nowrap;
		min-height: 40px;
	}
	.mobile .btn {
		min-height: 48px;
	}
	.note {
		margin: 0;
		font-size: 12.5px;
		line-height: 1.45;
		color: var(--muted);
	}
	.addr-box {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 14px 16px;
		border-radius: 22px;
		background: color-mix(in srgb, var(--color-text) 6%, transparent);
	}
	.addr-box.primary {
		padding: 18px;
		border-radius: 24px;
		background: color-mix(in srgb, var(--color-accent) 12%, transparent);
		border: 1.5px solid color-mix(in srgb, var(--color-accent) 40%, transparent);
	}
	.addr-text {
		flex: 1;
		min-width: 0;
	}
	.addr {
		font-size: 17px;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
		overflow-wrap: anywhere;
	}
	.addr-note {
		font-size: 12px;
		color: var(--muted);
	}
	.copy-addr {
		white-space: nowrap;
		flex: none;
		min-height: 40px;
	}
	.steps {
		display: flex;
		flex-direction: column;
		gap: 10px;
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.steps li {
		display: flex;
		gap: 12px;
		align-items: flex-start;
	}
	.n {
		width: 26px;
		height: 26px;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: var(--color-accent-2-200);
		color: var(--color-accent-2-800);
		font-weight: 700;
		font-size: 13px;
	}
	.step {
		font-size: 14px;
		line-height: 1.45;
		padding-top: 3px;
		text-wrap: pretty;
	}
	.info {
		display: flex;
		flex-direction: column;
		gap: 6px;
		padding: 12px 14px;
		border-radius: 18px;
		border: 1px dashed var(--color-divider);
		font-size: 13px;
		line-height: 1.45;
	}
	.muted {
		color: var(--muted);
	}
</style>
