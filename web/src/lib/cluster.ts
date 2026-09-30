// The design's greedy single-pass clusterer (DESIGN-NOTES §5.1), in screen
// space. Each point joins the first unlocked group whose running centroid is
// strictly within `radius`; the locked id (the selected marker) forms its own
// singleton that nothing joins. Input order decides group creation, so callers
// pass markers in their stable order.

export interface ClusterPoint {
	id: string;
	x: number;
	y: number;
}

export interface ClusterGroup {
	ids: string[];
	x: number;
	y: number;
}

export function cluster(points: ClusterPoint[], radius: number, lockedId?: string): ClusterGroup[] {
	const groups: (ClusterGroup & { locked: boolean })[] = [];
	const r2 = radius * radius;
	for (const p of points) {
		if (p.id === lockedId) {
			groups.push({ ids: [p.id], x: p.x, y: p.y, locked: true });
			continue;
		}
		const g = groups.find((g) => !g.locked && (g.x - p.x) ** 2 + (g.y - p.y) ** 2 < r2);
		if (!g) {
			groups.push({ ids: [p.id], x: p.x, y: p.y, locked: false });
			continue;
		}
		const n = g.ids.length;
		g.x = (g.x * n + p.x) / (n + 1);
		g.y = (g.y * n + p.y) / (n + 1);
		g.ids.push(p.id);
	}
	return groups.map(({ ids, x, y }) => ({ ids, x, y }));
}
