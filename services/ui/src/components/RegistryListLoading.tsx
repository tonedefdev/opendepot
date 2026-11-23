"use client";

import * as React from "react";
import Container from "@mui/material/Container";
import Skeleton from "@mui/material/Skeleton";
import Box from "@mui/material/Box";
import useMediaQuery from "@mui/material/useMediaQuery";
import { useTheme } from "@mui/material/styles";
import { loadRegistryPreferences } from "@/lib/registryPreferences";
import { ResourceListSkeleton } from "./ResourceListSkeleton";
import { type ListView, resolveListView } from "./resourceListLayout";

// Mirrors ResourceList's view choice, so the route loading state matches the layout that renders next.
export default function RegistryListLoading() {
  const theme = useTheme();
  const compact = useMediaQuery(theme.breakpoints.down("lg"));
  const [view, setView] = React.useState<ListView>("rows");
  // Read after mount so the server-rendered markup matches the first client render.
  React.useEffect(() => {
    const saved = loadRegistryPreferences().view;
    if (saved) setView(saved);
  }, []);

  return (
    <Container maxWidth="xl" sx={{ py: 4 }}>
      {/* Page title */}
      <Skeleton variant="text" width={280} height={48} sx={{ mb: 1 }} />
      <Skeleton variant="text" width={420} height={24} sx={{ mb: 4 }} />

      <ResourceListSkeleton view={resolveListView(view, compact)} />

      {/* Pagination placeholder */}
      <Box sx={{ display: "flex", justifyContent: "flex-end", mt: 3 }}>
        <Skeleton variant="rounded" width={320} height={36} />
      </Box>
    </Container>
  );
}
