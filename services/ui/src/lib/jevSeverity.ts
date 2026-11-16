export type JevSeverity = "success" | "warning" | "error";

// RISK_COLORS maps a Jev risk level to an MUI Chip color.
export const RISK_COLORS: Record<string, "success" | "info" | "warning" | "error"> = {
  Minimal: "success",
  Low: "success",
  Moderate: "warning",
  High: "error",
  Critical: "error",
};

// probabilitySeverity mirrors cli/output.go probabilityColor. Rows where higher is better (Safe) are
// inverted so that a high safe probability reads as green.
export function probabilitySeverity(v: number, higherIsBetter: boolean): JevSeverity {
  const risk = higherIsBetter ? 1 - v : v;

  if (risk >= 0.5) {
    return "error";
  }

  if (risk >= 0.25) {
    return "warning";
  }

  return "success";
}

// riskLevelSeverity mirrors cli/output.go riskLevelColor. Unrecognized levels return undefined so the
// row stays neutral, as it does in the CLI.
export function riskLevelSeverity(level: string): JevSeverity | undefined {
  switch (level) {
    case "Minimal":
    case "Low":
      return "success";
    case "Moderate":
      return "warning";
    case "High":
    case "Critical":
      return "error";
    default:
      return undefined;
  }
}
