// The one date formatter (docs/agents/ui-conventions.md, Money and dates): long
// style, the month never abbreviated ("3 September 2026"). A business date is a
// `YYYY-MM-DD` string from the server and stays one: it is never parsed into a
// Date, which would read it as UTC and shift the day (CLAUDE.md rule 5).

const MONTHS: Record<string, readonly string[]> = {
  "en-GB": [
    "January",
    "February",
    "March",
    "April",
    "May",
    "June",
    "July",
    "August",
    "September",
    "October",
    "November",
    "December",
  ],
  "id-ID": [
    "Januari",
    "Februari",
    "Maret",
    "April",
    "Mei",
    "Juni",
    "Juli",
    "Agustus",
    "September",
    "Oktober",
    "November",
    "Desember",
  ],
};

const ISO_DATE = /^(\d{4})-(\d{2})-(\d{2})$/;

function parts(iso: string): { year: string; month: number; day: number } | null {
  const m = ISO_DATE.exec(iso);
  if (!m) return null;
  const year = Number(m[1]);
  const month = Number(m[2]);
  const day = Number(m[3]);
  if (month < 1 || month > 12 || day < 1) return null;
  const leap = (year % 4 === 0 && year % 100 !== 0) || year % 400 === 0;
  const length = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1]!;
  return day > length ? null : { year: m[1]!, month, day };
}

/** "3 September 2026". Anything that is not a real `YYYY-MM-DD` comes back as given. */
export function formatDate(iso: string, locale: string, withYear = true): string {
  const p = parts(iso);
  if (!p) return iso;
  const months = MONTHS[locale] ?? MONTHS["en-GB"]!;
  return `${p.day} ${months[p.month - 1]}${withYear ? ` ${p.year}` : ""}`;
}

/** The two ends of a window, the year said once when both ends share it. */
export function formatDateRange(
  start: string,
  end: string,
  locale: string,
): { start: string; end: string } {
  const a = parts(start);
  const b = parts(end);
  const sameYear = a !== null && b !== null && a.year === b.year;
  return { start: formatDate(start, locale, !sameYear), end: formatDate(end, locale) };
}

const WEEKDAYS: Record<string, readonly string[]> = {
  "en-GB": ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"],
  "id-ID": ["Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"],
};

/** "Saturday, 22 November", and the year too when `withYear`. Anything that is
 * not a real `YYYY-MM-DD` comes back as given. The weekday is calendar
 * arithmetic on the date's own parts, never the device's clock or zone. */
export function formatWeekdayDate(iso: string, locale: string, withYear = false): string {
  const p = parts(iso);
  if (!p) return iso;
  const weekday = new Date(Date.UTC(Number(p.year), p.month - 1, p.day)).getUTCDay();
  const names = WEEKDAYS[locale] ?? WEEKDAYS["en-GB"]!;
  return `${names[weekday]}, ${formatDate(iso, locale, withYear)}`;
}

/** The calendar day before a `YYYY-MM-DD`, or the input when it is not one. */
export function previousDay(iso: string): string {
  const p = parts(iso);
  if (!p) return iso;
  return new Date(Date.UTC(Number(p.year), p.month - 1, p.day - 1)).toISOString().slice(0, 10);
}

/** A business date's year, or null when it is not a `YYYY-MM-DD`. */
export function yearOf(iso: string): string | null {
  return parts(iso)?.year ?? null;
}
