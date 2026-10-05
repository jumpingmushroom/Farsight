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
	const groups: ClusterGroup[] = [];
	const r2 = radius * radius;
	// Only the distance squared matters (a negative radius acts as its size);
	// a zero or NaN one joins nothing.
	if (!(r2 > 0)) return points.map((p) => ({ ids: [p.id], x: p.x, y: p.y }));
	// The first matching group in creation order, found through a grid of
	// radius-sized cells over the unlocked groups' running centroids: a
	// centroid strictly within `radius` of p is in p's cell or a neighbour,
	// so only those are checked (the linear scan was O(points × groups)).
	// Cells a hair wider than the radius, so float rounding in the cell
	// division can never put a matching centroid two cells away.
	const cell = Math.abs(radius) * (1 + 1e-9);
	const cells = new Map<number, number[]>();
	const cellOf = new Map<number, number>(); // group index → its cell key
	const key = (cx: number, cy: number) => cx * 1_000_003 + cy;
	const place = (gi: number) => {
		const g = groups[gi];
		const k = key(Math.floor(g.x / cell), Math.floor(g.y / cell));
		const was = cellOf.get(gi);
		if (was === k) return;
		if (was !== undefined) {
			const list = cells.get(was)!;
			list.splice(list.indexOf(gi), 1);
		}
		const list = cells.get(k);
		if (list) list.push(gi);
		else cells.set(k, [gi]);
		cellOf.set(gi, k);
	};
	for (const p of points) {
		if (p.id === lockedId) {
			groups.push({ ids: [p.id], x: p.x, y: p.y });
			continue;
		}
		const cx = Math.floor(p.x / cell);
		const cy = Math.floor(p.y / cell);
		let best = -1;
		for (let dx = -1; dx <= 1; dx++) {
			for (let dy = -1; dy <= 1; dy++) {
				const list = cells.get(key(cx + dx, cy + dy));
				if (!list) continue;
				for (const gi of list) {
					if (best !== -1 && gi > best) continue;
					const g = groups[gi];
					if ((g.x - p.x) ** 2 + (g.y - p.y) ** 2 < r2) best = gi;
				}
			}
		}
		if (best === -1) {
			groups.push({ ids: [p.id], x: p.x, y: p.y });
			place(groups.length - 1);
			continue;
		}
		const g = groups[best];
		const n = g.ids.length;
		g.x = (g.x * n + p.x) / (n + 1);
		g.y = (g.y * n + p.y) / (n + 1);
		g.ids.push(p.id);
		place(best);
	}
	return groups;
}
