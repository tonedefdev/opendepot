import * as React from "react";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Grid from "@mui/material/Grid";
import Paper from "@mui/material/Paper";
import Skeleton from "@mui/material/Skeleton";
import Typography from "@mui/material/Typography";
import AccountTreeIcon from "@mui/icons-material/AccountTree";
import BarChartIcon from "@mui/icons-material/BarChart";
import CloudQueueIcon from "@mui/icons-material/CloudQueue";
import DownloadIcon from "@mui/icons-material/Download";
import ExtensionIcon from "@mui/icons-material/Extension";
import PieChartIcon from "@mui/icons-material/PieChart";
import SecurityIcon from "@mui/icons-material/Security";
import StorageIcon from "@mui/icons-material/Storage";
import SyncIcon from "@mui/icons-material/Sync";
import TrendingUpIcon from "@mui/icons-material/TrendingUp";
import WarehouseIcon from "@mui/icons-material/Warehouse";
import PageHeader from "@/components/PageHeader";
import StatCard from "@/components/StatCard";

const statCards = [
  { label: "Modules", icon: <ExtensionIcon fontSize="small" />, accentColor: "#6366f1" },
  { label: "Providers", icon: <CloudQueueIcon fontSize="small" />, accentColor: "#0ea5e9" },
  { label: "Versions", icon: <AccountTreeIcon fontSize="small" />, accentColor: "#8b5cf6" },
  { label: "Depots", icon: <WarehouseIcon fontSize="small" />, accentColor: "#f97316" },
  { label: "Storage Used", icon: <StorageIcon fontSize="small" />, accentColor: "#f59e0b" },
  { label: "Downloads", icon: <DownloadIcon fontSize="small" />, accentColor: "#10b981" },
];

function SectionHeader({ icon, title }: { icon: React.ReactNode; title: string }) {
  return (
    <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 1, "& svg": { fontSize: 18, color: "text.secondary" } }}>
      {icon}
      <Typography variant="subtitle1" fontWeight={600}>
        {title}
      </Typography>
    </Box>
  );
}

export default function StatsLoading() {
  return (
    <>
      <PageHeader
        icon={<BarChartIcon color="primary" fontSize="small" />}
        title="Registry Statistics"
        description="Live metrics across all visible modules, providers, and versions."
        mobileOnly
      />
      <Container maxWidth="xl" sx={{ py: 3 }} role="status" aria-label="Loading statistics">
        <Box sx={{ mb: 0.5, display: { xs: "none", sm: "flex" }, alignItems: "center", minHeight: 40 }}>
          <Typography variant="h5" fontWeight={600}>
            Registry Statistics
          </Typography>
        </Box>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 3, display: { xs: "none", sm: "block" } }}>
          Live metrics across all visible modules, providers, and versions.
        </Typography>

        <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
          <Grid container spacing={2}>
            {statCards.map((card) => (
              <Grid key={card.label} size={{ xs: 6, sm: 4, md: 4, lg: 2 }}>
                <StatCard loading label={card.label} value={0} icon={card.icon} accentColor={card.accentColor} />
              </Grid>
            ))}
          </Grid>

          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <Paper elevation={2} sx={{ p: 2.5, height: "100%" }}>
                <SectionHeader icon={<SyncIcon />} title="Sync Health" />
                <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
                  {["Synced", "Unsynced", "Failed"].map((label) => (
                    <Box key={label}>
                      <Box sx={{ display: "flex", justifyContent: "space-between", mb: 0.5 }}>
                        <Typography variant="body2">{label}</Typography>
                        <Skeleton variant="text" width={40} />
                      </Box>
                      <Skeleton variant="rounded" height={8} sx={{ borderRadius: 4 }} />
                    </Box>
                  ))}
                </Box>
              </Paper>
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <Paper elevation={2} sx={{ p: 2.5, height: "100%" }}>
                <SectionHeader icon={<SecurityIcon />} title="Security Posture" />
                <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1, mb: 1.5 }}>
                  {[84, 72, 90, 66, 96].map((width, index) => (
                    <Skeleton key={index} variant="rounded" width={width} height={24} sx={{ borderRadius: 4 }} />
                  ))}
                </Box>
                <Skeleton variant="text" width={160} />
              </Paper>
            </Grid>
          </Grid>

          <Paper elevation={2} sx={{ p: 2.5 }}>
            <SectionHeader icon={<PieChartIcon />} title="Storage Distribution" />
            <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1.5 }}>
              {[150, 170, 130].map((width, index) => (
                <Skeleton key={index} variant="rounded" width={width} height={36} sx={{ borderRadius: 2 }} />
              ))}
            </Box>
          </Paper>

          <Paper elevation={2} sx={{ p: 2.5 }}>
            <SectionHeader icon={<TrendingUpIcon />} title="Most Downloaded" />
            <Box sx={{ display: "grid", gridTemplateColumns: "2.5fr 1fr 1.5fr 1fr 1.5fr", columnGap: 2, mt: 1 }}>
              {["Resource", "Kind", "Version", "Downloads", "Last Downloaded"].map((heading) => (
                <Typography
                  key={heading}
                  variant="caption"
                  color="text.secondary"
                  fontWeight={600}
                  sx={{ px: 2, py: 1, textTransform: "uppercase", letterSpacing: "0.06em", borderBottom: "1px solid", borderColor: "divider", textAlign: heading === "Downloads" ? "right" : "left" }}
                >
                  {heading}
                </Typography>
              ))}
              {Array.from({ length: 5 }, (_, row) => (
                <React.Fragment key={row}>
                  <Box sx={{ px: 2, py: 1, borderBottom: "1px solid", borderColor: "divider" }}>
                    <Skeleton variant="text" width="45%" />
                    <Skeleton variant="text" width="30%" height={16} />
                  </Box>
                  <Box sx={{ px: 2, display: "flex", alignItems: "center", borderBottom: "1px solid", borderColor: "divider" }}>
                    <Skeleton variant="rounded" width={64} height={24} sx={{ borderRadius: 1.5 }} />
                  </Box>
                  <Box sx={{ px: 2, display: "flex", alignItems: "center", borderBottom: "1px solid", borderColor: "divider" }}>
                    <Skeleton variant="text" width={48} />
                  </Box>
                  <Box sx={{ px: 2, display: "flex", alignItems: "center", justifyContent: "flex-end", borderBottom: "1px solid", borderColor: "divider" }}>
                    <Skeleton variant="text" width={24} />
                  </Box>
                  <Box sx={{ px: 2, display: "flex", alignItems: "center", borderBottom: "1px solid", borderColor: "divider" }}>
                    <Skeleton variant="text" width="50%" />
                  </Box>
                </React.Fragment>
              ))}
            </Box>
          </Paper>
        </Box>
      </Container>
    </>
  );
}
