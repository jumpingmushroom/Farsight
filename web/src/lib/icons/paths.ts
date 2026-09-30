// The ten marker icons (DESIGN-NOTES §4.3) as SVG element strings: one
// source of truth for pin HTML (Leaflet divIcons need strings) and for
// MarkerIcon.svelte (real SVG in the DOM). The Lucide entries copy the
// installed lucide-svelte geometry (paths.test.ts checks they match); portal,
// arch and the legacy home are the design's custom shapes.

export const ICON_NAMES = [
	'portal',
	'home',
	'bed',
	'skull',
	'paw-print',
	'signpost',
	'flame',
	'coins',
	'arch',
	'mountain'
] as const;

export type IconName = (typeof ICON_NAMES)[number];

export const ICON_PATHS: Record<IconName, string[]> = {
	// Custom: two concentric circles.
	portal: ['<circle cx="12" cy="12" r="9"/>', '<circle cx="12" cy="12" r="4"/>'],
	// Legacy Lucide "home" (the current `house` has different geometry).
	home: [
		'<path d="m3 9 9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
		'<polyline points="9 22 9 12 15 12 15 22"/>'
	],
	bed: ['<path d="M2 4v16"/>', '<path d="M2 8h18a2 2 0 0 1 2 2v10"/>', '<path d="M2 17h20"/>', '<path d="M6 8v9"/>'],
	skull: [
		'<path d="m12.5 17-.5-1-.5 1h1z"/>',
		'<path d="M15 22a1 1 0 0 0 1-1v-1a2 2 0 0 0 1.56-3.25 8 8 0 1 0-11.12 0A2 2 0 0 0 8 20v1a1 1 0 0 0 1 1z"/>',
		'<circle cx="15" cy="12" r="1"/>',
		'<circle cx="9" cy="12" r="1"/>'
	],
	'paw-print': [
		'<circle cx="11" cy="4" r="2"/>',
		'<circle cx="18" cy="8" r="2"/>',
		'<circle cx="20" cy="16" r="2"/>',
		'<path d="M9 10a5 5 0 0 1 5 5v3.5a3.5 3.5 0 0 1-6.84 1.045Q6.52 17.48 4.46 16.84A3.5 3.5 0 0 1 5.5 10Z"/>'
	],
	signpost: [
		'<path d="M12 13v8"/>',
		'<path d="M12 3v3"/>',
		'<path d="M18 6a2 2 0 0 1 1.387.56l2.307 2.22a1 1 0 0 1 0 1.44l-2.307 2.22A2 2 0 0 1 18 13H6a2 2 0 0 1-1.387-.56l-2.306-2.22a1 1 0 0 1 0-1.44l2.306-2.22A2 2 0 0 1 6 6z"/>'
	],
	flame: ['<path d="M12 3q1 4 4 6.5t3 5.5a1 1 0 0 1-14 0 5 5 0 0 1 1-3 1 1 0 0 0 5 0c0-2-1.5-3-1.5-5q0-2 2.5-4"/>'],
	coins: [
		'<path d="M13.744 17.736a6 6 0 1 1-7.48-7.48"/>',
		'<path d="M15 6h1v4"/>',
		'<path d="m6.134 14.768.866-.5 2 3.464"/>',
		'<circle cx="16" cy="8" r="6"/>'
	],
	// Custom: an arched gateway.
	arch: ['<path d="M4 22V10a8 8 0 0 1 16 0v12"/>', '<path d="M2 22h20"/>', '<path d="M9 22v-6a3 3 0 0 1 6 0v6"/>'],
	mountain: ['<path d="m8 3 4 8 5-5 5 15H2L8 3z"/>']
};

export const STROKE_WIDTH = 2.75;

/** A complete Lucide-style `<svg>` string for `name` at `px`, stroked in `color`. */
export function iconSvg(name: IconName, px: number, color: string): string {
	return (
		`<svg xmlns="http://www.w3.org/2000/svg" width="${px}" height="${px}" viewBox="0 0 24 24" fill="none" ` +
		`stroke="${color}" stroke-width="${STROKE_WIDTH}" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` +
		ICON_PATHS[name].join('') +
		'</svg>'
	);
}
