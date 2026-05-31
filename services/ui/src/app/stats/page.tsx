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
import ExtensionIcon from "@mui/icons-material/Extension";
import CloudQueueIcon from "@mui/icons-material/CloudQueue";
import AccountTreeIcon from "@mui/icons-material/AccountTree";
import StorageIcon from "@mui/icons-material/Storage";
import DownloadIcon from "@mui/icons-material/Download";
import WarehouseIcon from "@mui/icons-material/Warehouse";
import SyncIcon from "@mui/icons-material/Sync";
import SecurityIcon from "@mui/icons-material/Security";
import PieChartIcon from "@mui/icons-material/PieChart";
import TrendingUpIcon from "@mui/icons-material/TrendingUp";
import { SiGooglecloud } from "react-icons/si";
import { FaAws, FaMicrosoft } from "react-icons/fa6";
import { getStats, getDepotsGraph } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { redirect } from "next/navigation";
import RefreshIconButton from "@/components/RefreshIconButton";

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

interface StatCardProps {
  label: string;
  value: string | number;
  sub?: string;
  icon: React.ReactNode;
  accentColor: string;
}

function StatCard({ label, value, sub, icon, accentColor }: StatCardProps) {
  return (
    <Paper
      elevation={3}
      sx={{
        height: "100%",
        overflow: "hidden",
        borderTop: `4px solid ${accentColor}`,
      }}
    >
      <Box sx={{ p: { xs: 1.5, sm: 2, lg: 2.5 } }}>
        <Box sx={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", mb: 1 }}>
          <Typography
            variant="body2"
            color="text.secondary"
            fontWeight={500}
            sx={{ fontSize: { xs: "0.75rem", lg: "0.875rem" } }}
          >
            {label}
          </Typography>
          <Box
            sx={{
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              width: { xs: 32, lg: 36 },
              height: { xs: 32, lg: 36 },
              borderRadius: 1.5,
              bgcolor: `${accentColor}18`,
              color: accentColor,
              flexShrink: 0,
              "& svg": { fontSize: { xs: "1.1rem", md: "0.95rem", lg: "1.25rem" } },
            }}
          >
            {icon}
          </Box>
        </Box>
        <Typography
          fontWeight={700}
          sx={{
            lineHeight: 1.1,
            fontSize: { xs: "1.5rem", sm: "1.75rem", lg: "2rem" },
          }}
        >
          {value}
        </Typography>
        {sub && (
          <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: "block" }}>
            {sub}
          </Typography>
        )}
      </Box>
    </Paper>
  );
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
      redirect("/auth/login");
    }
    fetchError = msg;
  }

  const syncTotal =
    stats ? stats.syncHealth.syncedVersions + stats.syncHealth.unsyncedVersions + stats.syncHealth.failedVersions : 0;

  return (
    <Container maxWidth="xl" sx={{ py: 3 }}>
      <Box display="flex" alignItems="center" gap={1} sx={{ mb: 0.5 }}>
        <Typography variant="h5" fontWeight={600}>
          Registry Statistics
        </Typography>
        <RefreshIconButton />
      </Box>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 3 }}>
        Live metrics across all visible modules, providers, and versions.
      </Typography>

      {fetchError && (
        <Alert severity="error" sx={{ mb: 3 }}>
          {fetchError}
        </Alert>
      )}

      {stats && (
        <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
          {/* Summary cards */}
          <Grid container spacing={2}>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 2 }}>
              <StatCard
                label="Modules"
                value={stats.totalModules}
                icon={<ExtensionIcon fontSize="small" />}
                accentColor="#6366f1"
              />
            </Grid>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 2 }}>
              <StatCard
                label="Providers"
                value={stats.totalProviders}
                icon={<CloudQueueIcon fontSize="small" />}
                accentColor="#0ea5e9"
              />
            </Grid>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 2 }}>
              <StatCard
                label="Versions"
                value={stats.totalVersions}
                icon={<AccountTreeIcon fontSize="small" />}
                accentColor="#8b5cf6"
              />
            </Grid>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 2 }}>
              <StatCard
                label="Depots"
                value={totalDepots}
                icon={<WarehouseIcon fontSize="small" />}
                accentColor="#f97316"
              />
            </Grid>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 2 }}>
              <StatCard
                label="Storage Used"
                value={formatBytes(stats.totalStorageBytes)}
                icon={<StorageIcon fontSize="small" />}
                accentColor="#f59e0b"
              />
            </Grid>
            <Grid size={{ xs: 6, sm: 4, md: 4, lg: 2 }}>
              <StatCard
                label="Total Downloads"
                value={stats.totalDownloads.toLocaleString()}
                icon={<DownloadIcon fontSize="small" />}
                accentColor="#10b981"
              />
            </Grid>
          </Grid>

          {/* Sync health + Security posture */}
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <Paper elevation={2} sx={{ p: 2.5, height: "100%" }}>
                <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 1 }}>
                  <SyncIcon sx={{ fontSize: 18, color: "text.secondary" }} />
                  <Typography variant="subtitle1" fontWeight={600}>
                    Sync Health
                  </Typography>
                </Box>
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
                          sx={{ height: 8, borderRadius: 4, "& .MuiLinearProgress-bar": { bgcolor: colour } }}
                        />
                      </Box>
                    ))}
                  </Box>
                )}
              </Paper>
            </Grid>

            <Grid size={{ xs: 12, md: 6 }}>
              <Paper elevation={2} sx={{ p: 2.5, height: "100%" }}>
                <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 1 }}>
                  <SecurityIcon sx={{ fontSize: 18, color: "text.secondary" }} />
                  <Typography variant="subtitle1" fontWeight={600}>
                    Security Posture
                  </Typography>
                </Box>
                {stats.securityPosture.totalAffectedResources === 0 ? (
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
                            sx={{ color: "#fff" }}
                          />
                        );
                      })}
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
            <Paper elevation={2} sx={{ p: 2.5 }}>
              <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 1 }}>
                <PieChartIcon sx={{ fontSize: 18, color: "text.secondary" }} />
                <Typography variant="subtitle1" fontWeight={600}>
                  Storage Distribution
                </Typography>
              </Box>
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
                          display: "flex",
                          alignItems: "center",
                          gap: 1,
                          px: 1.5,
                          py: 0.75,
                          borderRadius: 2,
                          border: "1px solid",
                          borderColor: `${color}40`,
                          bgcolor: `${color}0d`,
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
                            bgcolor: `${color}20`,
                            color,
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
          )}

          {/* Most downloaded */}
          <Paper elevation={2} sx={{ p: 2.5 }}>
            <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 1 }}>
              <TrendingUpIcon sx={{ fontSize: 18, color: "text.secondary" }} />
              <Typography variant="subtitle1" fontWeight={600}>
                Most Downloaded
              </Typography>
            </Box>
            {stats.mostDownloaded.length === 0 ? (
              <Typography variant="body2" color="text.secondary">
                No download events recorded yet.
              </Typography>
            ) : (
              <TableContainer>
                <Table size="small">
                  <TableHead>
                    <TableRow>
                      <TableCell>Resource</TableCell>
                      <TableCell>Kind</TableCell>
                      <TableCell>Version</TableCell>
                      <TableCell align="right">Downloads</TableCell>
                      <TableCell>Last Downloaded</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {stats.mostDownloaded.map((r, i) => (
                      <TableRow key={i} hover>
                        <TableCell>
                          <Typography variant="body2" fontWeight={500}>
                            {r.name}
                          </Typography>
                          <Typography variant="caption" color="text.secondary">
                            {r.namespace}
                          </Typography>
                        </TableCell>
                        <TableCell>
                          <Chip label={r.kind} size="small" variant="outlined" />
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
        </Box>
      )}
    </Container>
  );
}
