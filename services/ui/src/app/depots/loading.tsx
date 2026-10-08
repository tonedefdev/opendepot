import * as React from "react";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Skeleton from "@mui/material/Skeleton";
import Typography from "@mui/material/Typography";
import WarehouseIcon from "@mui/icons-material/Warehouse";
import PageHeader from "@/components/PageHeader";

export default function DepotsLoading() {
  return (
    <main>
      <PageHeader
        icon={<WarehouseIcon color="primary" fontSize="small" />}
        title="Depots"
        description="Visualise the relationships between Depots and their managed Modules and Providers."
        mobileOnly
      />
      <Container maxWidth="xl" sx={{ py: 4 }} role="status" aria-label="Loading depots">
        <Box mb={3} sx={{ display: { xs: "none", sm: "block" } }}>
          <Box display="flex" alignItems="center" minHeight={40}>
            <Typography variant="h4" component="h1">
              Depots
            </Typography>
          </Box>
          <Typography variant="body1" color="text.secondary" mt={1} mb={2}>
            Visualise the relationships between Depots and their managed Modules and Providers.
          </Typography>
        </Box>

        <Box sx={{ width: { xs: "100%", sm: 420 }, mb: 3 }}>
          <Skeleton variant="rounded" height={40} sx={{ mb: 2 }} />
          <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
            {[72, 84, 88, 80].map((width, index) => (
              <Skeleton key={index} variant="rounded" width={width} height={24} sx={{ borderRadius: 4 }} />
            ))}
          </Box>
        </Box>

        <Box
          sx={{
            position: "relative",
            height: "calc(100vh - 290px)",
            minHeight: 420,
            backgroundImage: "radial-gradient(var(--mui-palette-divider) 1px, transparent 1px)",
            backgroundSize: "20px 20px",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            gap: { xs: 3, sm: 5 },
          }}
        >
          {[1, 5, 8].map((count, column) => (
            <Box key={column} sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
              {Array.from({ length: count }, (_, row) => (
                <Skeleton key={row} variant="rounded" width={96} height={28} sx={{ borderRadius: 1 }} />
              ))}
            </Box>
          ))}
          <Skeleton variant="rounded" width={32} height={120} sx={{ position: "absolute", left: 16, bottom: 16 }} />
          <Skeleton variant="rounded" width={200} height={150} sx={{ position: "absolute", right: 16, bottom: 16, display: { xs: "none", sm: "block" } }} />
        </Box>
      </Container>
    </main>
  );
}
