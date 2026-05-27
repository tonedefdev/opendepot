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
import { getStats } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";

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
}

function StatCard({ label, value, sub }: StatCardProps) {
  return (
    <Paper elevation={2} sx={{ p: 2.5, height: "100%" }}>
      <Typography variant="body2" color="text.secondary" gutterBottom>
        {label}
      </Typography>
      <Typography variant="h4" fontWeight={600}>
        {value}
      </Typography>
      {sub && (
        <Typography variant="caption" color="text.secondary">
          {sub}
        </Typography>
      )}
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

export default async function StatsPage() {
  const token = await getServerSessionToken();
  let stats;
  let fetchError: string | null = null;

  try {
    stats = await getStats(undefined, token);
  } catch (err) {
    fetchError = err instanceof Error ? err.message : "Failed to load stats.";
  }

  const syncTotal =
    stats ? stats.syncHealth.syncedVersions + stats.syncHealth.unsyncedVersions + stats.syncHealth.failedVersions : 0;

  return (
    <Container maxWidth="xl" sx={{ py: 3 }}>
      <Typography variant="h5" fontWeight={600} gutterBottom>
        Registry Statistics
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
            <Grid item xs={6} sm={4} md={2}>
              <StatCard label="Modules" value={stats.totalModules} />
            </Grid>
            <Grid item xs={6} sm={4} md={2}>
              <StatCard label="Providers" value={stats.totalProviders} />
            </Grid>
            <Grid item xs={6} sm={4} md={2}>
              <StatCard label="Versions" value={stats.totalVersions} />
            </Grid>
            <Grid item xs={6} sm={4} md={2}>
              <StatCard
                label="Storage Used"
                value={formatBytes(stats.totalStorageBytes)}
              />
            </Grid>
            <Grid item xs={6} sm={4} md={2}>
              <StatCard
                label="Total Downloads"
                value={stats.totalDownloads.toLocaleString()}
              />
            </Grid>
          </Grid>

          {/* Sync health + Security posture */}
          <Grid container spacing={2}>
            <Grid item xs={12} md={6}>
              <Paper elevation={2} sx={{ p: 2.5, height: "100%" }}>
                <Typography variant="subtitle1" fontWeight={600} gutterBottom>
                  Sync Health
                </Typography>
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

            <Grid item xs={12} md={6}>
              <Paper elevation={2} sx={{ p: 2.5, height: "100%" }}>
                <Typography variant="subtitle1" fontWeight={600} gutterBottom>
                  Security Posture
                </Typography>
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
              <Typography variant="subtitle1" fontWeight={600} gutterBottom>
                Storage Distribution
              </Typography>
              <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
                {stats.storageDistribution
                  .slice()
                  .sort((a, b) => b.count - a.count)
                  .map((s) => (
                    <Chip key={s.backend} label={`${s.backend}: ${s.count}`} variant="outlined" />
                  ))}
              </Box>
            </Paper>
          )}

          {/* Most downloaded */}
          <Paper elevation={2} sx={{ p: 2.5 }}>
            <Typography variant="subtitle1" fontWeight={600} gutterBottom>
              Most Downloaded
            </Typography>
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
