import * as React from "react";
import Box from "@mui/material/Box";
import Divider from "@mui/material/Divider";
import Skeleton from "@mui/material/Skeleton";
import Typography from "@mui/material/Typography";
import PrecisionManufacturingIcon from "@mui/icons-material/PrecisionManufacturing";
import PageHeader from "@/components/PageHeader";

export default function AssemblyLoading() {
  return (
    <Box sx={{ display: "flex", flexDirection: "column", height: "100vh" }} role="status" aria-label="Loading assembly line">
      <PageHeader
        icon={<PrecisionManufacturingIcon color="primary" fontSize="small" />}
        title="Assembly Line"
        description="Compose modules visually by wiring outputs to inputs."
      />
      <Box sx={{ flex: 1, minHeight: 0, display: "flex" }}>
        <Box
          sx={{
            width: 280,
            flexShrink: 0,
            borderRight: "1px solid",
            borderColor: "divider",
            bgcolor: "background.paper",
            display: { xs: "none", sm: "flex" },
            flexDirection: "column",
          }}
        >
          <Box sx={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column", p: 1.5, gap: 1 }}>
            <Skeleton variant="rounded" height={40} sx={{ flexShrink: 0 }} />
            <Box sx={{ flex: 1, minHeight: 0, overflow: "hidden" }}>
              <Typography variant="caption" fontWeight={600} color="text.secondary" sx={{ display: "block", mb: 0.75 }}>
                Modules
              </Typography>
              {Array.from({ length: 10 }, (_, index) => (
                <Box key={index} sx={{ p: 1, mb: 0.75, borderRadius: 1, border: "1px solid", borderColor: "divider" }}>
                  <Skeleton variant="text" width={`${55 + ((index * 17) % 35)}%`} />
                  <Skeleton variant="text" width="70%" height={16} />
                </Box>
              ))}
            </Box>
          </Box>
          <Divider />
          <Box sx={{ p: 1.5, display: "flex", flexDirection: "column", gap: 1 }}>
            <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
              <Typography variant="caption" fontWeight={600} color="text.secondary" sx={{ textTransform: "uppercase", letterSpacing: "0.05em" }}>
                Variables
              </Typography>
              <Skeleton variant="rounded" width={56} height={28} />
            </Box>
            <Skeleton variant="text" width="45%" height={16} />
          </Box>
          <Divider />
          <Box sx={{ p: 1.5, display: "grid", gridTemplateColumns: "1fr 1fr", gap: 1 }}>
            <Skeleton variant="rounded" height={30} />
            <Skeleton variant="rounded" height={30} />
          </Box>
        </Box>
        <Box
          sx={{
            flex: 1,
            minWidth: 0,
            position: "relative",
            bgcolor: "background.default",
            backgroundImage: "radial-gradient(var(--mui-palette-divider) 1px, transparent 1px)",
            backgroundSize: "20px 20px",
          }}
        >
          <Skeleton variant="rounded" width={32} height={120} sx={{ position: "absolute", left: 16, bottom: 16 }} />
          <Skeleton variant="rounded" width={200} height={150} sx={{ position: "absolute", right: 16, bottom: 16, display: { xs: "none", sm: "block" } }} />
        </Box>
      </Box>
    </Box>
  );
}
