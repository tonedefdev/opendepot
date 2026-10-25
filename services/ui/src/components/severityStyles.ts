import type { SxProps, Theme } from "@mui/material/styles";

// Plain object styles (not functions) so server components can pass them to MUI.
const modeSelector = (mode: "dark" | "light") => `[data-mui-color-scheme="${mode}"] &`;

export const unknownSeverityChipSx: SxProps<Theme> = {
  color: "#fff",
  bgcolor: "#6e7681",
  [modeSelector("light")]: {
    bgcolor: "#6e7781",
  },
};

// Darker fills keep white text at or above 4.5:1. Light-mode palette fills already pass except exempted.
const darkSeverityFills: Record<string, string> = {
  critical: "#e91309",
  high: "#976e18",
  medium: "#02856e",
  low: "#2e863a",
  exempted: "#0473de",
};

const lightSeverityFills: Record<string, string> = {
  exempted: "#038282",
};

export function severityChipSx(severity: string): SxProps<Theme> {
  const key = severity.toLowerCase();
  if (key === "unknown") return unknownSeverityChipSx;
  const sx: Record<string, unknown> = { color: "#fff" };
  if (darkSeverityFills[key]) sx[modeSelector("dark")] = { bgcolor: darkSeverityFills[key] };
  if (lightSeverityFills[key]) sx[modeSelector("light")] = { bgcolor: lightSeverityFills[key] };
  return sx as SxProps<Theme>;
}
