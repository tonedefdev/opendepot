import type { SxProps, Theme } from "@mui/material/styles";
import { metaChipSx, statusChipSx } from "./chipStyles";

// Severity chips keep the semantic hues used across the app, drawn as tints so they match the other status chips.
const SEVERITY_TONES: Record<string, "error" | "warning" | "info" | "success" | "secondary"> = {
  critical: "error",
  high: "warning",
  medium: "info",
  low: "success",
  exempted: "secondary",
};

export const unknownSeverityChipSx: SxProps<Theme> = metaChipSx;

export function severityChipSx(severity: string): SxProps<Theme> {
  const tone = SEVERITY_TONES[severity.toLowerCase()];
  return tone ? statusChipSx(tone) : unknownSeverityChipSx;
}
