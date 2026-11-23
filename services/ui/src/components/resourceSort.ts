export type SortDir = "asc" | "desc";

export interface SortOption {
  /** Unique key for the dropdown item. */
  value: string;
  label: string;
  sortBy: string;
  /** Set when the option fixes the direction. Otherwise the current direction is kept. */
  sortDir?: SortDir;
}

export const SORT_OPTIONS: SortOption[] = [
  { value: "name", label: "Name", sortBy: "name" },
  { value: "namespace", label: "Namespace", sortBy: "namespace" },
  { value: "latest_version", label: "Latest version", sortBy: "latest_version" },
  { value: "kind", label: "Kind", sortBy: "kind" },
  { value: "status", label: "Status", sortBy: "status" },
  { value: "source", label: "Source", sortBy: "source" },
  { value: "risk", label: "Risk", sortBy: "risk" },
  { value: "last_updated:desc", label: "Last updated (newest first)", sortBy: "last_updated", sortDir: "desc" },
  { value: "last_updated:asc", label: "Last updated (oldest first)", sortBy: "last_updated", sortDir: "asc" },
];

// Last updated has two dropdown items, one per direction, so the dropdown always reflects the URL's sort_dir.
// An unknown sort_by falls back to Name, matching the server default.
export function sortOptionFor(sortBy: string, sortDir: SortDir): SortOption {
  return (
    SORT_OPTIONS.find((o) => o.sortBy === sortBy && (o.sortDir ?? sortDir) === sortDir) ?? SORT_OPTIONS[0]
  );
}
