import { describe, expect, it } from "vitest";
import { pageFromSearch } from "./registryPaging";

describe("pageFromSearch", () => {
  it("reads the requested page from the navigation search string", () => {
    expect(pageFromSearch("q=ns&page=3&page_size=12", 1)).toBe(3);
  });

  it("treats a missing page as page 1", () => {
    expect(pageFromSearch("page_size=12", 4)).toBe(1);
  });

  it("falls back to the committed page for an invalid page", () => {
    expect(pageFromSearch("page=0", 4)).toBe(4);
    expect(pageFromSearch("page=abc", 4)).toBe(4);
    expect(pageFromSearch("page=-2", 4)).toBe(4);
  });
});
