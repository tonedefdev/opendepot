"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import IconButton from "@mui/material/IconButton";
import AddIcon from "@mui/icons-material/Add";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";

export const NAME_COLUMN_WIDTH = 180;
export const TOGGLE_WIDTH = 28;

export const monoSx = { fontFamily: "monospace", fontSize: "0.8125rem" };
export const compactFieldSx = {
  "& .MuiFilledInput-root": { borderRadius: 1 },
  "& .MuiFilledInput-input": { ...monoSx, py: 0.75 },
};
export const nameColumnSx = { flex: { xs: "1 1 120px", sm: `0 0 ${NAME_COLUMN_WIDTH}px` }, minWidth: 0 };
export const rowLabelSx = {
  ...monoSx,
  ...nameColumnSx,
  px: 1.5,
  color: "text.secondary",
  overflow: "hidden",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
};
export const branchSx = {
  ml: `${TOGGLE_WIDTH / 2 - 1}px`,
  pl: 1.5,
  borderLeft: "1px solid",
  borderColor: "divider",
};
export const childBranchSx = { ...branchSx, ml: `${TOGGLE_WIDTH / 2 + 3}px` };
export const valueBranchSx = {
  ml: `${TOGGLE_WIDTH + 8}px`,
  pl: 0,
};
export const rowsSx = { display: "flex", flexDirection: "column", gap: 0.25 };
export const rowSx = {
  display: "flex",
  alignItems: "center",
  flexWrap: "wrap",
  gap: 1,
  px: 0.5,
  py: 0.25,
  borderRadius: 1,
  "&:hover": { bgcolor: "action.hover" },
  "& .row-action": { opacity: 0.4, transition: "opacity 120ms" },
  "&:hover .row-action, &:focus-within .row-action": { opacity: 1 },
  "@media (hover: none)": { "& .row-action": { opacity: 1 } },
};
export const rowActionsSx = { ml: "auto", display: "flex", alignItems: "center", gap: 0.5 };

export function RowSpacer() {
  return <Box sx={{ width: TOGGLE_WIDTH, flexShrink: 0 }} />;
}

export function RowToggle({ label, expanded, onToggle }: { label: string; expanded: boolean; onToggle: () => void }) {
  return (
    <IconButton
      size="small"
      aria-label={label}
      aria-expanded={expanded}
      onClick={onToggle}
      sx={{ width: TOGGLE_WIDTH, height: TOGGLE_WIDTH }}
    >
      <ChevronRightIcon
        fontSize="small"
        sx={{ transform: expanded ? "rotate(90deg)" : "none", transition: "transform 150ms" }}
      />
    </IconButton>
  );
}

export function RemoveButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <IconButton className="row-action" size="small" onClick={onClick} aria-label={label}>
      <DeleteOutlineIcon fontSize="small" />
    </IconButton>
  );
}

export function AddButton({ onClick, children }: { onClick: () => void; children: React.ReactNode }) {
  return (
    <Button size="small" startIcon={<AddIcon />} onClick={onClick} sx={{ alignSelf: "flex-start", ml: `${TOGGLE_WIDTH}px` }}>
      {children}
    </Button>
  );
}
