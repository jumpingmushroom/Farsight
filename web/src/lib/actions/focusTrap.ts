// A small focus trap for modal dialogs (JoinDialog, UnlockDialog): Tab and
// Shift+Tab cycle through the focusable elements inside the node, the
// initial focus goes to `initial` (a selector) or the first focusable, and on
// destroy focus returns to the element that was focused when the trap began
// (the opener), or to `returnTo` when given and still in the page (the
// opener may already be gone, e.g. a sheet row that closed as it opened the
// dialog). Esc is left to each dialog.

export interface FocusTrapOptions {
	/** Selector (inside the node) for the element to focus first. */
	initial?: string;
	/** Where focus goes on destroy, preferred over the captured opener while connected. */
	returnTo?: ReturnTo;
}

export type ReturnTo = HTMLElement | (() => HTMLElement | null | undefined) | null | undefined;

/** The element to refocus on close: `returnTo` (resolved now) if connected, else the captured opener if connected. */
export function resolveReturnTarget(returnTo: ReturnTo, captured: HTMLElement | null): HTMLElement | null {
	const r = typeof returnTo === 'function' ? returnTo() : returnTo;
	if (r?.isConnected) return r;
	return captured?.isConnected ? captured : null;
}

const FOCUSABLE = [
	'a[href]',
	'button:not([disabled])',
	'input:not([disabled]):not([type="hidden"])',
	'select:not([disabled])',
	'textarea:not([disabled])',
	'[tabindex]:not([tabindex="-1"])'
].join(',');

/**
 * The index to move to from `current` among `count` focusables, wrapping at
 * both ends. `current` −1 (focus outside the list) enters at the first
 * element, or the last when moving backwards. −1 when there is nothing.
 */
export function nextFocusIndex(current: number, count: number, backwards: boolean): number {
	if (count <= 0) return -1;
	if (current < 0 || current >= count) return backwards ? count - 1 : 0;
	return backwards ? (current - 1 + count) % count : (current + 1) % count;
}

function focusables(node: HTMLElement): HTMLElement[] {
	return Array.from(node.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
		(el) => el.getClientRects().length > 0 && !el.closest('[inert]')
	);
}

export function focusTrap(node: HTMLElement, options: FocusTrapOptions = {}) {
	const active = document.activeElement;
	const opener = active instanceof HTMLElement && !node.contains(active) ? active : null;

	const first = (options.initial && node.querySelector<HTMLElement>(options.initial)) || focusables(node)[0];
	(first ?? node).focus();

	function onkeydown(e: KeyboardEvent) {
		if (e.key !== 'Tab') return;
		const list = focusables(node);
		e.preventDefault();
		const cur = document.activeElement instanceof HTMLElement ? list.indexOf(document.activeElement) : -1;
		const next = nextFocusIndex(cur, list.length, e.shiftKey);
		(next >= 0 ? list[next] : node).focus();
	}

	node.addEventListener('keydown', onkeydown);
	return {
		destroy() {
			node.removeEventListener('keydown', onkeydown);
			resolveReturnTarget(options.returnTo, opener)?.focus();
		}
	};
}
