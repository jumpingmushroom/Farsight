// Minimal browser globals so `leaflet` (which probes window, document and
// navigator at import time) can be imported by Vitest in the Node
// environment. Import this module before anything that imports leaflet;
// Vitest isolates test files, so the stubs never leak into other suites.
const g = globalThis as Record<string, unknown>;
if (typeof g.window === 'undefined') {
	g.window = globalThis;
	g.devicePixelRatio = 1;
	if (typeof g.navigator === 'undefined') g.navigator = { userAgent: 'node', platform: 'node' };
	g.document = {
		documentElement: { style: {} },
		createElement: () => ({ getContext: () => null, style: {} })
	};
}
export {};
