import { describe, expect, test } from 'vitest';
import { nextFocusIndex, resolveReturnTarget } from './focusTrap';

describe('nextFocusIndex', () => {
	test('moves forward and wraps', () => {
		expect(nextFocusIndex(0, 3, false)).toBe(1);
		expect(nextFocusIndex(2, 3, false)).toBe(0);
	});
	test('moves backward and wraps', () => {
		expect(nextFocusIndex(1, 3, true)).toBe(0);
		expect(nextFocusIndex(0, 3, true)).toBe(2);
	});
	test('enters from outside at the first or last', () => {
		expect(nextFocusIndex(-1, 3, false)).toBe(0);
		expect(nextFocusIndex(-1, 3, true)).toBe(2);
		expect(nextFocusIndex(5, 3, false)).toBe(0);
	});
	test('single and empty lists', () => {
		expect(nextFocusIndex(0, 1, false)).toBe(0);
		expect(nextFocusIndex(0, 1, true)).toBe(0);
		expect(nextFocusIndex(-1, 0, false)).toBe(-1);
	});
});

describe('resolveReturnTarget', () => {
	const el = (connected: boolean) => ({ isConnected: connected }) as unknown as HTMLElement;
	test('prefers a connected returnTo element over the captured opener', () => {
		const r = el(true);
		expect(resolveReturnTarget(r, el(true))).toBe(r);
	});
	test('accepts a getter, called at close time', () => {
		const r = el(true);
		expect(resolveReturnTarget(() => r, null)).toBe(r);
	});
	test('falls back to the captured opener when returnTo is missing or detached', () => {
		const c = el(true);
		expect(resolveReturnTarget(undefined, c)).toBe(c);
		expect(resolveReturnTarget(el(false), c)).toBe(c);
		expect(resolveReturnTarget(() => null, c)).toBe(c);
	});
	test('null when neither is connected', () => {
		expect(resolveReturnTarget(el(false), el(false))).toBeNull();
		expect(resolveReturnTarget(undefined, null)).toBeNull();
	});
});
