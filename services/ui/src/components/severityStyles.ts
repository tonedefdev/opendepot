import type { SxProps, Theme } from "@mui/material/styles";
import { metaChipSx, statusChipSx } from "./chipStyles";

// Severity chips keep the semantic hues used across the app. Dark mode draws them as tints. Light mode draws them as solid fills with white text.
const SEVERITY_TONES: Record<string, "error" | "warning" | "info" | "success" | "secondary"> = {
  critical: "error",
  high: "warning",
  medium: "info",
  low: "success",
  exempted: "secondary",
};

// Each light-mode fill clears 4.5:1 with white text. The bridge blue (secondary) only reaches 3.7:1, so exempted uses the brand blue instead.
const SOLID_LIGHT_FILL: Record<"error" | "warning" | "info" | "success" | "secondary", "error" | "warning" | "info" | "success" | "primary"> = {
  error: "error",
  warning: "warning",
  info: "info",
  success: "success",
  secondary: "primary",
};

const lightMode = '[data-mui-color-scheme="light"] &';

export const unknownSeverityChipSx: SxProps<Theme> = metaChipSx;

export function severityChipSx(severity: string): SxProps<Theme> {
  const tone = SEVERITY_TONES[severity.toLowerCase()];
  if (!tone) return unknownSeverityChipSx;
  const fill = SOLID_LIGHT_FILL[tone];
  return {
    ...(statusChipSx(tone) as object),
    [lightMode]: {
      bgcolor: `${fill}.main`,
      borderColor: `${fill}.main`,
      color: "common.white",
    },
  };
}
