// Kept free of "use client" so the server-rendered Registry Explorer page can read the default page size.
export const REGISTRY_PAGE_SIZES = [12, 24, 48, 96];
export const DEFAULT_REGISTRY_PAGE_SIZE = 12;
export const REGISTRY_SORT_FIELDS = ["name", "namespace", "latest_version", "kind", "status", "source", "risk", "last_updated"];
export const REGISTRY_PREFERENCES_KEY = "opendepot.registry.preferences";

export type RegistryView = "rows" | "cards";
export type RegistrySortDir = "asc" | "desc";

export interface RegistryPreferences {
  pageSize?: number;
  sortBy?: string;
  sortDir?: RegistrySortDir;
  view?: RegistryView;
}

// Unknown or malformed values are dropped so a corrupted entry falls back to the defaults.
export function parseRegistryPreferences(raw: string | null): RegistryPreferences {
  if (!raw) return {};
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return {};
  }
  if (typeof value !== "object" || value === null) return {};

  const stored = value as Record<string, unknown>;
  const prefs: RegistryPreferences = {};
  if (typeof stored.pageSize === "number" && REGISTRY_PAGE_SIZES.includes(stored.pageSize)) {
    prefs.pageSize = stored.pageSize;
  }
  if (typeof stored.sortBy === "string" && REGISTRY_SORT_FIELDS.includes(stored.sortBy)) {
    prefs.sortBy = stored.sortBy;
  }
  if (stored.sortDir === "asc" || stored.sortDir === "desc") prefs.sortDir = stored.sortDir;
  if (stored.view === "rows" || stored.view === "cards") prefs.view = stored.view;
  return prefs;
}

export function loadRegistryPreferences(): RegistryPreferences {
  if (typeof window === "undefined") return {};
  try {
    return parseRegistryPreferences(window.localStorage.getItem(REGISTRY_PREFERENCES_KEY));
  } catch {
    return {};
  }
}

export function saveRegistryPreferences(patch: RegistryPreferences): void {
  if (typeof window === "undefined") return;
  try {
    const next = { ...loadRegistryPreferences(), ...patch };
    window.localStorage.setItem(REGISTRY_PREFERENCES_KEY, JSON.stringify(next));
  } catch {
    // Storage can be unavailable (private mode, quota). The list still works, it just won't persist.
  }
}
