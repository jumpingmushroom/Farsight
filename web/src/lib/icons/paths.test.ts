import { describe, expect, test } from 'vitest';
import { ICON_NAMES, ICON_PATHS, iconSvg } from './paths';

// The installed lucide-svelte sources, read raw (its `exports` hide the files
// from a normal import).
const LUCIDE = import.meta.glob<string>(
	'/node_modules/lucide-svelte/dist/icons/{bed,skull,paw-print,signpost,flame,coins,mountain,anvil,sparkles,circle-dot,castle,landmark}.svelte',
	{ query: '?raw', import: 'default', eager: true }
);

/** The element strings lucide-svelte renders for `name`, read from its iconNode. */
function lucideElements(name: string): string[] {
	const src = LUCIDE[`/node_modules/lucide-svelte/dist/icons/${name}.svelte`];
	if (!src) throw new Error(`lucide-svelte has no ${name}.svelte`);
	const m = src.match(/const iconNode = (\[.*?\]);\n/);
	if (!m) throw new Error(`no iconNode in ${name}`);
	const node = JSON.parse(m[1]) as [string, Record<string, string>][];
	return node.map(([tag, attrs]) => {
		const a = Object.entries(attrs)
			.map(([k, v]) => `${k}="${v}"`)
			.join(' ');
		return `<${tag} ${a}/>`;
	});
}

describe('ICON_PATHS', () => {
	test('every IconName has at least one SVG element', () => {
		expect(ICON_NAMES).toEqual([
			'portal',
			'home',
			'bed',
			'skull',
			'paw-print',
			'signpost',
			'flame',
			'coins',
			'arch',
			'mountain',
			'anvil',
			'sparkles',
			'circle-dot',
			'castle',
			'landmark'
		]);
		for (const name of ICON_NAMES) {
			const els = ICON_PATHS[name];
			expect(els.length, name).toBeGreaterThan(0);
			for (const el of els) expect(el, name).toMatch(/^<(path|circle|polyline) [^<>]+\/>$/);
		}
	});

	test('Lucide icons match the installed lucide-svelte geometry', () => {
		for (const name of [
			'bed',
			'skull',
			'paw-print',
			'signpost',
			'flame',
			'coins',
			'mountain',
			'anvil',
			'sparkles',
			'circle-dot',
			'castle',
			'landmark'
		] as const) {
			expect(ICON_PATHS[name], name).toEqual(lucideElements(name));
		}
	});

	test('custom icons are the DESIGN-NOTES §4.3 geometry', () => {
		expect(ICON_PATHS.portal).toEqual([
			'<circle cx="12" cy="12" r="9"/>',
			'<circle cx="12" cy="12" r="4"/>'
		]);
		expect(ICON_PATHS.arch).toEqual([
			'<path d="M4 22V10a8 8 0 0 1 16 0v12"/>',
			'<path d="M2 22h20"/>',
			'<path d="M9 22v-6a3 3 0 0 1 6 0v6"/>'
		]);
		expect(ICON_PATHS.home).toEqual([
			'<path d="m3 9 9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
			'<polyline points="9 22 9 12 15 12 15 22"/>'
		]);
	});
});

describe('iconSvg', () => {
	test('renders a Lucide-style SVG at stroke-width 2.75', () => {
		const s = iconSvg('portal', 15, '#f5ead8');
		expect(s.startsWith('<svg ')).toBe(true);
		expect(s).toContain('width="15"');
		expect(s).toContain('height="15"');
		expect(s).toContain('viewBox="0 0 24 24"');
		expect(s).toContain('stroke="#f5ead8"');
		expect(s).toContain('stroke-width="2.75"');
		expect(s).toContain('fill="none"');
		expect(s).toContain('stroke-linecap="round"');
		expect(s).toContain('stroke-linejoin="round"');
		expect(s).toContain('aria-hidden="true"');
		expect(s).toContain(ICON_PATHS.portal.join(''));
		expect(s.endsWith('</svg>')).toBe(true);
	});
});
