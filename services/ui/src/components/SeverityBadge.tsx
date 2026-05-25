"use client";

import * as React from "react";
import Chip from "@mui/material/Chip";
import Tooltip from "@mui/material/Tooltip";
import type { BrowseScanCounts } from "@/lib/api";

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

export function SeverityChip({ severity, count }: Props) {
  if (count === 0) return null;
  return (
    <Tooltip title={`${count} ${severity.toUpperCase()}`}>
      <Chip
        size="small"
        color={colorMap[severity]}
        label={`${severity.charAt(0).toUpperCase()} ${count}`}
      />
    </Tooltip>
  );
}

export default function SeverityBadge({ counts }: { counts: BrowseScanCounts | null }) {
  if (!counts) return null;
  return (
    <React.Fragment>
      <SeverityChip severity="critical" count={counts.critical} />
      <SeverityChip severity="high" count={counts.high} />
      <SeverityChip severity="medium" count={counts.medium} />
      <SeverityChip severity="low" count={counts.low} />
      <SeverityChip severity="unknown" count={counts.unknown} />
    </React.Fragment>
  );
}
