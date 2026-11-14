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
