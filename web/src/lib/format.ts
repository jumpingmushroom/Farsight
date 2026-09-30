// Formatters per DESIGN-NOTES §6 (Copy and formatting). Every time-relative
// formatter takes `now` explicitly and never reads the clock itself, so
// callers control determinism. Clock formatting uses local `Date` getters,
// so in production this shows the viewer's local time; tests pin TZ=UTC.

const MONTH_ABBR = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

function pad2(n: number): string {
	return String(n).padStart(2, '0');
}

function sameLocalDay(a: Date, b: Date): boolean {
	return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}

function isPreviousLocalDay(a: Date, b: Date): boolean {
	const prev = new Date(b.getFullYear(), b.getMonth(), b.getDate() - 1);
	return a.getFullYear() === prev.getFullYear() && a.getMonth() === prev.getMonth() && a.getDate() === prev.getDate();
}

function monthDay(d: Date): string {
	return `${d.getDate()} ${MONTH_ABBR[d.getMonth()]}`;
}

/**
 * −1,234 style: en-US grouping and a U+2212 minus sign. Deliberately departs
 * from the literal §6.2 formula in one respect: a value that rounds to zero
 * (e.g. −0.4) is normalised to "0" rather than "−0" — a negative zero reads
 * as a rendering bug, not a real negative quantity.
 */
export function fmtN(n: number): string {
	const rounded = Math.round(n);
	if (rounded === 0) return '0';
	return (rounded < 0 ? '−' : '') + Math.abs(rounded).toLocaleString('en-US');
}

/** Plain non-negative integer formatting: 2,184. */
export function fmtInt(n: number): string {
	return Math.round(n).toLocaleString('en-US');
}

/** "318742" -> "318 742" (3+3 grouping). */
export function fmtCode(code: string): string {
	if (code.length !== 6) return code;
	return `${code.slice(0, 3)} ${code.slice(3)}`;
}

/** 24-hour HH:MM in the viewer's local time zone. */
export function fmtClock(iso: string): string {
	const d = new Date(iso);
	return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

function compactDuration(sec: number): string {
	const days = Math.floor(sec / 86400);
	if (days >= 1) {
		const hours = Math.floor((sec % 86400) / 3600);
		return `${days}d ${hours}h`;
	}
	const hours = Math.floor(sec / 3600);
	const minutes = Math.floor((sec % 3600) / 60);
	if (hours >= 1) return `${hours}h ${minutes}m`;
	return `${minutes}m`;
}

/** 1h 12m | 38m | 1d 3h */
export function fmtSession(sec: number): string {
	return compactDuration(sec);
}

/** 3d 6h | 5h 12m | 12m */
export function fmtUptime(sec: number): string {
	return compactDuration(sec);
}

/** "5 min", "1 h 12 m", "28 Sep" */
export function fmtActivityTime(iso: string, now: Date): string {
	const diffSec = Math.max(0, (now.getTime() - new Date(iso).getTime()) / 1000);
	const minTotal = Math.floor(diffSec / 60);
	if (minTotal < 60) {
		return `${Math.max(1, minTotal)} min`;
	}
	const hoursTotal = Math.floor(diffSec / 3600);
	if (hoursTotal < 24) {
		const m = minTotal - hoursTotal * 60;
		return m === 0 ? `${hoursTotal} h` : `${hoursTotal} h ${m} m`;
	}
	return monthDay(new Date(iso));
}

/** "last seen 24 min ago" | "last seen 3 h ago" | "last seen yesterday" | "last seen 28 Sep" */
export function fmtLastSeen(iso: string, now: Date): string {
	const at = new Date(iso);
	// The previous-calendar-day check runs before the "< 60 min" branch: a
	// midnight crossing (e.g. 23:50 -> 00:10, 20 minutes apart) must read
	// "yesterday", not "20 min ago" — the day boundary takes priority over
	// every other branch, not just over "{h} h ago".
	if (isPreviousLocalDay(at, now)) {
		return 'last seen yesterday';
	}
	const diffSec = Math.max(0, (now.getTime() - at.getTime()) / 1000);
	const minTotal = Math.floor(diffSec / 60);
	if (minTotal < 60) {
		return `last seen ${minTotal} min ago`;
	}
	if (sameLocalDay(at, now)) {
		const hours = Math.floor(diffSec / 3600);
		return `last seen ${hours} h ago`;
	}
	return `last seen ${monthDay(at)}`;
}

/** "just now" | "12 min ago" | "3 h 41 min ago" */
export function fmtMapAge(minutes: number): string {
	if (minutes < 1) return 'just now';
	if (minutes < 60) return `${Math.floor(minutes)} min ago`;
	const h = Math.floor(minutes / 60);
	const m = Math.floor(minutes % 60);
	return `${h} h ${m} min ago`;
}

/** "today 03:12" | "yesterday 03:12" | "28 Sep 03:12" */
export function fmtDayRef(iso: string, now: Date): string {
	const d = new Date(iso);
	const clock = fmtClock(iso);
	if (sameLocalDay(d, now)) return `today ${clock}`;
	if (isPreviousLocalDay(d, now)) return `yesterday ${clock}`;
	return `${monthDay(d)} ${clock}`;
}

/** 12.4% */
export function fmtPct(p: number, decimals = 1): string {
	return `${p.toFixed(decimals)}%`;
}

/** "2.5 km" */
export function fmtKm(m: number): string {
	return `${(m / 1000).toFixed(1)} km`;
}

const ROMAN = ['', 'I', 'II', 'III', 'IV', 'V', 'VI', 'VII', 'VIII'];

/** 1..8 -> I..VIII */
export function roman(n: number): string {
	return ROMAN[n] ?? String(n);
}
