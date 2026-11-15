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
import DownloadIcon from "@mui/icons-material/Download";
import StorageIcon from "@mui/icons-material/Storage";
import SyncIcon from "@mui/icons-material/Sync";
import SecurityIcon from "@mui/icons-material/Security";
import PieChartIcon from "@mui/icons-material/PieChart";
import TrendingUpIcon from "@mui/icons-material/TrendingUp";
import { SiGooglecloud } from "react-icons/si";
import { FaAws, FaMicrosoft } from "react-icons/fa6";
import { getStats, getDepotsGraph, getScanPolicyCatalog } from "@/lib/api";
import type { JevRiskLevelStats } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { redirect } from "next/navigation";
import RefreshIconButton from "@/components/RefreshIconButton";
import BarChartIcon from "@mui/icons-material/BarChart";
import VerifiedUserOutlinedIcon from "@mui/icons-material/VerifiedUserOutlined";
import PageHeader from "@/components/PageHeader";
import OpenNeedsReviewButton from "@/components/OpenNeedsReviewButton";
import StatCard, { StatStrip } from "@/components/StatCard";
import { DepotKindIcon } from "@/components/DepotKindIcon";
import { severityChipSx } from "@/components/severityStyles";
import { ACCENT_COLOR, kindColor } from "@/lib/kindColors";
import { JEV_INPUT_USD_PER_MILLION_TOKENS, formatJevInputCost } from "@/lib/jevCost";
import { metaChipSurfaceSx } from "@/components/chipStyles";
import MobileRecordCard, { MobileRecordList } from "@/components/MobileRecordCard";

// Format bytes into human-readable string.
function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const value = bytes / Math.pow(1024, i);
  return `${value.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

// A count only needs a sync note when part of it is out of sync.
function syncedNote(synced: number, total: number): string | undefined {
  return synced === total ? undefined : `${synced} of ${total} synced`;
}

// Capitalise first letter of a string.
function ucfirst(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

function JevMetric({ label, value, sub, tone }: { label: string; value: string; sub?: string; tone?: "error" | "warning" }) {
  return (
    <Box>
      <Typography variant="caption" color="text.secondary" component="div">
        {label}
      </Typography>
      <Typography variant="h6" fontWeight={600} sx={{ color: tone ? `${tone}.main` : "text.primary", fontVariantNumeric: "tabular-nums" }}>
        {value}
      </Typography>
      {sub && (
        <Typography variant="caption" color="text.secondary">
          {sub}
        </Typography>
      )}
    </Box>
  );
}

const severityColour: Record<string, "error" | "warning" | "info" | "default" | "success"> = {
  critical: "error",
  high: "warning",
  medium: "info",
  low: "success",
  unknown: "default",
};

// JEV_RISK_LEVELS orders Jev risk levels from lowest to highest. Colour loudness rises with severity, so Critical
// is the strongest red and High sits below it in amber rather than a paler red.
const JEV_RISK_LEVELS: { key: keyof JevRiskLevelStats; label: string; colour: string }[] = [
  { key: "minimal", label: "Minimal", colour: "success.light" },
  { key: "low", label: "Low", colour: "success.main" },
  { key: "moderate", label: "Moderate", colour: "warning.light" },
  { key: "high", label: "High", colour: "warning.main" },
  { key: "critical", label: "Critical", colour: "error.main" },
];

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
  let securityPoliciesEnabled = false;

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

  // The queue link follows the sidebar: it appears only when Security Policies is enabled.
  if (token) {
    try {
      securityPoliciesEnabled = (await getScanPolicyCatalog(token)).writesEnabled;
    } catch {
      // Leave the queue link hidden when the policy catalog is unavailable.
    }
  }

  const syncTotal =
    stats ? stats.syncHealth.syncedVersions + stats.syncHealth.unsyncedVersions + stats.syncHealth.failedVersions : 0;

  const syncSegments = stats
    ? [
        { label: "Synced", count: stats.syncHealth.syncedVersions, colour: "success.main" },
        { label: "Unsynced", count: stats.syncHealth.unsyncedVersions, colour: "warning.main" },
        { label: "Failed", count: stats.syncHealth.failedVersions, colour: "error.main" },
      ]
    : [];

  const jevRisk = stats
    ? JEV_RISK_LEVELS.map((level) => ({ label: level.label, colour: level.colour, count: stats.jev.riskLevels[level.key] }))
    : [];
  const jevScored = jevRisk.reduce((sum, seg) => sum + seg.count, 0);

  return (
    <>
      <PageHeader
        icon={<BarChartIcon fontSize="small" />}
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
          <StatStrip columns={8}>
            <StatCard
              label="Modules"
              value={stats.totalModules}
              sub={syncedNote(stats.modulesSynced, stats.totalModules)}
              icon={<DepotKindIcon kind="module" fontSize="small" />}
              accentColor={kindColor("module")}
            />
            <StatCard
              label="Providers"
              value={stats.totalProviders}
              sub={syncedNote(stats.providersSynced, stats.totalProviders)}
              icon={<DepotKindIcon kind="provider" fontSize="small" />}
              accentColor={kindColor("provider")}
            />
            <StatCard
              label="Skills"
              value={stats.totalSkills}
              sub={syncedNote(stats.skillsSynced, stats.totalSkills)}
              icon={<DepotKindIcon kind="skill" fontSize="small" />}
              accentColor={kindColor("skill")}
            />
            <StatCard
              label="Agents"
              value={stats.totalAgents}
              sub={syncedNote(stats.agentsSynced, stats.totalAgents)}
              icon={<DepotKindIcon kind="agent" fontSize="small" />}
              accentColor={kindColor("agent")}
            />
            <StatCard
              label="Depots"
              value={totalDepots}
              icon={<DepotKindIcon kind="depot" fontSize="small" />}
            />
            <StatCard
              label="Versions"
              value={stats.totalVersions}
              sub={syncedNote(stats.versionsSynced, stats.totalVersions)}
              icon={<DepotKindIcon kind="version" fontSize="small" />}
              accentColor={ACCENT_COLOR}
            />
            <StatCard
              label="Storage Used"
              value={formatBytes(stats.totalStorageBytes)}
              icon={<StorageIcon fontSize="small" />}
              accentColor={ACCENT_COLOR}
            />
            <StatCard
              label={`Downloads (${stats.downloadWindow})`}
              value={stats.totalDownloads.toLocaleString()}
              icon={<DownloadIcon fontSize="small" />}
              accentColor={ACCENT_COLOR}
            />
          </StatStrip>

          {/* Sync health + Security posture */}
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }} sx={{ display: "flex", flexDirection: "column" }}>
              <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 2 }}>
                <SyncIcon sx={{ fontSize: 22, color: "primary.main" }} />
                <Typography variant="h6" fontWeight={600}>
                  Sync Health
                </Typography>
              </Box>
              <Paper variant="outlined" sx={{ p: 2.5, flex: 1 }}>
                {syncTotal === 0 ? (
                  <Typography variant="body2" color="text.secondary">
                    No versions found.
                  </Typography>
                ) : (
                  <>
                    <Box
                      role="img"
                      aria-label={syncSegments.map((seg) => `${seg.count} ${seg.label.toLowerCase()}`).join(", ")}
                      sx={{ display: "flex", gap: "2px", height: 8, mb: 2, borderRadius: 4, overflow: "hidden", bgcolor: "action.selected" }}
                    >
                      {syncSegments
                        .filter((seg) => seg.count > 0)
                        .map((seg) => (
                          <Box key={seg.label} sx={{ width: `${(seg.count / syncTotal) * 100}%`, bgcolor: seg.colour }} />
                        ))}
                    </Box>
                    <Box sx={{ display: "flex", flexWrap: "wrap", columnGap: 3, rowGap: 0.5 }}>
                      {syncSegments.map((seg) => (
                        <Box key={seg.label} sx={{ display: "flex", alignItems: "center", gap: 0.75 }}>
                          <Box sx={{ width: 8, height: 8, borderRadius: "2px", bgcolor: seg.colour }} />
                          <Typography variant="body2" color="text.secondary">
                            {seg.label}
                          </Typography>
                          <Typography variant="body2" fontWeight={600} sx={{ fontVariantNumeric: "tabular-nums" }}>
                            {seg.count}
                          </Typography>
                        </Box>
                      ))}
                    </Box>
                  </>
                )}
              </Paper>
            </Grid>

            <Grid size={{ xs: 12, md: 6 }} sx={{ display: "flex", flexDirection: "column" }}>
              <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 2 }}>
                <SecurityIcon sx={{ fontSize: 22, color: "primary.main" }} />
                <Typography variant="h6" fontWeight={600}>
                  Security Posture
                </Typography>
              </Box>
              <Paper variant="outlined" sx={{ p: 2.5, flex: 1 }}>
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

          {/* Jev assessment */}
          <Box>
            <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 2 }}>
              <VerifiedUserOutlinedIcon sx={{ fontSize: 22, color: "primary.main" }} />
              <Typography variant="h6" fontWeight={600}>
                Jev Assessment
              </Typography>
            </Box>
            <Paper variant="outlined" sx={{ p: 2.5 }}>
              {stats.jev.assessedVersions === 0 ? (
                <Typography variant="body2" color="text.secondary">
                  No Versions have been assessed by Jev. Jev is off by default.
                </Typography>
              ) : (
                <Grid container spacing={3}>
                  <Grid size={{ xs: 12, md: 7 }}>
                    <Box sx={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(132px, 1fr))", gap: 2.5 }}>
                      <JevMetric label="Assessed" value={stats.jev.assessedVersions.toLocaleString()} />
                      <JevMetric label="Estimated cost" value={formatJevInputCost(stats.jev.inputTokens)} />
                      <JevMetric label="Input tokens" value={stats.jev.inputTokens.toLocaleString()} />
                      <JevMetric label="Chunks assessed" value={stats.jev.assessedChunks.toLocaleString()} />
                      <JevMetric
                        label="Blocked"
                        value={stats.jev.blockedVersions.toLocaleString()}
                        tone={stats.jev.blockedVersions > 0 ? "error" : undefined}
                      />
                      {securityPoliciesEnabled ? (
                        <OpenNeedsReviewButton>
                          <JevMetric
                            label="Needs review"
                            value={stats.jev.needsReviewVersions.toLocaleString()}
                            sub="Open queue"
                            tone={stats.jev.needsReviewVersions > 0 ? "warning" : undefined}
                          />
                        </OpenNeedsReviewButton>
                      ) : (
                        <JevMetric
                          label="Needs review"
                          value={stats.jev.needsReviewVersions.toLocaleString()}
                          tone={stats.jev.needsReviewVersions > 0 ? "warning" : undefined}
                        />
                      )}
                      <JevMetric label="Exempted" value={stats.jev.exemptedVersions.toLocaleString()} />
                      <JevMetric
                        label="Failed"
                        value={stats.jev.failedVersions.toLocaleString()}
                        tone={stats.jev.failedVersions > 0 ? "error" : undefined}
                      />
                    </Box>
                    <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: 2 }}>
                      Estimated from the input tokens Jev reported across all visible Versions, at {`$${JEV_INPUT_USD_PER_MILLION_TOKENS}`} per million.
                      Jev does not bill output tokens.
                    </Typography>
                  </Grid>

                  <Grid size={{ xs: 12, md: 5 }}>
                    <Typography variant="subtitle2" fontWeight={600} sx={{ mb: 1 }}>
                      Risk levels
                    </Typography>
                    {jevScored === 0 ? (
                      <Typography variant="body2" color="text.secondary">
                        No scored assessments.
                      </Typography>
                    ) : (
                      <>
                        <Box
                          role="img"
                          aria-label={jevRisk.map((seg) => `${seg.count} ${seg.label.toLowerCase()}`).join(", ")}
                          sx={{ display: "flex", gap: "2px", height: 8, mb: 2, borderRadius: 4, overflow: "hidden", bgcolor: "action.selected" }}
                        >
                          {jevRisk
                            .filter((seg) => seg.count > 0)
                            .map((seg) => (
                              <Box key={seg.label} sx={{ width: `${(seg.count / jevScored) * 100}%`, bgcolor: seg.colour }} />
                            ))}
                        </Box>
                        <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
                          {jevRisk.map((seg) => (
                            <Box key={seg.label} sx={{ display: "flex", alignItems: "center", gap: 0.75 }}>
                              <Box sx={{ width: 8, height: 8, borderRadius: "2px", bgcolor: seg.colour }} />
                              <Typography variant="body2" color="text.secondary" sx={{ flex: 1 }}>
                                {seg.label}
                              </Typography>
                              <Typography variant="body2" fontWeight={600} sx={{ fontVariantNumeric: "tabular-nums" }}>
                                {seg.count}
                              </Typography>
                            </Box>
                          ))}
                        </Box>
                      </>
                    )}
                  </Grid>
                </Grid>
              )}
            </Paper>
          </Box>

          {/* Storage distribution */}
          {stats.storageDistribution.length > 0 && (
            <Box sx={{ mt: 1 }}>
              <Box display="flex" alignItems="center" gap={0.75} sx={{ mb: 2 }}>
                <PieChartIcon sx={{ fontSize: 22, color: "primary.main" }} />
                <Typography variant="h6" fontWeight={600}>
                  Storage Distribution
                </Typography>
              </Box>
              <Paper variant="outlined" sx={{ p: 2.5 }}>
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
                              color: "text.primary",
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
              <TrendingUpIcon sx={{ fontSize: 22, color: "primary.main" }} />
              <Typography variant="h6" fontWeight={600}>
                Top 10 Most Downloaded
              </Typography>
            </Box>
            <Typography variant="body2" color="text.secondary" sx={{ mt: -1.5, mb: 2 }}>
              Ranked by total downloads within each kind. Each list shows the top 10 only.
            </Typography>
            <Grid container spacing={2}>
              {([
                { label: "Modules", kind: "module", rows: stats.mostDownloaded.modules },
                { label: "Providers", kind: "provider", rows: stats.mostDownloaded.providers },
                { label: "Skills", kind: "skill", rows: stats.mostDownloaded.skills },
                { label: "Agents", kind: "agent", rows: stats.mostDownloaded.agents },
              ] as const).map(({ label, kind, rows }) => (
                <Grid key={label} size={{ xs: 12, md: 6 }}>
                  <Paper variant="outlined" sx={{ p: 2.5, height: "100%" }}>
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
                      <>
                      <TableContainer component={Paper} variant="outlined" sx={{ display: { xs: "none", lg: "block" } }}>
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
                                  <Typography variant="body2" fontFamily="var(--od-font-mono)">
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
                      <MobileRecordList>
                        {rows.map((r) => (
                          <MobileRecordCard
                            key={`${r.namespace}/${r.name}`}
                            title={
                              <Box>
                                <Typography variant="body2" fontWeight={500}>
                                  {r.name}
                                </Typography>
                                <Typography variant="caption" color="text.secondary">
                                  {r.namespace}
                                </Typography>
                              </Box>
                            }
                            fields={[
                              {
                                label: "Version",
                                value: <Typography component="span" variant="body2" fontFamily="var(--od-font-mono)">{r.version}</Typography>,
                              },
                              {
                                label: "Downloads",
                                value: <Typography component="span" variant="body2" fontWeight={600}>{r.downloadCount.toLocaleString()}</Typography>,
                              },
                              {
                                label: "Last Downloaded",
                                wide: true,
                                value: r.lastDownloadedAt ? new Date(r.lastDownloadedAt).toLocaleString() : "—",
                              },
                            ]}
                          />
                        ))}
                      </MobileRecordList>
                      </>
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
