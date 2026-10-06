<!--
  Desktop time-and-weather pill (design `:309-336`, DESIGN-NOTES §1.1 item
  5, spec "Pill"): under the map-updated pill at `left: pillL`. A button
  with the 34 px sun (accent-100) or moon (cold 25%) disc, "{label} ·
  {clock}" and the "{home}: {weather}" line, and a chevron; it opens a
  360 px dropdown with WeatherContent. `open` is the shell's (Esc and the
  other menus close it there); a pointer-down outside closes it here.

  The `mobile` variant is the design's compact chip (`:479`: 44 px, 32 px
  disc, "{clock} · {weather}"), unpositioned (MobileShell places it under
  the top bar); a tap calls `onopen` and the shell opens WeatherSheet.
-->
<script lang="ts">
	import ChevronDown from 'lucide-svelte/icons/chevron-down';
	import Moon from 'lucide-svelte/icons/moon';
	import Sun from 'lucide-svelte/icons/sun';
	import type { TimeView } from '$lib/worldtime';
	import WeatherContent from './WeatherContent.svelte';

	let {
		view,
		left,
		top = 106,
		open = $bindable(false),
		mobile = false,
		onopen
	}: {
		view: TimeView;
		left?: number;
		top?: number;
		mobile?: boolean;
		open?: boolean;
		/** Opening: the shell closes its other menus (mobile: opens the sheet). */
		onopen?: () => void;
	} = $props();

	const uid = $props.id();
	let root: HTMLDivElement | undefined = $state();

	function toggle(): void {
		if (mobile) {
			onopen?.();
			return;
		}
		open = !open;
		if (open) onopen?.();
	}

	function onpointerdown(e: PointerEvent): void {
		if (!mobile && open && root && !root.contains(e.target as Node)) open = false;
	}
</script>

<svelte:window {onpointerdown} />

{#if mobile}
	<button
		class="chip"
		type="button"
		aria-haspopup="dialog"
		aria-expanded={open}
		aria-label="Time and weather: {view.label} · {view.chip}"
		onclick={toggle}
		data-testid="weather-chip"
	>
		<span class="disc small" class:night={view.night} aria-hidden="true">
			{#if view.night}<Moon size={16} strokeWidth={2.75} />{:else}<Sun size={16} strokeWidth={2.75} />{/if}
		</span>
		<span class="chip-text">{view.chip}</span>
	</button>
{:else}
	<div class="wrap" style:left="{left ?? 16}px" style:top="{top}px" bind:this={root} data-testid="weather-pill">
		<button
			class="pill"
			class:open
			type="button"
			aria-expanded={open}
			aria-controls={open ? `${uid}-panel` : undefined}
			onclick={toggle}
		>
			<span class="disc" class:night={view.night} aria-hidden="true">
				{#if view.night}<Moon size={18} strokeWidth={2.75} />{:else}<Sun size={18} strokeWidth={2.75} />{/if}
			</span>
			<span class="text">
				<b class="title">{view.label} · {view.clock}</b>
				<span class="line">{view.line}</span>
			</span>
			<span class="chev" aria-hidden="true"><ChevronDown size={14} strokeWidth={2.75} /></span>
		</button>
		{#if open}
			<div class="panel" id="{uid}-panel" role="region" aria-label="Time and weather">
				<WeatherContent {view} />
			</div>
		{/if}
	</div>
{/if}

<style>
	.wrap {
		position: absolute;
	}
	.pill {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 7px 14px 7px 7px;
		border-radius: 999px;
		border: 1px solid var(--color-divider);
		background: var(--glass);
		box-shadow: var(--shadow-md);
		color: var(--color-text);
		font: inherit;
		cursor: pointer;
	}
	.pill.open {
		border-color: var(--cold);
	}
	.pill:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}
	.disc {
		width: 34px;
		height: 34px;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: var(--color-accent-100);
		color: var(--color-accent-700);
	}
	.disc.night {
		background: color-mix(in srgb, var(--cold) 25%, transparent);
		color: var(--cold-ink);
	}
	.disc.small {
		width: 32px;
		height: 32px;
	}
	.chip {
		display: flex;
		align-items: center;
		gap: 8px;
		height: 44px;
		padding: 0 14px 0 6px;
		border-radius: 999px;
		border: 1px solid var(--color-divider);
		background: var(--glass);
		backdrop-filter: blur(14px);
		-webkit-backdrop-filter: blur(14px);
		box-shadow: var(--shadow-md);
		color: var(--color-text);
		font: inherit;
		cursor: pointer;
	}
	.chip:focus-visible {
		outline: 2px solid var(--color-accent);
		outline-offset: 2px;
	}
	.chip-text {
		font-size: 13px;
		font-weight: 700;
		white-space: nowrap;
		font-variant-numeric: tabular-nums;
	}
	.text {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		line-height: 1.25;
	}
	.title {
		font-size: 14px;
		white-space: nowrap;
		font-variant-numeric: tabular-nums;
	}
	.line {
		font-size: 12px;
		color: var(--muted);
		white-space: nowrap;
	}
	.chev {
		display: grid;
		margin-left: 2px;
	}
	.panel {
		position: absolute;
		top: 58px;
		left: 0;
		width: 360px;
		box-sizing: border-box;
		padding: 16px;
		border-radius: 26px;
		background: var(--color-surface);
		color: var(--color-text);
		border: 1px solid var(--color-divider);
		box-shadow: var(--shadow-lg);
	}
</style>
