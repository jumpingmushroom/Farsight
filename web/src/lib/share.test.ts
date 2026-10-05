import { describe, expect, test } from 'vitest';
import { hashFor, parseHash, sameView } from './share';

describe('parseHash', () => {
	test('server and key', () => {
		expect(parseHash('#s=demo&k=a%20b')).toEqual({ server: 'demo', key: 'a b' });
	});
	test('server only', () => {
		expect(parseHash('#s=x')).toEqual({ server: 'x' });
	});
	test('empty hash', () => {
		expect(parseHash('')).toEqual({});
	});
	test('junk', () => {
		expect(parseHash('#total-nonsense')).toEqual({});
	});
	test('a "+"-encoded value decodes to a space', () => {
		expect(parseHash('#s=x&k=a+b')).toEqual({ server: 'x', key: 'a b' });
	});
	test('duplicate keys: the first wins', () => {
		expect(parseHash('#s=first&s=second')).toEqual({ server: 'first' });
	});
});

describe('hashFor', () => {
	test('escapes the server id', () => {
		expect(hashFor('a b')).toBe('#s=a%20b');
	});
});

describe('views (Plan 7)', () => {
	test('a profile', () => {
		expect(parseHash('#s=demo&p=76561190000000001')).toEqual({
			server: 'demo',
			view: { kind: 'profile', player: '76561190000000001' }
		});
		expect(hashFor('demo', { kind: 'profile', player: 'a b' })).toBe('#s=demo&p=a%20b');
	});
	test('the activity timeline', () => {
		expect(parseHash('#s=demo&activity')).toEqual({ server: 'demo', view: { kind: 'activity' } });
		expect(hashFor('demo', { kind: 'activity' })).toBe('#s=demo&activity');
	});
	test('a profile wins over activity; an empty p is no view', () => {
		expect(parseHash('#s=d&activity&p=1').view).toEqual({ kind: 'profile', player: '1' });
		expect(parseHash('#s=d&p=').view).toBeUndefined();
	});
	test('round trip', () => {
		for (const v of [undefined, { kind: 'activity' } as const, { kind: 'profile', player: 'Xbox_2' } as const]) {
			expect(parseHash(hashFor('x', v)).view).toEqual(v);
		}
	});
	test('sameView', () => {
		expect(sameView(undefined, undefined)).toBe(true);
		expect(sameView({ kind: 'activity' }, undefined)).toBe(false);
		expect(sameView({ kind: 'profile', player: '1' }, { kind: 'profile', player: '1' })).toBe(true);
		expect(sameView({ kind: 'profile', player: '1' }, { kind: 'profile', player: '2' })).toBe(false);
	});
});
