import { describe, expect, test } from 'vitest';
import { copyText, type ClipboardEnv } from './clipboard';

interface FakeTextarea {
	value: string;
	style: Record<string, string>;
	attrs: Record<string, string>;
	selected: boolean;
	removed: boolean;
	setAttribute(k: string, v: string): void;
	select(): void;
	remove(): void;
}

function fakeEnv(opts: { clipboard?: 'ok' | 'reject'; exec: boolean }) {
	const log = { written: [] as string[], copied: [] as string[], textareas: [] as FakeTextarea[], selectedNode: undefined as unknown };
	let selectedValue: string | undefined;
	const env = {
		clipboard:
			opts.clipboard === undefined
				? undefined
				: {
						writeText: (t: string) => {
							if (opts.clipboard === 'reject') return Promise.reject(new Error('denied'));
							log.written.push(t);
							return Promise.resolve();
						}
					},
		document: {
			body: { appendChild: (n: unknown) => n },
			createElement: () => {
				const ta: FakeTextarea = {
					value: '',
					style: {},
					attrs: {},
					selected: false,
					removed: false,
					setAttribute(k, v) {
						this.attrs[k] = v;
					},
					select() {
						this.selected = true;
						selectedValue = this.value;
					},
					remove() {
						this.removed = true;
					}
				};
				log.textareas.push(ta);
				return ta;
			},
			execCommand: () => {
				if (opts.exec && selectedValue !== undefined) log.copied.push(selectedValue);
				return opts.exec;
			},
			createRange: () => ({ selectNodeContents: (n: unknown) => (log.selectedNode = n) })
		},
		getSelection: () => ({ removeAllRanges: () => {}, addRange: () => {} })
	} as unknown as ClipboardEnv;
	return { env, log };
}

const shown = { textContent: '318 742' } as unknown as HTMLElement;

describe('copyText', () => {
	test('uses navigator.clipboard when it works', async () => {
		const { env, log } = fakeEnv({ clipboard: 'ok', exec: true });
		expect(await copyText('318742', shown, env)).toBe(true);
		expect(log.written).toEqual(['318742']);
		expect(log.textareas).toHaveLength(0);
	});
	test('no clipboard API (HTTP): copies the raw text through a removed textarea', async () => {
		const { env, log } = fakeEnv({ exec: true });
		expect(await copyText('318742', shown, env)).toBe(true);
		expect(log.copied).toEqual(['318742']);
		expect(log.textareas[0].removed).toBe(true);
		expect(log.selectedNode).toBeUndefined();
	});
	test('clipboard rejects: falls back to the textarea', async () => {
		const { env, log } = fakeEnv({ clipboard: 'reject', exec: true });
		expect(await copyText('318742', shown, env)).toBe(true);
		expect(log.copied).toEqual(['318742']);
	});
	test('everything fails: selects the shown text and returns false', async () => {
		const { env, log } = fakeEnv({ exec: false });
		expect(await copyText('318742', shown, env)).toBe(false);
		expect(log.textareas[0].removed).toBe(true);
		expect(log.selectedNode).toBe(shown);
	});
});
