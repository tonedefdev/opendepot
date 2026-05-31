import * as React from "react";
import Container from "@mui/material/Container";
import Grid from "@mui/material/Grid";
import Skeleton from "@mui/material/Skeleton";
import Box from "@mui/material/Box";

export default function Loading() {
  return (
    <Container maxWidth="xl" sx={{ py: 4 }}>
      {/* Page title */}
      <Skeleton variant="text" width={280} height={48} sx={{ mb: 1 }} />
      <Skeleton variant="text" width={420} height={24} sx={{ mb: 4 }} />

      {/* Resource cards */}
      <Grid container spacing={2}>
        {Array.from({ length: 12 }).map((_, i) => (
          <Grid key={i} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
            <Skeleton variant="rounded" height={180} />
          </Grid>
        ))}
      </Grid>

      {/* Pagination placeholder */}
      <Box sx={{ display: "flex", justifyContent: "flex-end", mt: 3 }}>
        <Skeleton variant="rounded" width={320} height={36} />
      </Box>
    </Container>
  );
}
