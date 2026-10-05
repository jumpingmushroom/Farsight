import { describe, expect, test } from 'vitest';
import { ActivityFilters } from './filters.svelte';

describe('ActivityFilters', () => {
	test('toggles categories and people; reset clears both', () => {
		const f = new ActivityFilters();
		expect(f.active).toBe(false);
		f.toggle('server');
		f.toggle('death');
		f.toggle('server');
		expect(f.off).toEqual(['death']);
		f.togglePerson('111');
		f.togglePerson('222');
		f.togglePerson('111');
		expect(f.people).toEqual(['222']);
		expect(f.active).toBe(true);
		f.everyone();
		expect(f.people).toEqual([]);
		f.reset();
		expect(f.off).toEqual([]);
		expect(f.active).toBe(false);
	});
});
