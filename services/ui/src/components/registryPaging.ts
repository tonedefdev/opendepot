/** The page a registry search string points at. A missing or invalid page falls back to the given page. */
export function pageFromSearch(search: string, fallback: number): number {
  const page = Number.parseInt(new URLSearchParams(search).get("page") ?? "1", 10);
  return Number.isInteger(page) && page >= 1 ? page : fallback;
}
