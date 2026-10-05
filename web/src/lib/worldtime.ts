// The pure client-side world-time model (Plan 9): estimates the game's
// netTime now from the server's `clock` anchor, and derives the clock
// face, phase label, "next" text, current weather period and night/evening
// map tint from it. Built against docs/superpowers/specs/2026-10-05-time-and-weather-design.md
// and the plan's global constraints (exact formulas, thresholds, copy).

import type { Clock, Weather } from './types';

/** A game day is 1800 s. */
const DAY_SEC = 1800;

export type Phase = 'morning' | 'day' | 'evening' | 'night';

/** netTime now: the anchor plus seconds elapsed since `at`, counted only while `clock.running`. */
export function netTimeNow(clock: Clock, now: Date): number {
	if (!clock.running) return clock.netTime;
	const elapsed = Math.max(0, (now.getTime() - new Date(clock.at).getTime()) / 1000);
	return clock.netTime + elapsed;
}

/** The raw day fraction (0..1): `(netTime mod 1800) / 1800`. */
export function dayFraction(t: number): number {
	const mod = t % DAY_SEC;
	return (mod < 0 ? mod + DAY_SEC : mod) / DAY_SEC;
}

/**
 * The rescaled clock fraction (0..1, sun position) from the raw day
 * fraction: raw<0.15 -> f/0.15*0.25; 0.15-0.85 -> 0.25+(f-0.15)/0.7*0.5;
 * >0.85 -> 0.75+(f-0.85)/0.15*0.25.
 */
export function clockFraction(raw: number): number {
	if (raw < 0.15) return (raw / 0.15) * 0.25;
	if (raw > 0.85) return 0.75 + ((raw - 0.85) / 0.15) * 0.25;
	return 0.25 + ((raw - 0.15) / 0.7) * 0.5;
}

/** The inverse of `clockFraction`: the raw day fraction for a given clock fraction (0..1). */
function invClockFraction(cf: number): number {
	if (cf < 0.25) return (cf / 0.25) * 0.15;
	if (cf > 0.75) return 0.85 + ((cf - 0.75) / 0.25) * 0.15;
	return 0.15 + ((cf - 0.25) / 0.5) * 0.7;
}

function pad2(n: number): string {
	return n < 10 ? `0${n}` : String(n);
}

/** "HH:MM" from the rescaled clock fraction. */
export function clockText(t: number): string {
	const totalMin = Math.round(clockFraction(dayFraction(t)) * 24 * 60) % (24 * 60);
	return `${pad2(Math.floor(totalMin / 60))}:${pad2(totalMin % 60)}`;
}

/** Morning 06:00-11:00, Day 11:00-16:00, Evening 16:00-18:00, Night 18:00-06:00, by the rescaled clock. */
export function phase(t: number): Phase {
	const hour = clockFraction(dayFraction(t)) * 24;
	if (hour >= 6 && hour < 11) return 'morning';
	if (hour >= 11 && hour < 16) return 'day';
	if (hour >= 16 && hour < 18) return 'evening';
	return 'night';
}

const NEXT_PHASE: Record<Phase, { label: string; hour: number }> = {
	morning: { label: 'Midday', hour: 11 },
	day: { label: 'Evening', hour: 16 },
	evening: { label: 'Night', hour: 18 },
	night: { label: 'Dawn', hour: 6 }
};

/** "{Midday|Evening|Night|Dawn} in ~N min" to the current phase's next boundary, N real minutes rounded up. */
export function nextText(t: number): string {
	const raw = dayFraction(t);
	const { label, hour } = NEXT_PHASE[phase(t)];
	const targetRaw = invClockFraction(hour / 24);
	const deltaRaw = ((targetRaw - raw) % 1 + 1) % 1;
	const min = Math.ceil((deltaRaw * DAY_SEC) / 60);
	return `${label} in ~${min} min`;
}

/** The game day number: `floor(netTime / 1800)`. */
export function dayOf(t: number): number {
	return Math.floor(t / DAY_SEC);
}

/** Which of `weather.periods` (0, 1 or 2) covers netTime `t`; -1 outside the three periods. */
export function periodIndex(weather: Weather, t: number): 0 | 1 | 2 | -1 {
	if (weather.periods.length === 0) return -1;
	const current = Math.floor(t / weather.periodSec);
	const first = Math.floor(weather.periods[0].start / weather.periodSec);
	const index = current - first;
	return index >= 0 && index < weather.periods.length ? (index as 0 | 1 | 2) : -1;
}

/** The map tile tint for a phase: night and evening only. */
export function tintOf(ph: Phase): 'night' | 'evening' | undefined {
	if (ph === 'night') return 'night';
	if (ph === 'evening') return 'evening';
	return undefined;
}
