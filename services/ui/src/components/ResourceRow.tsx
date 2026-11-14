"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import TableCell from "@mui/material/TableCell";
import TableRow from "@mui/material/TableRow";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import LockIcon from "@mui/icons-material/Lock";
import PublicIcon from "@mui/icons-material/Public";
import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import SyncProblemIcon from "@mui/icons-material/SyncProblem";
import ErrorIcon from "@mui/icons-material/Error";
import WarningAmberIcon from "@mui/icons-material/WarningAmber";
import SecurityIcon from "@mui/icons-material/Security";
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

export default function ResourceRow({ resource }: Props) {
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
  const nameRef = React.useRef<HTMLAnchorElement>(null);
  const [nameTruncated, setNameTruncated] = React.useState(false);

  React.useEffect(() => {
    const el = nameRef.current;
    if (el) setNameTruncated(el.scrollWidth > el.clientWidth);
  }, [resource.name]);

  return (
    <TableRow
      data-testid="resource-row"
      hover
      sx={{ position: "relative", "&:last-child td": { borderBottom: 0 } }}
    >
      <TableCell sx={{ maxWidth: 0, width: "34%" }}>
        <Box display="flex" alignItems="center" gap={0.75} minWidth={0}>
          {resource.public ? (
            <PublicIcon
              sx={{ fontSize: 14, color: "info.main", flexShrink: 0 }}
            />
          ) : (
            <LockIcon
              sx={{ fontSize: 14, color: "text.secondary", flexShrink: 0 }}
            />
          )}
          <Typography
            variant="caption"
            noWrap
            sx={{
              color: "text.secondary",
              fontFamily: "monospace",
              fontSize: "0.75rem",
            }}
          >
            {resource.namespace}
          </Typography>
        </Box>
        <Tooltip
          title={nameTruncated ? resource.name : ""}
          placement="top"
          enterDelay={300}
          disableInteractive
        >
          <Typography
            ref={nameRef}
            component={Link}
            href={href}
            noWrap
            sx={{
              display: "block",
              color: "text.primary",
              textDecoration: "none",
              fontWeight: 600,
              fontSize: "0.9375rem",
              lineHeight: 1.5,
              "&::after": { content: '""', position: "absolute", inset: 0 },
              "&:focus-visible": { outline: "none" },
              "&:focus-visible::after": {
                outline: "2px solid",
                outlineColor: "primary.main",
                outlineOffset: -2,
              },
            }}
          >
            {resource.name}
          </Typography>
        </Tooltip>
      </TableCell>

      <TableCell sx={{ width: "18%" }}>
        <Box display="flex" alignItems="center" gap={0.75} flexWrap="wrap">
          {resource.kind === "provider" ? (
            <ProviderLogo provider={resource.name} size={18} />
          ) : resource.provider ? (
            <ProviderLogo provider={resource.provider} size={18} />
          ) : null}
          <Chip
            size="small"
            label={resource.kind}
            icon={
              isDepotKind(resource.kind) ? (
                <DepotKindIcon kind={resource.kind} />
              ) : undefined
            }
            sx={kindChipSx(resource.kind)}
          />
        </Box>
      </TableCell>

      <TableCell sx={{ width: "14%", display: { xs: "none", md: "table-cell" } }}>
        {resource.latestVersion ? (
          <Chip
            size="small"
            label={displayVersion(resource.latestVersion)}
            icon={<DepotKindIcon kind="version" />}
            sx={monoMetaChipSx}
          />
        ) : (
          <Typography variant="body2" color="text.secondary">
            —
          </Typography>
        )}
      </TableCell>

      <TableCell sx={{ width: "22%" }}>
        <Box display="flex" alignItems="center" gap={0.5} minWidth={0}>
          {isBlocked || syncFailed ? (
            <ErrorIcon
              sx={{ fontSize: 14, color: "error.main", flexShrink: 0 }}
            />
          ) : resource.synced ? (
            <CheckCircleIcon
              sx={{ fontSize: 14, color: "success.main", flexShrink: 0 }}
            />
          ) : (
            <SyncProblemIcon
              sx={{ fontSize: 14, color: "warning.main", flexShrink: 0 }}
            />
          )}
          {resource.hasUnsyncedVersions && (
            <Tooltip title="Some versions are out of sync">
              <WarningAmberIcon
                sx={{ fontSize: 14, color: "warning.main", flexShrink: 0 }}
              />
            </Tooltip>
          )}
          <Tooltip
            title={syncTooltip}
            placement="top"
            enterDelay={300}
            disableInteractive
          >
            <Typography
              variant="body2"
              noWrap
              color={isBlocked ? "error.main" : "text.secondary"}
              data-testid="resource-card-sync-status"
            >
              {syncLabel}
            </Typography>
          </Tooltip>
        </Box>
        {resource.sourceRef && (
          <Box mt={0.5}>
            <SourceRefChip
              sourceRef={resource.sourceRef}
              sourceCommit={resource.sourceCommit}
              sourcePath={resource.sourcePath}
              repoUrl={resource.repoUrl}
              linked={false}
            />
          </Box>
        )}
      </TableCell>

      <TableCell sx={{ width: "12%" }}>
        <Box
          display="flex"
          flexDirection="column"
          alignItems="flex-start"
          gap={0.5}
        >
          {isAgentKind && resource.jevRiskLevel && (
            <Chip
              size="small"
              label={`Risk: ${resource.jevRiskLevel}`}
              icon={<SecurityIcon />}
              sx={{ ...riskChipSx(resource.jevRiskLevel), flexShrink: 0 }}
            />
          )}
          {resource.scanCounts && (
            <Box display="flex" flexWrap="wrap" gap={0.5}>
              <SeverityBadge counts={resource.scanCounts} />
            </Box>
          )}
        </Box>
      </TableCell>
    </TableRow>
  );
}
