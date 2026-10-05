import './testing/leaflet-node';
import { describe, expect, test } from 'vitest';
import L from 'leaflet';
import { cancelOn, createSlicer, type FrameScheduler } from './slicer';

/** A fake clock and rAF: frames run only when the test calls `frame()`. */
function fakeScheduler() {
	let t = 0;
	let next = 1;
	const queued = new Map<number, () => void>();
	const sched: FrameScheduler = {
		now: () => t,
		request: (cb) => {
			const id = next++;
			queued.set(id, cb);
			return id;
		},
		cancel: (id) => void queued.delete(id)
	};
	return {
		sched,
		advance: (ms: number) => void (t += ms),
		pendingFrames: () => queued.size,
		/** Runs the callbacks queued before this frame (like the browser). */
		frame: () => {
			const cbs = [...queued.values()];
			queued.clear();
			for (const cb of cbs) cb();
		}
	};
}

/** `n` tasks that each take `ms` on the fake clock and log their index. */
function tasks(f: ReturnType<typeof fakeScheduler>, n: number, ms: number, log: string[], tag: string) {
	return Array.from({ length: n }, (_, i) => () => {
		f.advance(ms);
		log.push(`${tag}${i}`);
	});
}

describe('createSlicer', () => {
	test('a large batch is spread over frames within the budget, in order', () => {
		const f = fakeScheduler();
		const s = createSlicer(f.sched, 8);
		const log: string[] = [];
		s.run(tasks(f, 50, 1, log, 'a'));
		// The first slice runs at once: 8 ms of 1 ms tasks.
		expect(log).toHaveLength(8);
		expect(s.pending).toBe(42);
		expect(f.pendingFrames()).toBe(1);
		let frames = 0;
		while (f.pendingFrames()) {
			const before = log.length;
			f.frame();
			frames++;
			expect(log.length - before).toBeLessThanOrEqual(8);
		}
		expect(frames).toBe(6);
		expect(log).toEqual(Array.from({ length: 50 }, (_, i) => `a${i}`));
		expect(s.pending).toBe(0);
	});

	test('the budget counts from `startedAt`, but a slice always makes progress', () => {
		const f = fakeScheduler();
		const s = createSlicer(f.sched, 8);
		const log: string[] = [];
		const start = f.sched.now();
		f.advance(30); // the render itself already took 30 ms
		s.run(tasks(f, 5, 1, log, 'a'), start);
		expect(log).toEqual(['a0']);
		f.frame();
		expect(log).toHaveLength(5);
		expect(f.pendingFrames()).toBe(0);
	});

	test('a task slower than the budget still runs alone in its frame', () => {
		const f = fakeScheduler();
		const s = createSlicer(f.sched, 8);
		const log: string[] = [];
		s.run(tasks(f, 3, 20, log, 'a'));
		expect(log).toEqual(['a0']);
		f.frame();
		expect(log).toEqual(['a0', 'a1']);
		f.frame();
		expect(log).toEqual(['a0', 'a1', 'a2']);
		expect(f.pendingFrames()).toBe(0);
	});

	test('a superseding run drops the old queue and its frame', () => {
		const f = fakeScheduler();
		const s = createSlicer(f.sched, 8);
		const log: string[] = [];
		s.run(tasks(f, 50, 1, log, 'a'));
		f.frame();
		expect(log).toHaveLength(16);
		s.run(tasks(f, 20, 1, log, 'b'));
		// Never more than one frame queued: the old one was cancelled.
		expect(f.pendingFrames()).toBe(1);
		while (f.pendingFrames()) f.frame();
		expect(log.filter((l) => l.startsWith('a'))).toHaveLength(16);
		expect(log.filter((l) => l.startsWith('b'))).toEqual(Array.from({ length: 20 }, (_, i) => `b${i}`));
	});

	test('cancel drops everything and leaves no frame', () => {
		const f = fakeScheduler();
		const s = createSlicer(f.sched, 8);
		const log: string[] = [];
		s.run(tasks(f, 50, 1, log, 'a'));
		s.cancel();
		expect(f.pendingFrames()).toBe(0);
		expect(s.pending).toBe(0);
		expect(log).toHaveLength(8);
	});

	test('an empty or small batch finishes synchronously with no frame', () => {
		const f = fakeScheduler();
		const s = createSlicer(f.sched, 8);
		const log: string[] = [];
		s.run([]);
		s.run(tasks(f, 3, 1, log, 'a'));
		expect(log).toEqual(['a0', 'a1', 'a2']);
		expect(f.pendingFrames()).toBe(0);
	});

	test('a task may supersede the run it belongs to', () => {
		const f = fakeScheduler();
		const s = createSlicer(f.sched, 8);
		const log: string[] = [];
		s.run([() => s.run(tasks(f, 2, 1, log, 'b')), ...tasks(f, 5, 1, log, 'a')]);
		while (f.pendingFrames()) f.frame();
		expect(log).toEqual(['b0', 'b1']);
	});

	test('cancelOn drops the queue when the event fires (a map zoomstart), until unbound', () => {
		const f = fakeScheduler();
		const s = createSlicer(f.sched, 8);
		const map = new (L.Evented as unknown as new () => L.Evented)();
		const log: string[] = [];
		const unbind = cancelOn(map, 'zoomstart', s);
		s.run(tasks(f, 50, 1, log, 'a'));
		expect(s.pending).toBe(42);
		map.fire('zoomstart');
		expect(s.pending).toBe(0);
		expect(f.pendingFrames()).toBe(0);
		expect(log).toHaveLength(8);
		unbind();
		s.run(tasks(f, 50, 1, log, 'b'));
		map.fire('zoomstart');
		expect(s.pending).toBe(42);
		expect(f.pendingFrames()).toBe(1);
	});
});
