import { describe, expect, it } from "vitest";

import { SORT_OPTIONS, sortOptionFor } from "./resourceSort";

describe("sortOptionFor", () => {
  it("keeps the current direction for plain column options", () => {
    expect(sortOptionFor("kind", "desc").value).toBe("kind");
    expect(sortOptionFor("name", "asc").value).toBe("name");
  });

  it("maps last_updated to the option for its direction", () => {
    expect(sortOptionFor("last_updated", "desc").value).toBe("last_updated:desc");
    expect(sortOptionFor("last_updated", "asc").value).toBe("last_updated:asc");
  });

  it("falls back to Name for an unknown sort field", () => {
    expect(sortOptionFor("drop_table", "desc").value).toBe("name");
  });
});

describe("SORT_OPTIONS", () => {
  it("has a unique value for every option", () => {
    const values = SORT_OPTIONS.map((o) => o.value);
    expect(new Set(values).size).toBe(values.length);
  });

  it("points each last_updated option at its own direction", () => {
    const lastUpdated = SORT_OPTIONS.filter((o) => o.sortBy === "last_updated");
    expect(lastUpdated.map((o) => o.sortDir)).toEqual(["desc", "asc"]);
  });
});
