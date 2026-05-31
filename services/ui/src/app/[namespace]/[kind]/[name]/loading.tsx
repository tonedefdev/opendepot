import * as React from "react";
import Container from "@mui/material/Container";
import Box from "@mui/material/Box";
import Skeleton from "@mui/material/Skeleton";

export default function Loading() {
  return (
    <Container maxWidth="xl" sx={{ py: 4 }}>
      {/* Breadcrumbs */}
      <Skeleton variant="text" width={200} height={20} sx={{ mb: 2 }} />

      {/* Hero */}
      <Box sx={{ display: "flex", alignItems: "center", gap: 2, mb: 3 }}>
        <Skeleton variant="circular" width={48} height={48} />
        <Box sx={{ flex: 1 }}>
          <Skeleton variant="text" width={240} height={36} />
          <Skeleton variant="text" width={160} height={22} />
        </Box>
      </Box>

      {/* Section cards */}
      {[180, 220, 300].map((h, i) => (
        <Skeleton key={i} variant="rounded" height={h} sx={{ mb: 2 }} />
      ))}
    </Container>
  );
}
