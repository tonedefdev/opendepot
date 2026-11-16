import * as React from "react";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardContent from "@mui/material/CardContent";
import Typography from "@mui/material/Typography";

interface SectionCardProps {
  /** Anchor target, for links that jump to this section. */
  id?: string;
  icon: React.ReactNode;
  title: string;
  children: React.ReactNode;
}

export function SectionCard({ id, icon, title, children }: SectionCardProps) {
  return (
    <Card id={id} sx={{ mb: 2, scrollMarginTop: 16 }}>
      <CardContent>
        <Box display="flex" alignItems="center" gap={1} mb={2}>
          <Box sx={{ color: "primary.main", display: "flex" }}>{icon}</Box>
          <Typography variant="h6" sx={{ fontSize: "0.9375rem", fontWeight: 600 }}>
            {title}
          </Typography>
        </Box>
        {children}
      </CardContent>
    </Card>
  );
}
