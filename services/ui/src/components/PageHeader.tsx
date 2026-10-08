import * as React from "react";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import MobileSidebarButton from "@/components/MobileSidebarButton";

interface PageHeaderProps {
  icon: React.ReactNode;
  title: string;
  description: string;
  actions?: React.ReactNode;
  mobileOnly?: boolean;
}

export default function PageHeader({ icon, title, description, actions, mobileOnly = false }: PageHeaderProps) {
  return (
    <Box
      sx={{
        px: { xs: 2, sm: 3 },
        py: { xs: 1, sm: 1.5 },
        borderBottom: "1px solid",
        borderColor: "divider",
        display: mobileOnly ? { xs: "flex", sm: "none" } : "flex",
        alignItems: "center",
        gap: 1,
        minWidth: 0,
        position: "sticky",
        top: 0,
        zIndex: 1100,
        bgcolor: "background.default",
      }}
    >
      {icon}
      <Typography variant="h6" fontWeight={600} noWrap sx={{ fontSize: { xs: "1rem", sm: "1.25rem" } }}>
        {title}
      </Typography>
      <Typography variant="body2" color="text.secondary" noWrap sx={{ ml: 1, display: { xs: "none", sm: "block" } }}>
        {description}
      </Typography>
      <Box sx={{ display: "flex", alignItems: "center", gap: 0.5, ml: "auto" }}>
        {actions}
        <MobileSidebarButton />
      </Box>
    </Box>
  );
}
