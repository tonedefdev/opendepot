import * as React from "react";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Grid from "@mui/material/Grid";
import Paper from "@mui/material/Paper";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import Chip from "@mui/material/Chip";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableContainer from "@mui/material/TableContainer";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import LinearProgress from "@mui/material/LinearProgress";
import DownloadIcon from "@mui/icons-material/Download";
import StorageIcon from "@mui/icons-material/Storage";
import SyncIcon from "@mui/icons-material/Sync";
import SecurityIcon from "@mui/icons-material/Security";
import PieChartIcon from "@mui/icons-material/PieChart";
import TrendingUpIcon from "@mui/icons-material/TrendingUp";
import BarChartIcon from "@mui/icons-material/BarChart";
import { SiGooglecloud } from "react-icons/si";
import { FaAws, FaMicrosoft } from "react-icons/fa6";
import { getStats, getDepotsGraph } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { redirect } from "next/navigation";
import RefreshIconButton from "@/components/RefreshIconButton";
import PageHeader from "@/components/PageHeader";
import StatCard from "@/components/StatCard";
import { DepotKindIcon } from "@/components/DepotKindIcon";
import { severityChipSx } from "@/components/severityStyles";
import { NEUTRAL_COLOR, graphKindColor } from "@/lib/kindColors";
import { metaChipSurfaceSx } from "@/components/chipStyles";

// Format bytes into human-readable string.
function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const value = bytes / Math.pow(1024, i);
  return `${value.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

// Capitalise first letter of a string.
function ucfirst(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

const severityColour: Record<string, "error" | "warning" | "info" | "default" | "success"> = {
  critical: "error",
  high: "warning",
  medium: "info",
  low: "success",
  unknown: "default",
};

function storageBackendMeta(backend: string): { icon: React.ReactNode; color: string; label: string } {
  switch (backend.toLowerCase()) {
    case "s3":
      return { icon: <FaAws />, color: "#FF9900", label: "Amazon S3" };
    case "azurestorage":
      return { icon: <FaMicrosoft />, color: "#0078D4", label: "Azure Storage" };
    case "gcs":
      return { icon: <SiGooglecloud />, color: "#4285F4", label: "Google Cloud Storage" };
    default:
      return { icon: <StorageIcon style={{ fontSize: "1.1rem" }} />, color: "#64748b", label: "File System" };
  }
}

export default async function StatsPage() {
  const token = await getServerSessionToken();
  let stats;
  let totalDepots = 0;
  let fetchError: string | null = null;

  try {
    const [statsResult, graphResult] = await Promise.all([
      getStats(undefined, token),
      getDepotsGraph(undefined, token),
    ]);
    stats = statsResult;
    totalDepots = graphResult.summary.totalDepots;
  } catch (err) {
    const msg = err instanceof Error ? err.message : "Failed to load stats.";
    if (msg.includes("401") || msg.includes("unauthorized")) {
      redirect("/login");
    }
    fetchError = msg;
  }

  const syncTotal =
    stats ? stats.syncHealth.syncedVersions + stats.syncHealth.unsyncedVersions + stats.syncHealth.failedVersions : 0;

  return (
    <>
      <PageHeader
        icon={<BarChartIcon color="primary" fontSize="small" />}
        title="Registry Statistics"
        description="Live metrics across all visible modules, providers, skills, agents, and versions."
        actions={<RefreshIconButton />}
      />
      <Container maxWidth="xl" sx={{ py: 3 }}>
      {fetchError && (
        <Alert severity="error" sx={{ mb: 3 }}>
          {fetchError}
        </Alert>
      )}

      {stats && (
        <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
          {/* Summary cards */}
          <Grid container spacing={2}>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 3 }}>
              <StatCard
                label="Modules"
                value={stats.totalModules}
                sub={`${stats.modulesSynced} synced`}
                icon={<DepotKindIcon kind="module" fontSize="small" />}
                accentColor={graphKindColor("module")}
              />
            </Grid>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 3 }}>
              <StatCard
                label="Providers"
                value={stats.totalProviders}
                sub={`${stats.providersSynced} synced`}
                icon={<DepotKindIcon kind="provider" fontSize="small" />}
                accentColor={graphKindColor("provider")}
              />
            </Grid>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 3 }}>
              <StatCard
                label="Skills"
                value={stats.totalSkills}
                sub={`${stats.skillsSynced} synced`}
                icon={<DepotKindIcon kind="skill" fontSize="small" />}
                accentColor={graphKindColor("skill")}
              />
            </Grid>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 3 }}>
              <StatCard
                label="Agents"
                value={stats.totalAgents}
                sub={`${stats.agentsSynced} synced`}
                icon={<DepotKindIcon kind="agent" fontSize="small" />}
                accentColor={graphKindColor("agent")}
              />
            </Grid>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 3 }}>
              <StatCard
                label="Depots"
                value={totalDepots}
                icon={<DepotKindIcon kind="depot" fontSize="small" />}
              />
            </Grid>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 3 }}>
              <StatCard
                label="Versions"
                value={stats.totalVersions}
                sub={`${stats.versionsSynced} synced`}
                icon={<DepotKindIcon kind="version" fontSize="small" />}
                accentColor={graphKindColor("version")}
              />
            </Grid>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 3 }}>
              <StatCard
                label="Storage Used"
                value={formatBytes(stats.totalStorageBytes)}
                icon={<StorageIcon fontSize="small" />}
                accentColor={NEUTRAL_COLOR}
              />
            </Grid>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 3 }}>
              <StatCard
                label={`Downloads (${stats.downloadWindow})`}
                value={stats.totalDownloads.toLocaleString()}
                icon={<DownloadIcon fontSize="small" />}
                accentColor={NEUTRAL_COLOR}
              />
            </Grid>
          </Grid>

          {/* Sync health + Security posture */}
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }} sx={{ display: "flex", flexDirection: "column" }}>
              <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 2 }}>
                <SyncIcon sx={{ fontSize: 22, color: "text.secondary" }} />
                <Typography variant="h6" fontWeight={600}>
                  Sync Health
                </Typography>
              </Box>
              <Paper elevation={2} sx={{ p: 2.5, flex: 1 }}>
                {syncTotal === 0 ? (
                  <Typography variant="body2" color="text.secondary">
                    No versions found.
                  </Typography>
                ) : (
                  <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
                    {[
                      { label: "Synced", count: stats.syncHealth.syncedVersions, colour: "success.main" },
                      { label: "Unsynced", count: stats.syncHealth.unsyncedVersions, colour: "warning.main" },
                      { label: "Failed", count: stats.syncHealth.failedVersions, colour: "error.main" },
                    ].map(({ label, count, colour }) => (
                      <Box key={label}>
                        <Box sx={{ display: "flex", justifyContent: "space-between", mb: 0.5 }}>
                          <Typography variant="body2">{label}</Typography>
                          <Typography variant="body2" fontWeight={500}>
                            {count}/{syncTotal}
                          </Typography>
                        </Box>
                        <LinearProgress
                          variant="determinate"
                          value={syncTotal > 0 ? (count / syncTotal) * 100 : 0}
                          sx={{
                            height: 8,
                            borderRadius: 4,
                            bgcolor: "action.selected",
                            "& .MuiLinearProgress-bar": { bgcolor: colour, borderRadius: 4 },
                          }}
                        />
                      </Box>
                    ))}
                  </Box>
                )}
              </Paper>
            </Grid>

            <Grid size={{ xs: 12, md: 6 }} sx={{ display: "flex", flexDirection: "column" }}>
              <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 2 }}>
                <SecurityIcon sx={{ fontSize: 22, color: "text.secondary" }} />
                <Typography variant="h6" fontWeight={600}>
                  Security Posture
                </Typography>
              </Box>
              <Paper elevation={2} sx={{ p: 2.5, flex: 1 }}>
                {stats.securityPosture.totalAffectedResources === 0 && stats.securityPosture.exempted === 0 ? (
                  <Typography variant="body2" color="text.secondary">
                    No scan findings.
                  </Typography>
                ) : (
                  <>
                    <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1, mb: 1.5 }}>
                      {(["critical", "high", "medium", "low", "unknown"] as const).map((sev) => {
                        const count = stats.securityPosture[sev];
                        if (count === 0) return null;
                        return (
                          <Chip
                            key={sev}
                            label={`${ucfirst(sev)}: ${count}`}
                            color={severityColour[sev]}
                            size="small"
                            sx={severityChipSx(sev)}
                          />
                        );
                      })}
                      {stats.securityPosture.exempted > 0 && (
                        <Chip label={`Exempted: ${stats.securityPosture.exempted}`} color="secondary" size="small" sx={severityChipSx("exempted")} />
                      )}
                    </Box>
                    <Typography variant="caption" color="text.secondary">
                      {stats.securityPosture.totalAffectedResources} resource
                      {stats.securityPosture.totalAffectedResources !== 1 ? "s" : ""} with findings
                    </Typography>
                  </>
                )}
              </Paper>
            </Grid>
          </Grid>

          {/* Storage distribution */}
          {stats.storageDistribution.length > 0 && (
            <Box sx={{ mt: 1 }}>
              <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 2 }}>
                <PieChartIcon sx={{ fontSize: 22, color: "text.secondary" }} />
                <Typography variant="h6" fontWeight={600}>
                  Storage Distribution
                </Typography>
              </Box>
              <Paper elevation={2} sx={{ p: 2.5 }}>
                <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1.5 }}>
                  {stats.storageDistribution
                    .slice()
                    .sort((a, b) => b.count - a.count)
                    .map((s) => {
                      const { icon, color, label } = storageBackendMeta(s.backend);
                      return (
                        <Box
                          key={s.backend}
                          sx={{
                            ...metaChipSurfaceSx,
                            height: "auto",
                            display: "flex",
                            alignItems: "center",
                            gap: 1,
                            px: 1.5,
                            py: 0.75,
                            borderRadius: 2,
                            color: "text.primary",
                          }}
                        >
                          <Box sx={{ color, display: "flex", alignItems: "center", fontSize: "1.25rem" }}>
                            {icon}
                          </Box>
                          <Typography variant="body2" fontWeight={500}>
                            {label}
                          </Typography>
                          <Box
                            sx={{
                              ml: 0.5,
                              px: 0.75,
                              py: 0.1,
                              borderRadius: 1,
                              bgcolor: "action.selected",
                              color: "text.secondary",
                              fontSize: "0.75rem",
                              fontWeight: 700,
                              lineHeight: 1.5,
                            }}
                          >
                            {s.count}
                          </Box>
                        </Box>
                      );
                    })}
                </Box>
              </Paper>
            </Box>
          )}

          {/* Most downloaded */}
          <Box sx={{ mt: 1 }}>
            <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 2 }}>
              <TrendingUpIcon sx={{ fontSize: 22, color: "text.secondary" }} />
              <Typography variant="h6" fontWeight={600}>
                Most Downloaded
              </Typography>
            </Box>
            <Grid container spacing={2}>
              {([
                { label: "Modules", kind: "module", rows: stats.mostDownloaded.modules },
                { label: "Providers", kind: "provider", rows: stats.mostDownloaded.providers },
                { label: "Skills", kind: "skill", rows: stats.mostDownloaded.skills },
                { label: "Agents", kind: "agent", rows: stats.mostDownloaded.agents },
              ] as const).map(({ label, kind, rows }) => (
                <Grid key={label} size={{ xs: 12, md: 6 }}>
                  <Paper elevation={2} sx={{ p: 2.5, height: "100%" }}>
                    <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 1 }}>
                      <DepotKindIcon kind={kind} fontSize="small" />
                      <Typography variant="subtitle1" fontWeight={600}>
                        {label}
                      </Typography>
                    </Box>
                    {rows.length === 0 ? (
                      <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
                        No download events recorded yet.
                      </Typography>
                    ) : (
                      <TableContainer>
                        <Table size="small">
                          <TableHead>
                            <TableRow>
                              <TableCell>Resource</TableCell>
                              <TableCell>Version</TableCell>
                              <TableCell align="right">Downloads</TableCell>
                              <TableCell>Last Downloaded</TableCell>
                            </TableRow>
                          </TableHead>
                          <TableBody>
                            {rows.map((r) => (
                              <TableRow key={`${r.namespace}/${r.name}`} hover>
                                <TableCell>
                                  <Typography variant="body2" fontWeight={500}>
                                    {r.name}
                                  </Typography>
                                  <Typography variant="caption" color="text.secondary">
                                    {r.namespace}
                                  </Typography>
                                </TableCell>
                                <TableCell>
                                  <Typography variant="body2" fontFamily="monospace">
                                    {r.version}
                                  </Typography>
                                </TableCell>
                                <TableCell align="right">
                                  <Typography variant="body2" fontWeight={600}>
                                    {r.downloadCount.toLocaleString()}
                                  </Typography>
                                </TableCell>
                                <TableCell>
                                  <Typography variant="body2" color="text.secondary">
                                    {r.lastDownloadedAt
                                      ? new Date(r.lastDownloadedAt).toLocaleString()
                                      : "—"}
                                  </Typography>
                                </TableCell>
                              </TableRow>
                            ))}
                          </TableBody>
                        </Table>
                      </TableContainer>
                    )}
                  </Paper>
                </Grid>
              ))}
            </Grid>
          </Box>
        </Box>
      )}
      </Container>
    </>
  );
}
