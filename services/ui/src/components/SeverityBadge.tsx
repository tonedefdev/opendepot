"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Tooltip from "@mui/material/Tooltip";
import type { BrowseScanCounts } from "@/lib/api";
import { severityChipSx } from "./severityStyles";

interface Props {
  severity: "critical" | "high" | "medium" | "low" | "unknown";
  count: number;
}

const colorMap: Record<Props["severity"], "error" | "warning" | "info" | "default" | "success"> = {
  critical: "error",
  high: "warning",
  medium: "info",
  low: "success",
  unknown: "default",
};

export function SeverityChip({ severity, count, dense = false }: Props & { dense?: boolean }) {
  if (count === 0) return null;
  return (
    <Tooltip title={`${count} ${severity.toUpperCase()}`}>
      <Chip
        size="small"
        color={colorMap[severity]}
        label={`${severity.charAt(0).toUpperCase()} ${count}`}
        sx={severityChipSx(severity, dense)}
      />
    </Tooltip>
  );
}

// A list row stretches its name link over the whole row, so the chips lift above that layer to receive hover.
export default function SeverityBadge({ counts, dense = false }: { counts: BrowseScanCounts | null; dense?: boolean }) {
  if (!counts) return null;
  return (
    <Box sx={{ display: "inline-flex", alignItems: "center", gap: 0.5, position: "relative", zIndex: 1 }}>
      <SeverityChip severity="critical" count={counts.critical} dense={dense} />
      <SeverityChip severity="high" count={counts.high} dense={dense} />
      <SeverityChip severity="medium" count={counts.medium} dense={dense} />
      <SeverityChip severity="low" count={counts.low} dense={dense} />
      <SeverityChip severity="unknown" count={counts.unknown} dense={dense} />
      {counts.exempted > 0 && (
        <Tooltip title={`${counts.exempted} exempted findings`}>
          <Chip size="small" color="secondary" label={`E ${counts.exempted}`} sx={severityChipSx("exempted", dense)} />
        </Tooltip>
      )}
    </Box>
  );
}
