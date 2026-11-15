"use client";

import * as React from "react";
import type { Components } from "react-markdown";

function textOf(node: React.ReactNode): string {
  if (node == null || typeof node === "boolean") return "";
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (React.isValidElement<{ children?: React.ReactNode }>(node)) return textOf(node.props.children);
  return "";
}

// Copies each header's text onto its body cells as data-label so the mobile
// layout can show a label above every value once rows stack into cards.
function labelBodyCells(children: React.ReactNode): React.ReactNode {
  let labels: string[] = [];
  React.Children.forEach(children, (child) => {
    if (!React.isValidElement<{ children?: React.ReactNode }>(child) || child.type !== "thead") return;
    const headRow = React.Children.toArray(child.props.children).find(React.isValidElement) as
      | React.ReactElement<{ children?: React.ReactNode }>
      | undefined;
    if (!headRow) return;
    labels = React.Children.toArray(headRow.props.children).map((th) =>
      React.isValidElement<{ children?: React.ReactNode }>(th) ? textOf(th.props.children).trim() : "",
    );
  });

  return React.Children.map(children, (child) => {
    if (!React.isValidElement<{ children?: React.ReactNode }>(child) || child.type !== "tbody") return child;
    return React.cloneElement(
      child,
      undefined,
      React.Children.map(child.props.children, (row) => {
        if (!React.isValidElement<{ children?: React.ReactNode }>(row)) return row;
        return React.cloneElement(
          row,
          undefined,
          React.Children.map(row.props.children, (cell, index) =>
            React.isValidElement(cell)
              ? React.cloneElement(cell as React.ReactElement<Record<string, unknown>>, {
                  "data-label": labels[index] ?? "",
                })
              : cell,
          ),
        );
      }),
    );
  });
}

export const markdownTableComponents: Components = {
  table({ children }) {
    return <table>{labelBodyCells(children)}</table>;
  },
};

// Tables are full-width cards below lg (stacked rows, label above each value)
// and standard bordered tables at lg and up.
export const markdownTableSx = {
  "& table": {
    display: { xs: "block", lg: "table" },
    width: { xs: "100%", lg: "auto" },
    borderCollapse: "collapse",
    mb: 1.5,
  },
  "& thead": { display: { xs: "none", lg: "table-header-group" } },
  "& tbody": { display: { xs: "block", lg: "table-row-group" } },
  "& tr": {
    display: { xs: "block", lg: "table-row" },
    mb: { xs: 1.5, lg: 0 },
    p: { xs: 1.5, lg: 0 },
    border: { xs: "1px solid", lg: "none" },
    borderColor: "divider",
    borderRadius: 1.5,
  },
  "& th, & td": {
    display: { xs: "block", lg: "table-cell" },
    border: { xs: "none", lg: "1px solid" },
    borderColor: "divider",
    px: { xs: 0, lg: 1.5 },
    py: { xs: 0.5, lg: 0.75 },
    wordBreak: { xs: "break-word", lg: "normal" },
  },
  "& td[data-label]::before": {
    display: { xs: "block", lg: "none" },
    content: "attr(data-label)",
    fontSize: "0.6875rem",
    fontWeight: 600,
    textTransform: "uppercase",
    letterSpacing: "0.06em",
    color: "text.secondary",
    mb: 0.25,
  },
  "& td:first-of-type": { fontWeight: { xs: 600, lg: "inherit" } },
};
