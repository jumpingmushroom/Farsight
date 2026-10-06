// The biome legend, dependency-free so pure modules (worldtime.ts) can use
// it without pulling in Leaflet through markers.ts.

/**
 * The layers-panel legend (§3.13): the tile renderer's palette
 * (internal/tiles/tiles.go), in the design's order, plus Unexplored.
 */
export const BIOME_LEGEND: { name: string; hex: string }[] = [
	{ name: 'Ocean', hex: '#2E5478' },
	{ name: 'Meadows', hex: '#A3B25C' },
	{ name: 'Black Forest', hex: '#3E5834' },
	{ name: 'Swamp', hex: '#786246' },
	{ name: 'Mountains', hex: '#E2E6EC' },
	{ name: 'Plains', hex: '#D6BE6E' },
	{ name: 'Mistlands', hex: '#6E6478' },
	{ name: 'Ashlands', hex: '#963C28' },
	{ name: 'Deep North', hex: '#C8D7E6' },
	{ name: 'Unexplored', hex: '#cfbe9c' }
];
