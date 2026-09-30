// Copy to the clipboard (plan ruling for §3.19). navigator.clipboard first;
// it is missing on non-secure (HTTP) origins and may reject. Then the raw
// text is copied through a temporary off-screen <textarea> with
// execCommand('copy'). Only when that fails too is `fallback`'s text
// selected, so the user can copy it by hand, and false returned.
//
// The browser objects are injectable so the logic is unit-tested
// (clipboard.test.ts) without a DOM.

export interface ClipboardEnv {
	clipboard?: { writeText(text: string): Promise<void> };
	document: Pick<Document, 'createElement' | 'execCommand' | 'createRange' | 'body' | 'activeElement'>;
	getSelection: () => Pick<Selection, 'removeAllRanges' | 'addRange'> | null;
}

function browserEnv(): ClipboardEnv {
	return {
		clipboard: typeof navigator !== 'undefined' ? navigator.clipboard : undefined,
		document,
		getSelection: () => window.getSelection()
	};
}

function copyViaTextarea(text: string, env: ClipboardEnv): boolean {
	const doc = env.document;
	// Selecting the textarea moves focus; give it back to the Copy button.
	const prev = doc.activeElement as HTMLElement | null;
	const ta = doc.createElement('textarea');
	ta.value = text;
	ta.setAttribute('readonly', '');
	ta.setAttribute('aria-hidden', 'true');
	ta.style.position = 'fixed';
	ta.style.top = '0';
	ta.style.left = '-9999px';
	ta.style.opacity = '0';
	doc.body.appendChild(ta);
	try {
		ta.select();
		return doc.execCommand('copy');
	} catch {
		return false;
	} finally {
		ta.remove();
		prev?.focus?.();
	}
}

export async function copyText(text: string, fallback?: HTMLElement | null, env: ClipboardEnv = browserEnv()): Promise<boolean> {
	try {
		if (env.clipboard?.writeText) {
			await env.clipboard.writeText(text);
			return true;
		}
	} catch {
		// Fall through to the textarea copy.
	}
	if (copyViaTextarea(text, env)) return true;
	if (fallback) {
		try {
			const range = env.document.createRange();
			range.selectNodeContents(fallback);
			const sel = env.getSelection();
			sel?.removeAllRanges();
			sel?.addRange(range);
		} catch {
			// Nothing more to do; the caller shows the "copy manually" toast.
		}
	}
	return false;
}
