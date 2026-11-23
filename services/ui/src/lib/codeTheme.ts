import type { PrismTheme } from "prism-react-renderer";

// prism-react-renderer's stock github theme uses token colors that fail WCAG 4.5:1 on light surfaces.
// This keeps its token mapping and swaps in darker light-mode values.
export const lightCodeTheme: PrismTheme = {
  plain: { color: "#24292f", backgroundColor: "#f6f8fa" },
  styles: [
    { types: ["comment", "prolog", "doctype", "cdata"], style: { color: "#57606a", fontStyle: "italic" } },
    { types: ["namespace"], style: { opacity: 0.7 } },
    { types: ["string", "attr-value"], style: { color: "#116329" } },
    { types: ["punctuation", "operator"], style: { color: "#24292f" } },
    {
      types: ["entity", "url", "symbol", "number", "boolean", "variable", "constant", "property", "regex", "inserted"],
      style: { color: "#0550ae" },
    },
    { types: ["atrule", "keyword", "attr-name", "selector"], style: { color: "#cf222e" } },
    { types: ["function", "tag"], style: { color: "#116329" } },
    { types: ["deleted"], style: { color: "#cf222e" } },
    { types: ["function-variable"], style: { color: "#6f42c1" } },
  ],
};
