const MINUTE_MS = 60_000;
const HOUR_MS = 60 * MINUTE_MS;
const DAY_MS = 24 * HOUR_MS;
const MISSING = "\u2014";

// Largest unit first, so the first unit that fits the elapsed time is the one shown.
const UNITS: Array<{ unit: Intl.RelativeTimeFormatUnit; ms: number }> = [
  { unit: "year", ms: 365 * DAY_MS },
  { unit: "month", ms: 30 * DAY_MS },
  { unit: "day", ms: DAY_MS },
  { unit: "hour", ms: HOUR_MS },
  { unit: "minute", ms: MINUTE_MS },
];

// A fixed locale keeps "3 days ago" stable between the server render and the client hydration.
const relativeFormat = new Intl.RelativeTimeFormat("en-US", { numeric: "always" });

/** Short "3 days ago" text for an RFC 3339 timestamp. Returns an em dash when the value is missing or invalid. */
export function formatRelativeTime(value: string | undefined, now: number = Date.now()): string {
  if (!value) return MISSING;
  const then = Date.parse(value);
  if (Number.isNaN(then)) return MISSING;

  // A timestamp slightly ahead of this clock (skew between hosts) reads as just now, not as a future time.
  const elapsed = now - then;
  if (elapsed < MINUTE_MS) return "just now";
  for (const { unit, ms } of UNITS) {
    if (elapsed >= ms) return relativeFormat.format(-Math.floor(elapsed / ms), unit);
  }
  return "just now";
}

/** Full locale date-time for a tooltip. Returns an empty string when the value is missing or invalid. */
export function formatFullDateTime(value: string | undefined): string {
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "" : date.toLocaleString();
}
