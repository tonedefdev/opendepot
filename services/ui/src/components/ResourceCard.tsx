"use client";

import * as React from "react";
import Card from "@mui/material/Card";
import CardContent from "@mui/material/CardContent";
import CardActionArea from "@mui/material/CardActionArea";
import Typography from "@mui/material/Typography";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import LockIcon from "@mui/icons-material/Lock";
import PublicIcon from "@mui/icons-material/Public";
import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import SyncProblemIcon from "@mui/icons-material/SyncProblem";
import ErrorIcon from "@mui/icons-material/Error";
import WarningAmberIcon from "@mui/icons-material/WarningAmber";
import SecurityIcon from "@mui/icons-material/Security";
import Tooltip from "@mui/material/Tooltip";
import Link from "next/link";
import SeverityBadge from "./SeverityBadge";
import ProviderLogo from "./ProviderLogo";
import SourceRefChip from "./SourceRefChip";
import { DepotKindIcon, isDepotKind } from "./DepotKindIcon";
import type { BrowseResource } from "@/lib/api";
import { kindChipSx, monoMetaChipSx, riskChipSx } from "./chipStyles";

interface Props {
  resource: BrowseResource;
}

function displayVersion(v: string): string {
  if (!v) return "";
  return v.startsWith("v") ? v : `v${v}`;
}

export default function ResourceCard({ resource }: Props) {
  const href = `/${resource.namespace}/${resource.kind}/${resource.name}`;
  const isAgentKind = resource.kind === "skill" || resource.kind === "agent";
  const isBlocked = isAgentKind && !!resource.blocked;
  const syncFailed = /failed|error/i.test(resource.syncStatus);
  // A healthy sync collapses to "Synced"; failures keep the controller's message because it explains the problem.
  const syncLabel = isBlocked
    ? "Blocked by Jev"
    : resource.synced && !syncFailed
      ? "Synced"
      : resource.syncStatus || "Not synced";
  const syncTooltip = isBlocked
    ? (resource.blockReasons ?? []).join("; ") || "Blocked by Jev"
    : resource.syncStatus && resource.syncStatus !== syncLabel
      ? resource.syncStatus
      : "";
  const nameRef = React.useRef<HTMLDivElement>(null);
  const [nameTruncated, setNameTruncated] = React.useState(false);

  React.useEffect(() => {
    const el = nameRef.current;
    if (el) setNameTruncated(el.scrollWidth > el.clientWidth);
  }, [resource.name]);

  return (
    <Card data-testid="resource-card" sx={{ height: "100%", display: "flex", flexDirection: "column" }}>
      <CardActionArea component={Link} href={href} sx={{ flexGrow: 1, alignItems: "flex-start" }}>
        <CardContent sx={{ pb: "12px !important" }}>
          {/* Header row: namespace + visibility on the left, Jev risk on the right */}
          <Box display="flex" alignItems="center" justifyContent="space-between" gap={1} mb={0.75} minHeight={22}>
            <Box display="flex" alignItems="center" gap={0.75} minWidth={0}>
              {resource.public ? (
                <PublicIcon sx={{ fontSize: 13, color: "info.main" }} />
              ) : (
                <LockIcon sx={{ fontSize: 13, color: "text.secondary" }} />
              )}
              <Typography
                variant="caption"
                noWrap
                sx={{ color: "text.secondary", fontFamily: "monospace", fontSize: "0.72rem" }}
              >
                {resource.namespace}
              </Typography>
            </Box>
            {isAgentKind && resource.jevRiskLevel && (
              <Chip
                size="small"
                label={`Risk: ${resource.jevRiskLevel}`}
                icon={<SecurityIcon />}
                sx={{ ...riskChipSx(resource.jevRiskLevel), flexShrink: 0 }}
              />
            )}
          </Box>

          {/* Name */}
          <Tooltip title={nameTruncated ? resource.name : ""} placement="top" enterDelay={300} disableInteractive>
            <Typography
              ref={nameRef}
              variant="h6"
              component="div"
              gutterBottom
              noWrap
              sx={{ fontSize: "0.9375rem", fontWeight: 700, lineHeight: 1.3, mb: 1 }}
            >
              {resource.name}
            </Typography>
          </Tooltip>

          {/* Metadata: provider logo, kind, version, source ref. Neutral so the name and status lead. */}
          <Box display="flex" alignItems="center" gap={0.75} mb={1.25} flexWrap="wrap">
            {resource.kind === "provider" ? (
              <ProviderLogo provider={resource.name} size={22} />
            ) : resource.provider ? (
              <ProviderLogo provider={resource.provider} size={22} />
            ) : null}
            <Chip
              size="small"
              label={resource.kind}
              icon={isDepotKind(resource.kind) ? <DepotKindIcon kind={resource.kind} /> : undefined}
              sx={kindChipSx(resource.kind)}
            />
            {resource.latestVersion && (
              <Chip
                size="small"
                label={displayVersion(resource.latestVersion)}
                icon={<DepotKindIcon kind="version" />}
                sx={monoMetaChipSx}
              />
            )}
            {resource.sourceRef && (
              <SourceRefChip
                sourceRef={resource.sourceRef}
                sourceCommit={resource.sourceCommit}
                sourcePath={resource.sourcePath}
                repoUrl={resource.repoUrl}
                linked={false}
              />
            )}
          </Box>

          {/* Status: sync state. Color is reserved for this row. */}
          <Box display="flex" alignItems="center" gap={1} mb={1} minHeight={22}>
            <Box display="flex" alignItems="center" gap={0.5} minWidth={0}>
              {isBlocked || syncFailed ? (
                <ErrorIcon sx={{ fontSize: 13, color: "error.main" }} />
              ) : resource.synced ? (
                <CheckCircleIcon sx={{ fontSize: 13, color: "success.main" }} />
              ) : (
                <SyncProblemIcon sx={{ fontSize: 13, color: "warning.main" }} />
              )}
              {resource.hasUnsyncedVersions && (
                <Tooltip title="Some versions are out of sync">
                  <WarningAmberIcon sx={{ fontSize: 13, color: "warning.main" }} />
                </Tooltip>
              )}
              <Tooltip title={syncTooltip} placement="top" enterDelay={300} disableInteractive>
                <Typography
                  variant="caption"
                  noWrap
                  color={isBlocked ? "error.main" : "text.secondary"}
                  data-testid="resource-card-sync-status"
                >
                  {syncLabel}
                </Typography>
              </Tooltip>
            </Box>
          </Box>

          {/* Scan counts */}
          {resource.scanCounts && (
            <Box display="flex" flexWrap="wrap" gap={0.5}>
              <SeverityBadge counts={resource.scanCounts} />
            </Box>
          )}
        </CardContent>
      </CardActionArea>
    </Card>
  );
}
