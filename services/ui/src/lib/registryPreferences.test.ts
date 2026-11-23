import { afterEach, describe, expect, it, vi } from "vitest";

import {
  DEFAULT_REGISTRY_PAGE_SIZE,
  REGISTRY_PREFERENCES_KEY,
  loadRegistryPreferences,
  parseRegistryPreferences,
  saveRegistryPreferences,
} from "./registryPreferences";

function stubLocalStorage(initial: Record<string, string> = {}) {
  const store = new Map<string, string>(Object.entries(initial));
  const localStorage = {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => void store.set(key, value),
  };
  vi.stubGlobal("window", { localStorage });
  return store;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("parseRegistryPreferences", () => {
  it("returns an empty object for missing or invalid JSON", () => {
    expect(parseRegistryPreferences(null)).toEqual({});
    expect(parseRegistryPreferences("{not json")).toEqual({});
    expect(parseRegistryPreferences("null")).toEqual({});
    expect(parseRegistryPreferences("42")).toEqual({});
  });

  it("keeps valid values", () => {
    expect(
      parseRegistryPreferences(
        JSON.stringify({ pageSize: 48, sortBy: "risk", sortDir: "desc", view: "cards" }),
      ),
    ).toEqual({ pageSize: 48, sortBy: "risk", sortDir: "desc", view: "cards" });
  });

  it("drops values outside the supported options", () => {
    expect(
      parseRegistryPreferences(
        JSON.stringify({ pageSize: 13, sortBy: "drop_table", sortDir: "sideways", view: "grid" }),
      ),
    ).toEqual({});
    expect(parseRegistryPreferences(JSON.stringify({ pageSize: "24" }))).toEqual({});
  });

  it("keeps the last_updated sort field", () => {
    expect(parseRegistryPreferences(JSON.stringify({ sortBy: "last_updated", sortDir: "desc" }))).toEqual({
      sortBy: "last_updated",
      sortDir: "desc",
    });
  });
});

describe("saving and loading registry preferences", () => {
  it("merges each save into the stored preferences", () => {
    const store = stubLocalStorage();

    saveRegistryPreferences({ pageSize: 96 });
    saveRegistryPreferences({ view: "cards" });

    expect(JSON.parse(store.get(REGISTRY_PREFERENCES_KEY) ?? "{}")).toEqual({ pageSize: 96, view: "cards" });
    expect(loadRegistryPreferences()).toEqual({ pageSize: 96, view: "cards" });
  });

  it("returns no preferences on the server", () => {
    expect(loadRegistryPreferences()).toEqual({});
  });

  it("does not throw when storage is unavailable", () => {
    vi.stubGlobal("window", {
      localStorage: {
        getItem: () => {
          throw new Error("denied");
        },
        setItem: () => {
          throw new Error("quota");
        },
      },
    });

    expect(() => saveRegistryPreferences({ pageSize: 24 })).not.toThrow();
    expect(loadRegistryPreferences()).toEqual({});
  });
});

describe("defaults", () => {
  it("defaults the Registry Explorer to 12 per page", () => {
    expect(DEFAULT_REGISTRY_PAGE_SIZE).toBe(12);
  });
});
