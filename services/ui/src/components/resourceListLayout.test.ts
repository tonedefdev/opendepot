import { describe, expect, it } from "vitest";
import { resolveListView } from "./resourceListLayout";

describe("resolveListView", () => {
  it("shows the table only when rows is chosen on a wide viewport", () => {
    expect(resolveListView("rows", false)).toBe("rows");
  });

  it("shows cards when cards is chosen", () => {
    expect(resolveListView("cards", false)).toBe("cards");
  });

  it("shows cards below lg regardless of the saved view", () => {
    expect(resolveListView("rows", true)).toBe("cards");
    expect(resolveListView("cards", true)).toBe("cards");
  });
});
