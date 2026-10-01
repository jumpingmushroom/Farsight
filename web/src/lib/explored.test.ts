import { describe, expect, test } from 'vitest';
import { MASK_BYTES, cellOf, decodeExplored, emptyMask, isExplored, roundHalfEven, setCell } from './explored';

/** base64 of gzip of `bytes`, as the server sends `explored.bits`. */
async function gzipBase64(bytes: Uint8Array<ArrayBuffer>): Promise<string> {
	const stream = new Blob([bytes]).stream().pipeThrough(new CompressionStream('gzip'));
	const gz = new Uint8Array(await new Response(stream).arrayBuffer());
	let s = '';
	for (const b of gz) s += String.fromCharCode(b);
	return btoa(s);
}

describe('cells', () => {
	test('banker’s rounding, as the game and the server', () => {
		expect([0.5, 1.5, 2.5, -0.5, -1.5, -2.5, 2.4, -2.6].map(roundHalfEven)).toEqual([0, 2, 2, 0, -2, -2, 2, -3]);
		expect(cellOf(0, 0)).toEqual([1024, 1024]);
		expect(cellOf(6, -6)).toEqual([1024, 1024]);
		expect(cellOf(18, -18)).toEqual([1026, 1022]);
		expect(cellOf(11.9, -12.1)).toEqual([1025, 1023]);
	});

	test('isExplored reads the bit under a point; off-grid is unexplored', () => {
		const m = emptyMask();
		setCell(m, 1025, 1024);
		expect(m[(1024 * 2048 + 1025) >> 3]).toBe(1 << 1);
		expect(isExplored(m, 12, 0)).toBe(true);
		expect(isExplored(m, 17.9, 5.9)).toBe(true);
		expect(isExplored(m, 0, 0)).toBe(false);
		expect(isExplored(m, 30000, 0)).toBe(false);
		setCell(m, -1, 0); // ignored
		expect(m.every((b, i) => i === (1024 * 2048 + 1025) >> 3 || b === 0)).toBe(true);
	});
});

describe('decodeExplored', () => {
	test('decodes gzip+base64 bits', async () => {
		const m = emptyMask();
		setCell(m, 3, 0);
		setCell(m, 2047, 2047);
		const bits = await gzipBase64(m);
		const out = await decodeExplored({ source: 'tables', cell: 12, size: 2048, bits });
		expect(out?.length).toBe(MASK_BYTES);
		expect(out?.[0]).toBe(1 << 3);
		expect(out?.[MASK_BYTES - 1]).toBe(0x80);
	});

	test('rejects other grids, bad base64, bad gzip and short bitsets', async () => {
		const ok = await gzipBase64(emptyMask());
		expect(await decodeExplored(undefined)).toBeUndefined();
		expect(await decodeExplored({ source: 'zones', cell: 64, size: 2048, bits: ok })).toBeUndefined();
		expect(await decodeExplored({ source: 'zones', cell: 12, size: 1024, bits: ok })).toBeUndefined();
		expect(await decodeExplored({ source: 'zones', cell: 12, size: 2048, bits: '!!!' })).toBeUndefined();
		expect(await decodeExplored({ source: 'zones', cell: 12, size: 2048, bits: btoa('plain') })).toBeUndefined();
		const short = await gzipBase64(new Uint8Array(10));
		expect(await decodeExplored({ source: 'zones', cell: 12, size: 2048, bits: short })).toBeUndefined();
	});
});
