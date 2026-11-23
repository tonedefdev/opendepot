export type ListView = "rows" | "cards";

// Below lg the table cannot fit its columns, so cards are the only layout there.
export function resolveListView(view: ListView, compact: boolean): ListView {
  return compact || view === "cards" ? "cards" : "rows";
}

// Fixed so the Name column takes the remaining width and the Risk column keeps its measured width.
export const LAST_UPDATED_COLUMN_PX = 132;
