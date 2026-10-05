// The player profile's view model (Plan 7, design `:116-185`): status line,
// stat tiles, the 7-day chart, first/last seen, beds and the section
// lines. Times and days use the server's zone (profile.timeZone).

import { fmtInt, fmtLastSeen, fmtSession } from './format';
import type { Profile } from './types';
import { fullDayLabel, weekdayInitial, zDate, zDayRef } from './zoned';

export interface ProfileDayBar {
	/** Weekday initial: "M". */
	d: string;
	/** Hours, "" for none: "2.5", "3", "<0.1". */
	label: string;
	/** Bar height, % of the chart (the busiest day is 78). */
	pct: number;
	today: boolean;
	/** An unambiguous accessible label (fix round 1): "Tuesday 29 Sep: 2.7 h", ", today" appended for today's bar. */
	aria: string;
}

export interface ProfileView {
	initial: string;
	status: string;
	stats: { k: string; v: string }[];
	days: ProfileDayBar[];
	first: string;
	last: string;
	beds: string;
	tracked: string;
	portalCount: string;
	tameCount: string;
	deathLine: string;
	tombs: { id: string; text: string; x: number; z: number }[];
	bases: { id: string; name: string; sub: string; x: number; z: number }[];
}

const pad2 = (n: number) => String(n).padStart(2, '0');

/** Playtime: "38m", "9h 05m"; with `big`, ten hours and up as "212h". */
export function fmtPlaytime(sec: number, big = false): string {
	const h = Math.floor(sec / 3600);
	const m = Math.floor((sec % 3600) / 60);
	if (h === 0) return `${m}m`;
	if (big && h >= 10) return `${h}h`;
	return `${h}h ${pad2(m)}m`;
}

/** A chart label: hours to one decimal, whole hours bare, "" for none. */
export function hoursLabel(sec: number): string {
	if (sec <= 0) return '';
	const h = Math.round((sec / 3600) * 10) / 10;
	if (h === 0) return '<0.1';
	return Number.isInteger(h) ? String(h) : h.toFixed(1);
}

/** "2.7 h", or "0 hours" (spoken out, unlike the bare "" chart label). */
function hoursSpoken(sec: number): string {
	const label = hoursLabel(sec);
	return label ? `${label} h` : '0 hours';
}

export function dayBars(days: Profile['days']): ProfileDayBar[] {
	const max = Math.max(...days.map((d) => d.seconds / 3600), 1);
	return days.map((d, i) => {
		const today = i === days.length - 1;
		return {
			d: weekdayInitial(d.date),
			label: hoursLabel(d.seconds),
			pct: Math.round((d.seconds / 3600 / max) * 78),
			today,
			aria: `${fullDayLabel(d.date)}: ${hoursSpoken(d.seconds)}${today ? ', today' : ''}`
		};
	});
}

export function profileView(p: Profile, now: Date): ProfileView {
	const tz = p.timeZone;
	const sessionSec = p.since ? Math.max(0, (now.getTime() - new Date(p.since).getTime()) / 1000) : 0;
	const lastSeen = p.lastSeen ? fmtLastSeen(p.lastSeen, now) : '';
	return {
		initial: Array.from(p.name.trim())[0]?.toUpperCase() ?? '?',
		status: p.online ? `Online now · ${fmtSession(sessionSec)}` : lastSeen.charAt(0).toUpperCase() + lastSeen.slice(1),
		stats: [
			{ k: 'This week', v: fmtPlaytime(p.weekSeconds) },
			{ k: 'All time', v: fmtPlaytime(p.allSeconds, true) },
			{ k: 'Sessions', v: fmtInt(p.sessions) }
		],
		days: dayBars(p.days),
		first: zDate(p.firstSeen, tz),
		last: p.online ? 'Online now' : p.lastSeen ? zDayRef(p.lastSeen, tz, now) : '—',
		beds: p.beds.count ? `${p.beds.count} · ${p.beds.near.join(', ')}` : 'None placed',
		tracked: `All time and first seen count from ${zDate(p.trackedSince, tz)}, when tracking began.`,
		portalCount: `${p.portals.length} ${p.portals.length === 1 ? 'portal' : 'portals'}`,
		tameCount: p.tames.length ? String(p.tames.length) : '',
		deathLine: `${p.deaths.spotted} spotted · ${p.deaths.week} this week`,
		tombs: p.deaths.tombstones.map((t) => ({
			id: t.id,
			text: `Tombstone in the ${t.biome} · since save ${zDayRef(t.firstSeen, tz, now).replace(/^today /, '')}`,
			x: t.x,
			z: t.z
		})),
		bases: p.bases
			.slice()
			.sort((a, b) => b.pieces - a.pieces)
			.map((b) => ({ id: b.id, name: b.name, sub: `${fmtInt(b.pieces)} pieces · ${b.biome}`, x: b.x, z: b.z }))
	};
}
