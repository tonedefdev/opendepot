import * as React from "react";
import Box from "@mui/material/Box";
import Paper from "@mui/material/Paper";
import Skeleton from "@mui/material/Skeleton";
import Typography from "@mui/material/Typography";
import { ACCENT_COLOR } from "@/lib/kindColors";

interface StatCardProps {
  label: string;
  value: string | number;
  sub?: string;
  icon: React.ReactNode;
  // Tints the small inline icon only.
  accentColor?: string;
  loading?: boolean;
}

export default function StatCard({
  label,
  value,
  sub,
  icon,
  accentColor = ACCENT_COLOR,
  loading = false,
}: StatCardProps) {
  return (
    <Paper variant="outlined" sx={{ height: "100%", p: { xs: 1.5, lg: 2 } }}>
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1,
          mb: 1.5,
          minWidth: 0,
          color: "text.secondary",
        }}
      >
        <Box
          sx={{
            display: "flex",
            color: accentColor,
            "& svg": { fontSize: "1rem" },
          }}
        >
          {icon}
        </Box>
        <Typography
          variant="body2"
          color="text.secondary"
          noWrap
          sx={{ fontSize: { xs: "0.75rem", lg: "0.8125rem" } }}
        >
          {label}
        </Typography>
      </Box>
      <Typography
        fontWeight={600}
        sx={{
          lineHeight: 1.1,
          fontSize: { xs: "1.375rem", sm: "1.5rem", lg: "1.75rem" },
          fontVariantNumeric: "tabular-nums",
          whiteSpace: "nowrap",
        }}
      >
        {loading ? <Skeleton width="40%" /> : value}
      </Typography>
      {sub && (
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ mt: 0.5, display: "block" }}
        >
          {loading ? <Skeleton width="60%" /> : sub}
        </Typography>
      )}
    </Paper>
  );
}

// StatStrip sets StatCards side by side as cells of one outlined band, divided by hairlines. The 1px grid
// gap over a divider-colored backing draws the rules, so they stay correct however the cells wrap.
export function StatStrip({ children, columns }: { children: React.ReactNode; columns: number }) {
  return (
    <Paper
      variant="outlined"
      sx={{
        display: "grid",
        gridTemplateColumns: { xs: "repeat(2, 1fr)", sm: `repeat(${Math.min(columns, 4)}, 1fr)`, xl: `repeat(${columns}, 1fr)` },
        gap: "1px",
        overflow: "hidden",
        bgcolor: "divider",
        "& > .MuiPaper-root": { border: 0, borderRadius: 0 },
      }}
    >
      {children}
    </Paper>
  );
}
