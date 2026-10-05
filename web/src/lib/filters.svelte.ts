// The activity filters (Plan 7), shared by the timeline and the side
// panel's short activity list: categories switched off, and the players
// chosen (none = everyone).

import type { Category } from './types';

export class ActivityFilters {
	off = $state<Category[]>([]);
	people = $state<string[]>([]);

	toggle(c: Category): void {
		this.off = this.off.includes(c) ? this.off.filter((x) => x !== c) : [...this.off, c];
	}

	togglePerson(id: string): void {
		this.people = this.people.includes(id) ? this.people.filter((x) => x !== id) : [...this.people, id];
	}

	everyone(): void {
		this.people = [];
	}

	reset(): void {
		this.off = [];
		this.people = [];
	}

	get active(): boolean {
		return this.off.length > 0 || this.people.length > 0;
	}
}

export const filters = new ActivityFilters();
