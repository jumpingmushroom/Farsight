<!--
  Unlock a server (DESIGN-NOTES §3.21, plan ruling "Unlock screen"). Full screen
  when nothing is unlocked (canClose=false), otherwise a closable dialog opened
  from "Add a server…". Focus is trapped inside (focusTrap) and returns to
  the opener on close. The passphrase is never held in component state: it is
  read from the input on submit and the input is cleared once the call returns.
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import { focusTrap, type ReturnTo } from '$lib/actions/focusTrap';
	import { app, unlockMessage } from '$lib/state.svelte';

	interface Props {
		/** Prefilled server id (from `#s=`). */
		server?: string;
		/** Error to show on open (e.g. a failed share link). */
		error?: string;
		/** Esc / backdrop click close it only when at least one server is unlocked. */
		canClose?: boolean;
		onclose?: () => void;
		/** Where focus returns on close, when the opener may be gone (see focusTrap). */
		returnTo?: ReturnTo;
	}

	let { server: initialServer = '', error: initialError = '', canClose = false, onclose, returnTo }: Props = $props();

	// The props seed the fields once; later edits belong to the user.
	let server = $state(untrack(() => initialServer));
	let error = $state(untrack(() => initialError));
	let pending = $state(false);
	let serverInput: HTMLInputElement | undefined = $state();
	let passInput: HTMLInputElement | undefined = $state();

	// Initial focus: the server field when empty, else the passphrase.
	const initial = untrack(() => (server.trim() === '' ? '#unlock-server' : '#unlock-passphrase'));

	function close() {
		if (canClose && !pending) onclose?.();
	}

	function onkeydown(e: KeyboardEvent) {
		if (e.key === 'Escape' && canClose) {
			e.preventDefault();
			close();
		}
	}

	function onbackdrop(e: MouseEvent) {
		if (e.target === e.currentTarget) close();
	}

	async function onsubmit(e: SubmitEvent) {
		e.preventDefault();
		if (pending) return;
		const id = server.trim();
		if (id === '') {
			serverInput?.focus();
			return;
		}
		if (!passInput || passInput.value === '') {
			passInput?.focus();
			return;
		}
		pending = true;
		error = '';
		try {
			const r = await app.tryUnlock(id, passInput.value);
			if (r === 'ok') {
				app.closeUnlock();
				onclose?.();
				return;
			}
			error = unlockMessage(r);
		} catch {
			error = unlockMessage('error');
		} finally {
			pending = false;
			if (passInput) passInput.value = '';
		}
		passInput?.focus();
	}
</script>

<svelte:window {onkeydown} />

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions (Esc closes via the window handler) -->
<div class="dialog-backdrop unlock-backdrop" class:full={!canClose} onclick={onbackdrop}>
	<div class="dialog" role="dialog" aria-modal="true" aria-labelledby="unlock-title" tabindex="-1" use:focusTrap={{ initial, returnTo }}>
		<form {onsubmit}>
			<div class="brand">
				<span class="mark" aria-hidden="true">F</span>
				<span class="app-name">Farsight</span>
			</div>
			<h2 class="dialog-title" id="unlock-title">Unlock a server</h2>
			<div class="field">
				<label for="unlock-server">Server</label>
				<input
					id="unlock-server"
					class="input"
					name="server"
					type="text"
					autocomplete="off"
					autocapitalize="off"
					spellcheck="false"
					bind:value={server}
					bind:this={serverInput}
				/>
			</div>
			<div class="field">
				<label for="unlock-passphrase">Passphrase</label>
				<input
					id="unlock-passphrase"
					class="input"
					name="passphrase"
					type="password"
					autocomplete="current-password"
					bind:this={passInput}
				/>
			</div>
			{#if error}
				<p class="error" role="alert">{error}</p>
			{/if}
			<button class="btn btn-primary btn-block" type="submit" disabled={pending}>Unlock</button>
		</form>
	</div>
</div>

<style>
	.unlock-backdrop {
		z-index: 1000;
		backdrop-filter: blur(6px);
		-webkit-backdrop-filter: blur(6px);
	}
	.unlock-backdrop.full {
		background: color-mix(in srgb, var(--color-bg) 70%, transparent);
	}
	form {
		display: contents;
	}
	.brand {
		display: flex;
		align-items: center;
		gap: 10px;
	}
	.mark {
		width: 38px;
		height: 38px;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: var(--color-accent);
		color: var(--color-bg);
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 20px;
		line-height: 1;
	}
	.app-name {
		font-family: var(--font-heading);
		font-weight: var(--font-heading-weight);
		font-size: 18px;
	}
	.dialog-title {
		margin: 0;
	}
	.error {
		margin: 0;
		font-size: 13px;
		color: var(--color-accent-700);
	}
</style>
