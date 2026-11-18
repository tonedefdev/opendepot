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

// Fixed locale and UTC keep the pre-mount text identical on the server and in the first client render.
const stableDateFormat = new Intl.DateTimeFormat("en-US", { dateStyle: "medium", timeZone: "UTC" });
const stableDateTimeFormat = new Intl.DateTimeFormat("en-US", {
  dateStyle: "medium",
  timeStyle: "short",
  timeZone: "UTC",
});

/**
 * Short "3 days ago" text for an RFC 3339 timestamp. `now` is required so callers cannot read the clock during
 * server rendering. Returns an em dash when the value is missing or invalid.
 */
export function formatRelativeTime(value: string | undefined, now: number): string {
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

export interface LastUpdatedDisplay {
  /** Visible text. */
  text: string;
  /** Tooltip text. An empty string disables the tooltip. */
  title: string;
}

/**
 * Text for the LastUpdated cell. `now` is null until the component mounts. Before that the cell shows a fixed UTC
 * date so the server render and the first client render match. After mount it shows the relative time.
 */
export function displayLastUpdated(value: string | undefined, now: number | null): LastUpdatedDisplay {
  const then = value ? new Date(value) : null;
  if (!then || Number.isNaN(then.getTime())) return { text: MISSING, title: "" };
  if (now === null) {
    return { text: stableDateFormat.format(then), title: stableDateTimeFormat.format(then) };
  }
  return { text: formatRelativeTime(value, now), title: formatFullDateTime(value) };
}
