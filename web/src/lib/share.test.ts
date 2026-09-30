import { describe, expect, test } from 'vitest';
import { hashFor, parseHash } from './share';

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
