import * as React from "react";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Grid from "@mui/material/Grid";
import Paper from "@mui/material/Paper";
import Skeleton from "@mui/material/Skeleton";
import Typography from "@mui/material/Typography";
import BarChartIcon from "@mui/icons-material/BarChart";
import DownloadIcon from "@mui/icons-material/Download";
import PieChartIcon from "@mui/icons-material/PieChart";
import SecurityIcon from "@mui/icons-material/Security";
import StorageIcon from "@mui/icons-material/Storage";
import SyncIcon from "@mui/icons-material/Sync";
import TrendingUpIcon from "@mui/icons-material/TrendingUp";
import PageHeader from "@/components/PageHeader";
import StatCard from "@/components/StatCard";
import { DepotKindIcon } from "@/components/DepotKindIcon";
import { NEUTRAL_COLOR, graphKindColor } from "@/lib/kindColors";

const statCards = [
  { label: "Modules", icon: <DepotKindIcon kind="module" fontSize="small" />, accentColor: graphKindColor("module") },
  { label: "Providers", icon: <DepotKindIcon kind="provider" fontSize="small" />, accentColor: graphKindColor("provider") },
  { label: "Skills", icon: <DepotKindIcon kind="skill" fontSize="small" />, accentColor: graphKindColor("skill") },
  { label: "Agents", icon: <DepotKindIcon kind="agent" fontSize="small" />, accentColor: graphKindColor("agent") },
  { label: "Versions", icon: <DepotKindIcon kind="version" fontSize="small" />, accentColor: graphKindColor("version") },
  { label: "Depots", icon: <DepotKindIcon kind="depot" fontSize="small" /> },
  { label: "Storage Used", icon: <StorageIcon fontSize="small" />, accentColor: NEUTRAL_COLOR },
  { label: "Downloads", icon: <DownloadIcon fontSize="small" />, accentColor: NEUTRAL_COLOR },
];

function SectionHeader({ icon, title }: { icon: React.ReactNode; title: string }) {
  return (
    <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 2, "& svg": { fontSize: 22, color: "text.secondary" } }}>
      {icon}
      <Typography variant="h6" fontWeight={600}>
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
        description="Live metrics across all visible modules, providers, skills, agents, and versions."
        mobileOnly
      />
      <Container maxWidth="xl" sx={{ py: 3 }} role="status" aria-label="Loading statistics">
        <Box sx={{ mb: 0.5, display: { xs: "none", sm: "flex" }, alignItems: "center", minHeight: 40 }}>
          <Typography variant="h5" fontWeight={600}>
            Registry Statistics
          </Typography>
        </Box>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 3, display: { xs: "none", sm: "block" } }}>
          Live metrics across all visible modules, providers, skills, agents, and versions.
        </Typography>

        <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
          <Grid container spacing={2}>
            {statCards.map((card) => (
              <Grid key={card.label} size={{ xs: 6, sm: 4, md: 4, lg: 3 }}>
                <StatCard loading label={card.label} value={0} icon={card.icon} accentColor={card.accentColor} />
              </Grid>
            ))}
          </Grid>

          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }} sx={{ display: "flex", flexDirection: "column" }}>
              <SectionHeader icon={<SyncIcon />} title="Sync Health" />
              <Paper elevation={2} sx={{ p: 2.5, flex: 1 }}>
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
            <Grid size={{ xs: 12, md: 6 }} sx={{ display: "flex", flexDirection: "column" }}>
              <SectionHeader icon={<SecurityIcon />} title="Security Posture" />
              <Paper elevation={2} sx={{ p: 2.5, flex: 1 }}>
                <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1, mb: 1.5 }}>
                  {[84, 72, 90, 66, 96].map((width, index) => (
                    <Skeleton key={index} variant="rounded" width={width} height={24} sx={{ borderRadius: 4 }} />
                  ))}
                </Box>
                <Skeleton variant="text" width={160} />
              </Paper>
            </Grid>
          </Grid>

          <Box sx={{ mt: 1 }}>
            <SectionHeader icon={<PieChartIcon />} title="Storage Distribution" />
            <Paper elevation={2} sx={{ p: 2.5 }}>
              <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1.5 }}>
                {[150, 170, 130].map((width, index) => (
                  <Skeleton key={index} variant="rounded" width={width} height={36} sx={{ borderRadius: 2 }} />
                ))}
              </Box>
            </Paper>
          </Box>

          <Box sx={{ mt: 1 }}>
            <Box sx={{ display: "flex", alignItems: "center", gap: 1, mb: 2 }}>
              <TrendingUpIcon sx={{ fontSize: 22, color: "text.secondary" }} />
              <Typography variant="h6" fontWeight={600}>
                Most Downloaded
              </Typography>
            </Box>
            <Grid container spacing={2}>
              {["Modules", "Providers", "Skills", "Agents"].map((label) => (
                <Grid key={label} size={{ xs: 12, md: 6 }}>
                  <Paper elevation={2} sx={{ p: 2.5, height: "100%" }}>
                    <Box sx={{ display: "flex", alignItems: "center", gap: 0.75, mb: 1 }}>
                      <Skeleton variant="circular" width={18} height={18} />
                      <Skeleton variant="text" width={80} />
                    </Box>
                    {Array.from({ length: 3 }, (_, row) => (
                      <Box key={row} sx={{ display: "flex", justifyContent: "space-between", alignItems: "center", py: 1, borderBottom: "1px solid", borderColor: "divider" }}>
                        <Skeleton variant="text" width="45%" />
                        <Skeleton variant="text" width={48} />
                      </Box>
                    ))}
                  </Paper>
                </Grid>
              ))}
            </Grid>
          </Box>
        </Box>
      </Container>
    </>
  );
}
