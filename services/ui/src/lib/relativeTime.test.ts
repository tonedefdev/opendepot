import { describe, expect, it } from "vitest";
import { formatFullDateTime, formatRelativeTime } from "./relativeTime";

const NOW = Date.parse("2026-05-20T12:00:00Z");

describe("formatRelativeTime", () => {
  it("returns an em dash when the value is missing or invalid", () => {
    expect(formatRelativeTime(undefined, NOW)).toBe("\u2014");
    expect(formatRelativeTime("", NOW)).toBe("\u2014");
    expect(formatRelativeTime("not a date", NOW)).toBe("\u2014");
  });

  it("reads anything under a minute, including clock skew into the future, as just now", () => {
    expect(formatRelativeTime("2026-05-20T11:59:30Z", NOW)).toBe("just now");
    expect(formatRelativeTime("2026-05-20T12:00:30Z", NOW)).toBe("just now");
  });

  it("uses the largest unit that fits", () => {
    expect(formatRelativeTime("2026-05-20T11:55:00Z", NOW)).toBe("5 minutes ago");
    expect(formatRelativeTime("2026-05-20T09:00:00Z", NOW)).toBe("3 hours ago");
    expect(formatRelativeTime("2026-05-17T12:00:00Z", NOW)).toBe("3 days ago");
    expect(formatRelativeTime("2026-03-21T12:00:00Z", NOW)).toBe("2 months ago");
    expect(formatRelativeTime("2024-05-20T12:00:00Z", NOW)).toBe("2 years ago");
  });

  it("uses singular units for one", () => {
    expect(formatRelativeTime("2026-05-19T12:00:00Z", NOW)).toBe("1 day ago");
  });
});

describe("formatFullDateTime", () => {
  it("returns an empty string when the value is missing or invalid", () => {
    expect(formatFullDateTime(undefined)).toBe("");
    expect(formatFullDateTime("not a date")).toBe("");
  });

  it("returns the locale date-time for a valid RFC 3339 value", () => {
    expect(formatFullDateTime("2026-05-20T12:00:00Z")).toBe(new Date("2026-05-20T12:00:00Z").toLocaleString());
  });
});
