import * as React from "react";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { riskLevelSeverity } from "@/lib/jevSeverity";

// Jev risk levels are ordinal, so the meter shows position on the scale as well as color. High and Critical
// share the error color and are told apart by how many bars are lit.
const LEVELS = ["Minimal", "Low", "Moderate", "High", "Critical"];

interface Props {
  level: string;
  showLabel?: boolean;
}

export default function RiskMeter({ level, showLabel = true }: Props) {
  const index = LEVELS.indexOf(level);
  const severity = riskLevelSeverity(level);
  const tone = severity ? `${severity}.main` : "text.secondary";
  // Low readings stay quiet so that the levels that need attention are the ones carrying color.
  const labelColor = severity && severity !== "success" ? tone : "text.secondary";

  return (
    <Box
      component="span"
      role="img"
      aria-label={`Jev risk: ${level}`}
      sx={{ display: "inline-flex", alignItems: "center", gap: 1, whiteSpace: "nowrap" }}
    >
      <Box component="span" sx={{ display: "inline-flex", alignItems: "flex-end", gap: "2px", height: 14 }}>
        {LEVELS.map((name, i) => (
          <Box
            component="span"
            key={name}
            sx={{
              width: 3,
              height: 4 + i * 2.5,
              borderRadius: "1px",
              bgcolor: i <= index ? tone : "action.disabled",
            }}
          />
        ))}
      </Box>
      {showLabel && (
        <Typography component="span" variant="body2" sx={{ color: labelColor, fontWeight: 500 }}>
          {level}
        </Typography>
      )}
    </Box>
  );
}
