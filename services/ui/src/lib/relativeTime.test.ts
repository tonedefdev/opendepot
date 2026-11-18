import { describe, expect, it } from "vitest";
import { displayLastUpdated, formatFullDateTime, formatRelativeTime } from "./relativeTime";

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

describe("displayLastUpdated", () => {
  it("shows a fixed UTC date and never a relative phrase before mount", () => {
    const display = displayLastUpdated("2026-05-19T12:00:00Z", null);

    expect(display.text).toBe("May 19, 2026");
    expect(display.title).toContain("May 19, 2026");
    expect(display.text).not.toContain("ago");
  });

  it("reads the date in UTC so every time zone renders the same text", () => {
    // 23:30 UTC is still May 20 in UTC but already May 21 in UTC+1 and later zones.
    expect(displayLastUpdated("2026-05-20T23:30:00Z", null).text).toBe("May 20, 2026");
  });

  it("shows the relative time and the locale date-time after mount", () => {
    const display = displayLastUpdated("2026-05-19T12:00:00Z", NOW);

    expect(display.text).toBe("1 day ago");
    expect(display.title).toBe(new Date("2026-05-19T12:00:00Z").toLocaleString());
  });

  it("shows an em dash and no tooltip when the value is missing or invalid, before and after mount", () => {
    for (const now of [null, NOW]) {
      expect(displayLastUpdated(undefined, now)).toEqual({ text: "\u2014", title: "" });
      expect(displayLastUpdated("not a date", now)).toEqual({ text: "\u2014", title: "" });
    }
  });
});
