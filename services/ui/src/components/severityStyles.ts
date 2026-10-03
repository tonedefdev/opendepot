import type { SxProps, Theme } from "@mui/material/styles";

export const unknownSeverityChipSx: SxProps<Theme> = (theme) => ({
  color: "#fff",
  bgcolor: "#6e7681",
  ...theme.applyStyles("light", {
    bgcolor: "#6e7781",
  }),
});