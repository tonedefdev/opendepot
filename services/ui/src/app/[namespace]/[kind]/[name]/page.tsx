import * as React from "react";
import Container from "@mui/material/Container";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Chip from "@mui/material/Chip";
import Divider from "@mui/material/Divider";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";
import IconButton from "@mui/material/IconButton";
import Breadcrumbs from "@mui/material/Breadcrumbs";
import Tooltip from "@mui/material/Tooltip";
import StorageIcon from "@mui/icons-material/Storage";
import WarehouseIcon from "@mui/icons-material/Warehouse";
import GitHubIcon from "@mui/icons-material/GitHub";
import InventoryIcon from "@mui/icons-material/Inventory";
import DescriptionIcon from "@mui/icons-material/Description";
import Link from "next/link";
import BuildIcon from "@mui/icons-material/Build";
import CodeIcon from "@mui/icons-material/Code";
import AccountTreeIcon from "@mui/icons-material/AccountTree";
import SecurityIcon from "@mui/icons-material/Security";
import SeverityBadge from "@/components/SeverityBadge";
import ScanDrillDown from "@/components/ScanDrillDown";
import ProviderLogo from "@/components/ProviderLogo";
import MobileSidebarButton from "@/components/MobileSidebarButton";
import DrillDownWarningBridge from "@/components/DrillDownWarningBridge";
import UsageSnippet from "@/components/UsageSnippet";
import ResourceReadme from "@/components/ResourceReadme";
import ContractTable from "@/components/ContractTable";
import JevPanel from "@/components/JevPanel";
import { AgentConfigSnippet, AgentDefinition, AgentMetadataChips } from "@/components/AgentDetail";
import { SectionCard } from "@/components/SectionCard";
import SourceRefChip from "@/components/SourceRefChip";
import RiskMeter from "@/components/RiskMeter";
import { KindLabel, SyncStatus, displayVersion } from "@/components/resourceDisplay";
import { DepotKindIcon } from "@/components/DepotKindIcon";
import { monoMetaChipSx } from "@/components/chipStyles";
import { canEditJevExemptions } from "@/components/security-policies/jevExemptions";
import { describeJevUnblock } from "@/components/jevUnblock";
import { getContract, getResourceDetail, getScanPolicyCapabilities, getScanPolicyCatalog, listDepots } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { notFound } from "next/navigation";

const KIND_PLURALS: Record<string, string> = {
  agent: "Agents",
  skill: "Skills",
  module: "Modules",
  provider: "Providers",
};

interface PageProps {
  params: Promise<{
    namespace: string;
    kind: string;
    name: string;
  }>;
}

function LabelValue({ label, value }: { label: string; value?: React.ReactNode }) {
  if (!value && value !== 0) return null;
  return (
    <Box display="flex" gap={1} mb={0.75} alignItems="baseline">
      <Typography variant="body2" sx={{ color: "text.secondary", minWidth: 170, flexShrink: 0, fontSize: "0.8125rem" }}>
        {label}
      </Typography>
      <Typography variant="body2" sx={{ fontFamily: "var(--od-font-mono)", fontSize: "0.8125rem", wordBreak: "break-all" }}>
        {value}
      </Typography>
    </Box>
  );
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

// canCreateJevExemption asks the server whether the caller may write jevExemptions in the namespace. Any failure means no access.
async function canCreateJevExemption(namespace: string, token?: string): Promise<boolean> {
  try {
    const [catalog, capabilities] = await Promise.all([getScanPolicyCatalog(token), getScanPolicyCapabilities(namespace, token)]);
    return canEditJevExemptions(catalog, namespace, capabilities);
  } catch {
    return false;
  }
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
    (v) => !v.blocked && (!v.synced || /failed|error/i.test(v.syncStatus ?? "")),
  );
  const isProviderKind = kind === "provider";
  const isAgentKind = kind === "skill" || kind === "agent";
  const jevCanEdit = detail.jevAssessment?.blocked && detail.jevAssessment.contentDigest ? await canCreateJevExemption(namespace, token) : false;
  const jevResource = { kind: kind === "skill" ? "Skill" : "Agent", name } as const;
  const jevUnblock = detail.jevAssessment && isAgentKind ? describeJevUnblock({ unblock: detail.jevUnblock, namespace }) : null;
  const jevBlocked = isAgentKind && (!!detail.blocked || !!detail.jevAssessment?.blocked);
  const jevBlockReasons = detail.blockReasons?.length ? detail.blockReasons : (detail.jevAssessment?.blockReasons ?? []);
  // The list response carries the block on the resource; the detail response carries it on the assessment.
  const headerResource = { ...detail, blocked: jevBlocked, blockReasons: jevBlockReasons };

  const contract = isProviderKind || isAgentKind ? null : await getContract(namespace, kind, name, undefined, token).catch(() => null);

  const rawBase = process.env.NEXT_PUBLIC_BASE_URL ?? "";
  const registryHost = rawBase ? new URL(rawBase).host : "your-opendepot-host";
  const registryBaseUrl = rawBase || "https://your-opendepot-host";

  return (
    <Container maxWidth="xl" sx={{ py: 4, px: { xs: 2, md: 4 } }}>
      {/* The breadcrumb stops at the kind because the page title directly below it is the resource name. */}
      <Box display="flex" alignItems="center" gap={1} sx={{ mb: 2, minWidth: 0 }}>
        <Breadcrumbs sx={{ flex: 1, minWidth: 0, fontSize: "0.875rem", "& a": { textDecoration: "none", color: "text.secondary", "&:hover": { color: "primary.main" } } }}>
          <Link href="/">Registry</Link>
          <Link href={`/?namespace=${namespace}`}>{namespace}</Link>
          <Link href={`/?kind=${kind}`}>{KIND_PLURALS[kind] ?? capitalizeKind}</Link>
        </Breadcrumbs>
        <MobileSidebarButton />
      </Box>

      <Box
        component="header"
        sx={{ display: "flex", alignItems: "flex-start", gap: 2, mb: 3, pb: 3, borderBottom: "1px solid", borderColor: "divider" }}
      >
        {isProviderKind
          ? <ProviderLogo provider={detail.name} size={38} />
          : detail.provider && <ProviderLogo provider={detail.provider} size={38} />}

        <Box flex={1} minWidth={0}>
          <Typography variant="h1" sx={{ mb: 1.5, wordBreak: "break-word" }}>
            {detail.name}
          </Typography>

          <Box display="flex" flexWrap="wrap" columnGap={2.5} rowGap={1} alignItems="center">
            <KindLabel resource={headerResource} showLogo={false} />
            {detail.latestVersion && (
              <Typography variant="body2" sx={{ fontFamily: "var(--od-font-mono)", fontSize: "0.8125rem" }}>
                {displayVersion(detail.latestVersion)}
              </Typography>
            )}
            <SourceRefChip
              sourceRef={detail.sourceRef}
              sourceCommit={detail.sourceCommit}
              sourcePath={detail.sourcePath}
              repoUrl={detail.repoUrl}
              plain
            />
            <SyncStatus resource={headerResource} />
            {isAgentKind && detail.jevAssessment?.riskLevel && <RiskMeter level={detail.jevAssessment.riskLevel} />}
            {detail.scanCounts && (
              <Box display="flex" flexWrap="wrap" gap={0.5}>
                <SeverityBadge counts={detail.scanCounts} />
              </Box>
            )}
          </Box>
        </Box>

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
                  border: "1px solid",
                  borderColor: "divider",
                  borderRadius: "6px",
                }}
              >
                <GitHubIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          </Box>
        )}
      </Box>

      {/* A block is the first thing to know about a resource, so it is stated here and not left to the assessment further down. */}
      {jevBlocked && (
        <Alert
          severity="error"
          sx={{ mb: 3 }}
          action={
            <Button color="inherit" size="small" href="#jev-assessment" sx={{ whiteSpace: "nowrap" }}>
              View assessment
            </Button>
          }
        >
          Blocked by Jev. The registry does not serve this version.
          {jevBlockReasons.length > 0 && ` Reason: ${jevBlockReasons.join("; ")}.`}
        </Alert>
      )}

      {/* What a person needs first comes first: how to install it, then whether Jev cleared it, then the definition itself. */}
      {isAgentKind && (
        <SectionCard icon={<CodeIcon fontSize="small" />} title="opendepot.hcl">
          <AgentConfigSnippet
            kind={kind}
            namespace={detail.namespace}
            name={detail.name}
            registryHost={registryHost}
            latestVersion={detail.latestVersion}
          />
        </SectionCard>
      )}

      {isAgentKind && detail.jevAssessment && (
        <SectionCard id="jev-assessment" icon={<SecurityIcon fontSize="small" />} title="Jev Assessment">
          <JevPanel
            assessment={detail.jevAssessment}
            thresholds={detail.jevThresholds}
            exemption={{ namespace, versionName: detail.latestVersion, targetKind: jevResource.kind, targetName: name, canEdit: jevCanEdit, governingPolicyHidden: detail.jevUnblock?.namesHidden === true }}
            unblock={jevUnblock}
            namesHidden={detail.jevUnblock?.namesHidden === true}
          />
        </SectionCard>
      )}

      {isAgentKind && detail.definitionContent && (
        <AgentDefinition
          content={detail.definitionContent}
          fileName={detail.definitionFileName ?? ""}
          title={kind === "skill" ? "SKILL.md" : "AGENT.md"}
        />
      )}

      {isAgentKind && (detail.agentMetadata?.model || detail.agentMetadata?.tools?.length) && (
        <SectionCard icon={<BuildIcon fontSize="small" />} title="Tools and Model">
          <AgentMetadataChips metadata={detail.agentMetadata} />
        </SectionCard>
      )}

      {/* Usage */}
      {!isAgentKind && (
      <SectionCard icon={<CodeIcon fontSize="small" />} title="Usage">
        <UsageSnippet
          kind={isProviderKind ? "provider" : "module"}
          namespace={detail.namespace}
          name={detail.name}
          provider={detail.provider}
          latestVersion={detail.latestVersion}
          versionConstraints={detail.versionConstraints}
          registryHost={registryHost}
          registryBaseUrl={registryBaseUrl}
          providerNamespace={detail.providerNamespace}
          upstreamRegistry={detail.upstreamRegistry}
        />
      </SectionCard>
      )}

      {/* README */}
      {!isAgentKind && detail.readmeContent && (
        <SectionCard icon={<DescriptionIcon fontSize="small" />} title="README">
          <ResourceReadme
            content={detail.readmeContent}
            kind={isProviderKind ? "provider" : "module"}
            namespace={detail.namespace}
            name={detail.name}
            provider={detail.provider}
            registryHost={registryHost}
            providerNamespace={detail.providerNamespace}
            upstreamRegistry={detail.upstreamRegistry}
          />
        </SectionCard>
      )}

      {/* Assembly Line contract */}
      {contract && (
        <SectionCard icon={<AccountTreeIcon fontSize="small" />} title="Contract">
          <ContractTable contract={contract} />
        </SectionCard>
      )}

      {/* Overview */}
      <SectionCard icon={<InventoryIcon fontSize="small" />} title="Overview">
        <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" }, gap: 1 }}>
          <LabelValue label="Provider" value={detail.provider} />
          {isProviderKind && (
            <LabelValue label="Provider Namespace" value={detail.providerNamespace || "hashicorp"} />
          )}
          {isProviderKind && (
            <LabelValue label="Upstream Registry" value={detail.upstreamRegistry || "registry.opentofu.org"} />
          )}
          {detail.repoOwner && <LabelValue label="Repo Owner" value={detail.repoOwner} />}
          {isAgentKind && detail.sourcePath && <LabelValue label="Source Path" value={detail.sourcePath} />}
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
              size="small"
              icon={<DepotKindIcon kind="depot" />}
              sx={monoMetaChipSx}
            />
          </Box>
        </SectionCard>
      )}

      <Divider sx={{ my: 3 }} />

      {/* Versions */}
      <DrillDownWarningBridge
        initialHasUnsynced={hasUnsyncedVersions}
        namespace={namespace}
        kind={kind}
        name={name}
      />

      {/* Scan findings */}
      <Divider sx={{ my: 3 }} />
      <ScanDrillDown
        namespace={namespace}
        kind={kind}
        name={name}
        initialSourceScanFindings={detail.sourceScanFindings ?? []}
        initialBinaryScanFindings={detail.binaryScanFindings ?? {}}
        versions={sortedVersions}
      />
    </Container>
  );
}
