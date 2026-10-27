import type { SxProps, Theme } from "@mui/material/styles";
import { kindColor } from "@/lib/kindColors";
import { riskLevelSeverity } from "@/lib/jevSeverity";

// Plain object styles (not functions) so server components can pass them to MUI.
const lightMode = '[data-mui-color-scheme="light"] &';

export const META_CHIP_HEIGHT = 22;

type StatusTone = "success" | "warning" | "error" | "info" | "secondary";

// Shared surface for neutral metadata chips. SourceRefChip is not an MUI Chip, so it reuses this directly.
export const metaChipSurfaceSx = {
  height: META_CHIP_HEIGHT,
  borderRadius: "6px",
  border: "1px solid",
  borderColor: "divider",
  bgcolor: "rgba(240,246,252,0.04)",
  color: "text.secondary",
  fontSize: "0.75rem",
  fontWeight: 600,
  [lightMode]: {
    bgcolor: "rgba(31,35,40,0.03)",
  },
};

const chipPartsSx = {
  "& .MuiChip-label": { px: 0.75 },
  "& .MuiChip-icon": { fontSize: 14, ml: 0.75, mr: -0.25, color: "text.secondary" },
};

export const metaChipSx: SxProps<Theme> = {
  ...metaChipSurfaceSx,
  ...chipPartsSx,
};

export const monoMetaChipSx: SxProps<Theme> = {
  ...metaChipSurfaceSx,
  ...chipPartsSx,
  fontFamily: "monospace",
  fontWeight: 500,
};

// Kind chips stay neutral. Only the icon carries the kind accent. Pass iconColor to use the Stats and Depots palette.
export function kindChipSx(kind: string, iconColor: string = kindColor(kind)): SxProps<Theme> {
  return {
    ...metaChipSurfaceSx,
    ...chipPartsSx,
    color: "text.primary",
    textTransform: "capitalize",
    "& .MuiChip-icon": { ...chipPartsSx["& .MuiChip-icon"], color: iconColor },
  };
}

const statusBaseSx = {
  ...chipPartsSx,
  height: META_CHIP_HEIGHT,
  borderRadius: "6px",
  border: "1px solid",
  fontSize: "0.75rem",
  fontWeight: 600,
};

function statusTintSx(tone: StatusTone) {
  return {
    borderColor: `rgba(var(--mui-palette-${tone}-mainChannel) / 0.35)`,
    bgcolor: `rgba(var(--mui-palette-${tone}-mainChannel) / 0.12)`,
    color: `${tone}.main`,
  };
}

// Status chips use a tint of the semantic palette color so status reads as status, not as a solid button.
export function statusChipSx(tone: StatusTone): SxProps<Theme> {
  return {
    ...statusBaseSx,
    ...statusTintSx(tone),
  };
}

// Risk chips take the severity tint and carry the Jev shield in the same color as the label.
export function riskChipSx(level: string): SxProps<Theme> {
  const severity = riskLevelSeverity(level);
  if (!severity) return metaChipSx;
  return {
    ...statusBaseSx,
    ...statusTintSx(severity),
    "& .MuiChip-icon": { ...chipPartsSx["& .MuiChip-icon"], color: "inherit" },
  };
}
