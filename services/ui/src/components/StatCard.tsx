import * as React from "react";
import Box from "@mui/material/Box";
import Paper from "@mui/material/Paper";
import Skeleton from "@mui/material/Skeleton";
import Typography from "@mui/material/Typography";

interface StatCardProps {
  label: string;
  value: string | number;
  sub?: string;
  icon: React.ReactNode;
  accentColor: string;
  loading?: boolean;
}

export default function StatCard({ label, value, sub, icon, accentColor, loading = false }: StatCardProps) {
  return (
    <Paper
      elevation={3}
      sx={{
        height: "100%",
        overflow: "hidden",
        borderTop: `4px solid ${accentColor}`,
      }}
    >
      <Box sx={{ p: { xs: 1.5, sm: 2, lg: 2.5 } }}>
        <Box sx={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", mb: 1 }}>
          <Typography
            variant="body2"
            color="text.secondary"
            fontWeight={500}
            sx={{ fontSize: { xs: "0.75rem", lg: "0.875rem" } }}
          >
            {label}
          </Typography>
          <Box
            sx={{
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              width: { xs: 32, lg: 36 },
              height: { xs: 32, lg: 36 },
              borderRadius: 1.5,
              bgcolor: `${accentColor}18`,
              color: accentColor,
              flexShrink: 0,
              "& svg": { fontSize: { xs: "1.1rem", md: "0.95rem", lg: "1.25rem" } },
            }}
          >
            {icon}
          </Box>
        </Box>
        <Typography
          fontWeight={700}
          sx={{
            lineHeight: 1.1,
            fontSize: { xs: "1.5rem", sm: "1.75rem", lg: "2rem" },
          }}
        >
          {loading ? <Skeleton width="40%" /> : value}
        </Typography>
        {sub && (
          <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: "block" }}>
            {loading ? <Skeleton width="60%" /> : sub}
          </Typography>
        )}
      </Box>
    </Paper>
  );
}
