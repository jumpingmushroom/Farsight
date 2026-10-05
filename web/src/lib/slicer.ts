// Time-sliced work (perf: no marker render may block the main thread for
// more than about a frame). `run` takes a list of tasks, runs as many as fit
// in the budget at once, and the rest in later animation frames, `budgetMs`
// each. Every slice runs at least one task, so the queue always drains.
// A new `run` (or `cancel`) drops whatever the previous one hadn't done
// yet, and its pending frame: never more than one frame is queued.

export interface FrameScheduler {
	now(): number;
	request(cb: () => void): number;
	cancel(id: number): void;
}

export const browserScheduler: FrameScheduler = {
	now: () => performance.now(),
	request: (cb) => requestAnimationFrame(cb),
	cancel: (id) => cancelAnimationFrame(id)
};

export interface Slicer {
	/** Replaces the queue with `tasks` and starts on it; `startedAt` is when this frame's work began. */
	run(tasks: (() => void)[], startedAt?: number): void;
	cancel(): void;
	/** Tasks not yet run. */
	readonly pending: number;
}

export function createSlicer(sched: FrameScheduler = browserScheduler, budgetMs = 8): Slicer {
	let queue: (() => void)[] = [];
	let at = 0;
	let frame = 0;
	// Bumped by every run/cancel, so a slice notices when a task superseded it.
	let gen = 0;

	function slice(start: number): void {
		const mine = gen;
		do {
			queue[at++]();
			if (gen !== mine) return;
		} while (at < queue.length && sched.now() - start < budgetMs);
		if (at < queue.length) {
			frame = sched.request(() => {
				frame = 0;
				slice(sched.now());
			});
		} else {
			queue = [];
			at = 0;
		}
	}

	function cancel(): void {
		gen++;
		if (frame) sched.cancel(frame);
		frame = 0;
		queue = [];
		at = 0;
	}

	return {
		run(tasks, startedAt) {
			cancel();
			if (tasks.length === 0) return;
			queue = tasks;
			slice(startedAt ?? sched.now());
		},
		cancel,
		get pending() {
			return queue.length - at;
		}
	};
}
