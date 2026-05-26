import * as React from "react";
import Container from "@mui/material/Container";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Chip from "@mui/material/Chip";
import Divider from "@mui/material/Divider";
import Alert from "@mui/material/Alert";
import Card from "@mui/material/Card";
import CardContent from "@mui/material/CardContent";
import IconButton from "@mui/material/IconButton";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import Breadcrumbs from "@mui/material/Breadcrumbs";
import Tooltip from "@mui/material/Tooltip";
import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import SyncProblemIcon from "@mui/icons-material/SyncProblem";
import ErrorIcon from "@mui/icons-material/Error";
import WarningAmberIcon from "@mui/icons-material/WarningAmber";
import StorageIcon from "@mui/icons-material/Storage";
import WarehouseIcon from "@mui/icons-material/Warehouse";
import GitHubIcon from "@mui/icons-material/GitHub";
import InventoryIcon from "@mui/icons-material/Inventory";
import Link from "next/link";
import SeverityBadge from "@/components/SeverityBadge";
import ScanDrillDown from "@/components/ScanDrillDown";
import ProviderLogo from "@/components/ProviderLogo";
import CopyButton from "@/components/CopyButton";
import { getResourceDetail, listDepots } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { notFound } from "next/navigation";

interface PageProps {
  params: Promise<{
    namespace: string;
    kind: string;
    name: string;
  }>;
}

function SectionCard({
  icon,
  title,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <Card sx={{ mb: 2 }}>
      <CardContent>
        <Box display="flex" alignItems="center" gap={1} mb={2}>
          <Box sx={{ color: "primary.main", display: "flex" }}>{icon}</Box>
          <Typography variant="h6" sx={{ fontSize: "0.9375rem", fontWeight: 600 }}>
            {title}
          </Typography>
        </Box>
        {children}
      </CardContent>
    </Card>
  );
}

function LabelValue({ label, value }: { label: string; value?: React.ReactNode }) {
  if (!value && value !== 0) return null;
  return (
    <Box display="flex" gap={1} mb={0.75} alignItems="flex-start">
      <Typography
        variant="caption"
        sx={{
          color: "text.secondary",
          minWidth: 150,
          flexShrink: 0,
          pt: "2px",
          textTransform: "uppercase",
          letterSpacing: "0.04em",
          fontSize: "0.7rem",
          fontWeight: 600,
        }}
      >
        {label}
      </Typography>
      <Typography variant="body2" sx={{ fontFamily: "monospace", fontSize: "0.8125rem", wordBreak: "break-all" }}>
        {value}
      </Typography>
    </Box>
  );
}

function displayVersion(v: string): string {
  if (!v) return "";
  return v.startsWith("v") ? v : `v${v}`;
}

function compareVersionDesc(a: string, b: string): number {
  const normalize = (v: string) => v.replace(/^v/i, "");
  const tokenize = (v: string) =>
    normalize(v)
      .split(/[.+-]/)
      .map((part) => {
        const numeric = Number(part);
        return Number.isFinite(numeric) && part !== "" ? numeric : part.toLowerCase();
      });

  const aTokens = tokenize(a);
  const bTokens = tokenize(b);
  const length = Math.max(aTokens.length, bTokens.length);

  for (let i = 0; i < length; i += 1) {
    const left = aTokens[i];
    const right = bTokens[i];

    if (left === undefined) return 1;
    if (right === undefined) return -1;
    if (left === right) continue;

    if (typeof left === "number" && typeof right === "number") {
      return right - left;
    }

    if (typeof left === "number") return -1;
    if (typeof right === "number") return 1;

    return String(right).localeCompare(String(left));
  }

  return 0;
}

export default async function ResourceDetailPage({ params }: PageProps) {
  const { namespace, kind, name } = await params;
  const token = await getServerSessionToken();

  let detail;
  try {
    detail = await getResourceDetail(namespace, kind, name, token);
  } catch (err) {
    const msg = err instanceof Error ? err.message : "";
    if (msg.includes("404")) {
      notFound();
    }
    return (
      <Container maxWidth="xl" sx={{ py: 4 }}>
        <Alert severity="error">Failed to load resource detail: {msg}</Alert>
      </Container>
    );
  }

  const capitalizeKind = kind.charAt(0).toUpperCase() + kind.slice(1);
  const depots = detail.depotRef ? await listDepots(token).catch(() => undefined) : undefined;
  const managingDepot = detail.depotRef
    ? depots?.items.find((d) => d.namespace === detail.depotRef?.namespace && d.name === detail.depotRef?.name)
    : undefined;
  const hasStorageConfigValues =
    !!detail.storageConfig?.backend ||
    !!detail.storageConfig?.bucket ||
    !!detail.storageConfig?.region ||
    !!detail.storageConfig?.key ||
    !!detail.storageConfig?.directoryPath ||
    !!detail.storageConfig?.accountName ||
    !!detail.storageConfig?.accountUrl ||
    !!detail.storageConfig?.subscriptionID ||
    !!detail.storageConfig?.resourceGroup;
  const sortedVersions = [...(detail.versions ?? [])].sort((a, b) => compareVersionDesc(a.version, b.version));
  const hasUnsyncedVersions = (detail.versions ?? []).some(
    (v) => !v.synced || /failed|error/i.test(v.syncStatus ?? ""),
  );

  return (
    <Container maxWidth="xl" sx={{ py: 4, px: { xs: 2, md: 4 } }}>
      {/* Breadcrumbs */}
      <Breadcrumbs sx={{ mb: 3, "& a": { textDecoration: "none", color: "text.secondary", "&:hover": { color: "primary.main" } } }}>
        <Link href="/">Registry</Link>
        <Link href={`/?namespace=${namespace}`}>
          <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
            {namespace}
          </Typography>
        </Link>
        <Link href={`/?kind=${kind}`}>{capitalizeKind}</Link>
        <Typography variant="body2" color="text.primary" fontWeight={600}>
          {name}
        </Typography>
      </Breadcrumbs>

      {/* Hero */}
      <Box
        sx={{
          display: "flex",
          alignItems: "flex-start",
          gap: 2.5,
          mb: 4,
          p: 3,
          borderRadius: 2,
          border: "1px solid rgba(4,207,208,0.2)",
          background: "linear-gradient(135deg, rgba(4,207,208,0.06) 0%, rgba(3,222,184,0.03) 100%)",
        }}
      >
        {detail.provider && <ProviderLogo provider={detail.provider} size={44} />}

        <Box flex={1} minWidth={0}>
          <Typography variant="h4" component="h1" sx={{ fontWeight: 700, mb: 0.5, wordBreak: "break-word" }}>
            {detail.name}
          </Typography>
          <Typography variant="body2" sx={{ color: "text.secondary", fontFamily: "monospace", mb: 1.5 }}>
            {namespace} / {capitalizeKind}
          </Typography>

          <Box display="flex" flexWrap="wrap" gap={1} alignItems="center">
            <Chip label={capitalizeKind} color="primary" size="small" />
            {detail.provider && (
              <Chip label={detail.provider} size="small" variant="outlined" sx={{ fontFamily: "monospace" }} />
            )}
            {detail.latestVersion && (
              <Chip
                label={displayVersion(detail.latestVersion)}
                size="small"
                sx={{
                  fontFamily: "monospace",
                  background: "rgba(4,207,208,0.12)",
                  color: "#04cfd0",
                  border: "1px solid rgba(4,207,208,0.3)",
                }}
              />
            )}
            <Box display="flex" alignItems="center" gap={0.5}>
              {/failed|error/i.test(detail.syncStatus) ? (
                <ErrorIcon sx={{ fontSize: 14, color: "error.main" }} />
              ) : detail.synced ? (
                <CheckCircleIcon sx={{ fontSize: 14, color: "success.main" }} />
              ) : (
                <SyncProblemIcon sx={{ fontSize: 14, color: "warning.main" }} />
              )}
              {hasUnsyncedVersions && (
                <Tooltip title="Some versions are out of sync">
                  <WarningAmberIcon sx={{ fontSize: 14, color: "warning.main" }} />
                </Tooltip>
              )}
              <Typography variant="caption" color="text.secondary">
                {detail.syncStatus || (detail.synced ? "Synced" : "Not synced")}
              </Typography>
            </Box>
          </Box>

          {detail.scanCounts && (
            <Box display="flex" flexWrap="wrap" gap={0.5} mt={1.5}>
              <SeverityBadge counts={detail.scanCounts} />
            </Box>
          )}
        </Box>

        {/* Source repo link */}
        {(detail.repoUrl || detail.sourceRepository) && (
          <Box flexShrink={0}>
            <Tooltip title="Open source repository" placement="top">
              <IconButton
                component="a"
                href={detail.repoUrl || detail.sourceRepository}
                target="_blank"
                rel="noopener noreferrer"
                aria-label="Open source repository"
                sx={{
                  border: "1px solid rgba(240,246,252,0.2)",
                  borderRadius: 1.25,
                }}
              >
                <GitHubIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          </Box>
        )}
      </Box>

      {/* Overview */}
      <SectionCard icon={<InventoryIcon fontSize="small" />} title="Overview">
        <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" }, gap: 1 }}>
          <LabelValue label="Namespace" value={detail.namespace} />
          <LabelValue label="Kind" value={capitalizeKind} />
          <LabelValue label="Provider" value={detail.provider} />
          <LabelValue label="Latest Version" value={detail.latestVersion ? displayVersion(detail.latestVersion) : undefined} />
          {detail.repoOwner && <LabelValue label="Repo Owner" value={detail.repoOwner} />}
          {detail.versionHistoryLimit !== undefined && detail.versionHistoryLimit > 0 && (
            <LabelValue label="Version History Limit" value={String(detail.versionHistoryLimit)} />
          )}
          {detail.versionConstraints && (
            <LabelValue label="Version Constraints" value={detail.versionConstraints} />
          )}
        </Box>
      </SectionCard>

      {/* Storage Configuration */}
      <SectionCard icon={<StorageIcon fontSize="small" />} title="Storage Configuration">
        <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" }, gap: 1 }}>
          <LabelValue
            label="Backend"
            value={detail.storageConfig?.backend || managingDepot?.storageBackend || "Not configured"}
          />
          {detail.storageConfig?.bucket && <LabelValue label="Bucket" value={detail.storageConfig.bucket} />}
          {detail.storageConfig?.region && <LabelValue label="Region" value={detail.storageConfig.region} />}
          {detail.storageConfig?.key && <LabelValue label="Key" value={detail.storageConfig.key} />}
          {detail.storageConfig?.directoryPath && <LabelValue label="Directory Path" value={detail.storageConfig.directoryPath} />}
          {detail.storageConfig?.accountName && <LabelValue label="Account Name" value={detail.storageConfig.accountName} />}
          {detail.storageConfig?.accountUrl && <LabelValue label="Account URL" value={detail.storageConfig.accountUrl} />}
          {detail.storageConfig?.subscriptionID && <LabelValue label="Subscription ID" value={detail.storageConfig.subscriptionID} />}
          {detail.storageConfig?.resourceGroup && <LabelValue label="Resource Group" value={detail.storageConfig.resourceGroup} />}
          <LabelValue label="Presign Enabled" value={detail.storageConfig?.presignEnabled ? "Yes" : "No"} />
          {detail.storageConfig?.presignTTL && <LabelValue label="Presign TTL" value={detail.storageConfig.presignTTL} />}
          {!hasStorageConfigValues && managingDepot?.storageBackend && (
            <LabelValue label="Inherited From" value={`${managingDepot.namespace} / ${managingDepot.name}`} />
          )}
        </Box>
      </SectionCard>

      {/* GitHub Configuration */}
      <SectionCard icon={<GitHubIcon fontSize="small" />} title="GitHub Configuration">
        <LabelValue
          label="Authenticated Client"
          value={detail.githubConfig?.useAuthenticatedClient ? "Yes" : "No"}
        />
      </SectionCard>

      {/* Depot Association */}
      {detail.depotRef && (
        <SectionCard icon={<WarehouseIcon fontSize="small" />} title="Depot">
          <Box display="flex" gap={1} alignItems="center">
            <Chip
              label={`${detail.depotRef.namespace} / ${detail.depotRef.name}`}
              variant="outlined"
              size="small"
              sx={{ fontFamily: "monospace", borderColor: "rgba(3,222,184,0.4)", color: "#03deb8" }}
            />
          </Box>
        </SectionCard>
      )}

      <Divider sx={{ my: 3 }} />

      {/* Versions */}
      {sortedVersions.length > 0 && (
        <Box mb={4}>
          <Typography variant="h6" sx={{ mb: 2, fontWeight: 600 }}>
            Versions{" "}
            <Chip label={sortedVersions.length} size="small" sx={{ ml: 1, fontSize: "0.72rem" }} />
          </Typography>
          <Box sx={{ overflowX: "auto", borderRadius: 2, border: "1px solid rgba(240,246,252,0.08)" }}>
            <Table size="small" sx={{ minWidth: 860 }}>
              <TableHead>
                <TableRow>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>Version</TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>Sync Status</TableCell>
                  {detail.kind === "provider" && <TableCell sx={{ whiteSpace: "nowrap" }}>OS</TableCell>}
                  {detail.kind === "provider" && <TableCell sx={{ whiteSpace: "nowrap" }}>Arch</TableCell>}
                  <TableCell sx={{ whiteSpace: "nowrap" }}>File Name</TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>Checksum</TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>Last Scanned</TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>Findings</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {sortedVersions.map((v, idx) => (
                  <TableRow key={`${v.version}-${v.os || ""}-${v.arch || ""}-${idx}`} hover>
                    <TableCell sx={{ fontFamily: "monospace", fontSize: "0.8125rem", verticalAlign: "top" }}>
                      {displayVersion(v.version)}
                    </TableCell>
                    <TableCell sx={{ verticalAlign: "top" }}>
                      <Box display="flex" alignItems="center" gap={0.5}>
                        {/failed|error/i.test(v.syncStatus) ? (
                          <ErrorIcon sx={{ fontSize: 12, color: "error.main" }} />
                        ) : v.synced ? (
                          <CheckCircleIcon sx={{ fontSize: 12, color: "success.main" }} />
                        ) : (
                          <SyncProblemIcon sx={{ fontSize: 12, color: "warning.main" }} />
                        )}
                        <Typography variant="caption">{v.syncStatus}</Typography>
                      </Box>
                    </TableCell>
                    {detail.kind === "provider" && (
                      <TableCell sx={{ fontFamily: "monospace", fontSize: "0.8125rem", verticalAlign: "top" }}>{v.os || "—"}</TableCell>
                    )}
                    {detail.kind === "provider" && (
                      <TableCell sx={{ fontFamily: "monospace", fontSize: "0.8125rem", verticalAlign: "top" }}>{v.arch || "—"}</TableCell>
                    )}
                    <TableCell sx={{ fontFamily: "monospace", fontSize: "0.8125rem", maxWidth: 240, whiteSpace: "normal", wordBreak: "break-word", verticalAlign: "top" }}>
                      {v.fileName || "—"}
                    </TableCell>
                    <TableCell sx={{ fontFamily: "monospace", fontSize: "0.75rem", maxWidth: 220, verticalAlign: "top" }}>
                      {v.checksum ? (
                        <Box sx={{ display: "flex", alignItems: "flex-start", gap: 0.5 }}>
                          <Typography
                            variant="caption"
                            sx={{ fontFamily: "monospace", whiteSpace: "normal", wordBreak: "break-all", maxWidth: { xs: 120, md: 180 }, display: "block" }}
                          >
                            {v.checksum}
                          </Typography>
                          <CopyButton value={v.checksum} />
                        </Box>
                      ) : "—"}
                    </TableCell>
                    <TableCell sx={{ fontSize: "0.8125rem", verticalAlign: "top", whiteSpace: "nowrap" }}>{v.lastScanned || "—"}</TableCell>
                    <TableCell sx={{ verticalAlign: "top" }}>
                      <Box display="flex" gap={0.5} flexWrap="wrap" sx={{ maxWidth: 160 }}>
                        <SeverityBadge counts={v.scanCounts} />
                      </Box>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Box>
        </Box>
      )}

      {/* Scan findings */}
      <Divider sx={{ my: 3 }} />
      <Typography variant="h6" sx={{ mb: 2, fontWeight: 600 }}>
        Scan Findings
      </Typography>
      <ScanDrillDown
        sourceScanFindings={detail.sourceScanFindings ?? []}
        binaryScanFindings={detail.binaryScanFindings ?? {}}
        versions={sortedVersions}
      />
    </Container>
  );
}
