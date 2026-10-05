// Dates and clock times in a server's log time zone (Plan 7): profiles and
// the activity timeline count days the way the server does, wherever the
// viewer is. Built on Intl with a cached formatter per zone; an unknown
// zone falls back to UTC.

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const WEEKDAYS_FULL = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

export interface Zoned {
	y: number;
	/** 1–12 */
	m: number;
	d: number;
	hh: number;
	mm: number;
	/** 0 = Sunday */
	wd: number;
}

const formatters = new Map<string, Intl.DateTimeFormat>();

function formatter(tz: string): Intl.DateTimeFormat {
	let f = formatters.get(tz);
	if (!f) {
		const opts: Intl.DateTimeFormatOptions = {
			year: 'numeric',
			month: 'numeric',
			day: 'numeric',
			hour: '2-digit',
			minute: '2-digit',
			hourCycle: 'h23'
		};
		try {
			f = new Intl.DateTimeFormat('en-US', { ...opts, timeZone: tz });
		} catch {
			f = new Intl.DateTimeFormat('en-US', { ...opts, timeZone: 'UTC' });
		}
		formatters.set(tz, f);
	}
	return f;
}

/** The wall-clock fields of `at` in `tz`. */
export function zoned(at: string | Date, tz: string): Zoned {
	const parts: Record<string, number> = {};
	for (const p of formatter(tz).formatToParts(new Date(at))) {
		if (p.type !== 'literal') parts[p.type] = Number(p.value);
	}
	const wd = new Date(Date.UTC(parts.year, parts.month - 1, parts.day)).getUTCDay();
	return { y: parts.year, m: parts.month, d: parts.day, hh: parts.hour, mm: parts.minute, wd };
}

const pad2 = (n: number) => String(n).padStart(2, '0');

/** "2026-10-05": the local date, for grouping and comparing days. */
export function dayKey(at: string | Date, tz: string): string {
	const z = zoned(at, tz);
	return `${z.y}-${pad2(z.m)}-${pad2(z.d)}`;
}

/** The day before `key` ("2026-10-01" → "2026-09-30"). */
export function prevDayKey(key: string): string {
	const d = new Date(`${key}T12:00:00Z`);
	d.setUTCDate(d.getUTCDate() - 1);
	return d.toISOString().slice(0, 10);
}

/** "14:20" */
export function zClock(at: string | Date, tz: string): string {
	const z = zoned(at, tz);
	return `${pad2(z.hh)}:${pad2(z.mm)}`;
}

/** "30 Sep 2026" */
export function zDate(at: string | Date, tz: string): string {
	const z = zoned(at, tz);
	return `${z.d} ${MONTHS[z.m - 1]} ${z.y}`;
}

/** "Tue 29 Sep" */
export function zWeekday(at: string | Date, tz: string): string {
	const z = zoned(at, tz);
	return `${WEEKDAYS[z.wd]} ${z.d} ${MONTHS[z.m - 1]}`;
}

/** "30 Sep" */
export function zDayMonth(at: string | Date, tz: string): string {
	const z = zoned(at, tz);
	return `${z.d} ${MONTHS[z.m - 1]}`;
}

/** "today 14:20" | "yesterday 14:20" | "28 Sep 14:20" */
export function zDayRef(at: string | Date, tz: string, now: Date): string {
	const k = dayKey(at, tz);
	const today = dayKey(now, tz);
	const clock = zClock(at, tz);
	if (k === today) return `today ${clock}`;
	if (k === prevDayKey(today)) return `yesterday ${clock}`;
	return `${zDayMonth(at, tz)} ${clock}`;
}

/** The weekday initial of a local date key: "2026-10-05" → "M". */
export function weekdayInitial(key: string): string {
	return WEEKDAYS[new Date(`${key}T12:00:00Z`).getUTCDay()][0];
}

/** An unambiguous label for a local date key (fix round 1): "2026-09-29" → "Tuesday 29 Sep". */
export function fullDayLabel(key: string): string {
	const d = new Date(`${key}T12:00:00Z`);
	return `${WEEKDAYS_FULL[d.getUTCDay()]} ${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]}`;
}
