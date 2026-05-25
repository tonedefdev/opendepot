import * as React from "react";
import Container from "@mui/material/Container";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Chip from "@mui/material/Chip";
import Divider from "@mui/material/Divider";
import Alert from "@mui/material/Alert";
import Card from "@mui/material/Card";
import CardContent from "@mui/material/CardContent";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import Breadcrumbs from "@mui/material/Breadcrumbs";
import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import SyncProblemIcon from "@mui/icons-material/SyncProblem";
import LinkIcon from "@mui/icons-material/Link";
import StorageIcon from "@mui/icons-material/Storage";
import GitHubIcon from "@mui/icons-material/GitHub";
import InventoryIcon from "@mui/icons-material/Inventory";
import Link from "next/link";
import SeverityBadge from "@/components/SeverityBadge";
import ScanDrillDown from "@/components/ScanDrillDown";
import ProviderLogo from "@/components/ProviderLogo";
import CopyButton from "@/components/CopyButton";
import { getResourceDetail } from "@/lib/api";
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
          border: "1px solid rgba(4,125,241,0.2)",
          background: "linear-gradient(135deg, rgba(4,125,241,0.06) 0%, rgba(3,222,184,0.03) 100%)",
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
                  background: "rgba(4,125,241,0.12)",
                  color: "#047df1",
                  border: "1px solid rgba(4,125,241,0.3)",
                }}
              />
            )}
            <Box display="flex" alignItems="center" gap={0.5}>
              {detail.synced ? (
                <CheckCircleIcon sx={{ fontSize: 14, color: "success.main" }} />
              ) : (
                <SyncProblemIcon sx={{ fontSize: 14, color: "warning.main" }} />
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
            <Chip
              icon={<LinkIcon sx={{ fontSize: 14 }} />}
              label="Repository"
              size="small"
              component="a"
              href={detail.repoUrl || detail.sourceRepository}
              target="_blank"
              rel="noopener noreferrer"
              clickable
              variant="outlined"
              sx={{ borderColor: "rgba(240,246,252,0.2)" }}
            />
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
          {(detail.repoUrl || detail.sourceRepository) && (
            <LabelValue label="Source Repository" value={detail.repoUrl || detail.sourceRepository} />
          )}
        </Box>
      </SectionCard>

      {/* Storage Configuration */}
      {detail.storageConfig && (
        <SectionCard icon={<StorageIcon fontSize="small" />} title="Storage Configuration">
          <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" }, gap: 1 }}>
            <LabelValue label="Backend" value={detail.storageConfig.backend} />
            {detail.storageConfig.bucket && <LabelValue label="Bucket" value={detail.storageConfig.bucket} />}
            {detail.storageConfig.region && <LabelValue label="Region" value={detail.storageConfig.region} />}
            {detail.storageConfig.key && <LabelValue label="Key" value={detail.storageConfig.key} />}
            {detail.storageConfig.directoryPath && <LabelValue label="Directory Path" value={detail.storageConfig.directoryPath} />}
            {detail.storageConfig.accountName && <LabelValue label="Account Name" value={detail.storageConfig.accountName} />}
            {detail.storageConfig.accountUrl && <LabelValue label="Account URL" value={detail.storageConfig.accountUrl} />}
            {detail.storageConfig.subscriptionID && <LabelValue label="Subscription ID" value={detail.storageConfig.subscriptionID} />}
            {detail.storageConfig.resourceGroup && <LabelValue label="Resource Group" value={detail.storageConfig.resourceGroup} />}
            <LabelValue label="Presign Enabled" value={detail.storageConfig.presignEnabled ? "Yes" : "No"} />
            {detail.storageConfig.presignTTL && <LabelValue label="Presign TTL" value={detail.storageConfig.presignTTL} />}
          </Box>
        </SectionCard>
      )}

      {/* GitHub Configuration */}
      {detail.githubConfig && (
        <SectionCard icon={<GitHubIcon fontSize="small" />} title="GitHub Configuration">
          <LabelValue
            label="Authenticated Client"
            value={detail.githubConfig.useAuthenticatedClient ? "Yes" : "No"}
          />
        </SectionCard>
      )}

      {/* Depot Association */}
      {detail.depotRef && (
        <SectionCard icon={<StorageIcon fontSize="small" />} title="Depot">
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
      {detail.versions && detail.versions.length > 0 && (
        <Box mb={4}>
          <Typography variant="h6" sx={{ mb: 2, fontWeight: 600 }}>
            Versions{" "}
            <Chip label={detail.versions.length} size="small" sx={{ ml: 1, fontSize: "0.72rem" }} />
          </Typography>
          <Box sx={{ overflowX: "auto", borderRadius: 2, border: "1px solid rgba(240,246,252,0.08)" }}>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Version</TableCell>
                  <TableCell>Sync Status</TableCell>
                  {detail.kind === "provider" && <TableCell>OS</TableCell>}
                  {detail.kind === "provider" && <TableCell>Arch</TableCell>}
                  <TableCell>File Name</TableCell>
                  <TableCell>Checksum</TableCell>
                  <TableCell>Last Scanned</TableCell>
                  <TableCell>Findings</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {detail.versions.map((v, idx) => (
                  <TableRow key={`${v.version}-${v.os || ""}-${v.arch || ""}-${idx}`} hover>
                    <TableCell sx={{ fontFamily: "monospace", fontSize: "0.8125rem" }}>
                      {displayVersion(v.version)}
                    </TableCell>
                    <TableCell>
                      <Box display="flex" alignItems="center" gap={0.5}>
                        {v.synced ? (
                          <CheckCircleIcon sx={{ fontSize: 12, color: "success.main" }} />
                        ) : (
                          <SyncProblemIcon sx={{ fontSize: 12, color: "warning.main" }} />
                        )}
                        <Typography variant="caption">{v.syncStatus}</Typography>
                      </Box>
                    </TableCell>
                    {detail.kind === "provider" && (
                      <TableCell sx={{ fontFamily: "monospace", fontSize: "0.8125rem" }}>{v.os || "—"}</TableCell>
                    )}
                    {detail.kind === "provider" && (
                      <TableCell sx={{ fontFamily: "monospace", fontSize: "0.8125rem" }}>{v.arch || "—"}</TableCell>
                    )}
                    <TableCell sx={{ fontFamily: "monospace", fontSize: "0.75rem", maxWidth: 200, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                      {v.fileName || "—"}
                    </TableCell>
                    <TableCell sx={{ fontFamily: "monospace", fontSize: "0.7rem", maxWidth: 140 }}>
                      {v.checksum ? (
                        <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
                          <Typography
                            variant="caption"
                            sx={{ fontFamily: "monospace", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", maxWidth: 110, display: "block" }}
                          >
                            {v.checksum}
                          </Typography>
                          <CopyButton value={v.checksum} />
                        </Box>
                      ) : "—"}
                    </TableCell>
                    <TableCell sx={{ fontSize: "0.8125rem" }}>{v.lastScanned || "—"}</TableCell>
                    <TableCell>
                      <Box display="flex" gap={0.5}>
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
      />
    </Container>
  );
}
