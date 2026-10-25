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
import Tooltip from "@mui/material/Tooltip";
import Link from "next/link";
import SeverityBadge from "./SeverityBadge";
import ProviderLogo from "./ProviderLogo";
import SourceRefChip from "./SourceRefChip";
import { DepotKindIcon, isDepotKind } from "./DepotKindIcon";
import type { BrowseResource } from "@/lib/api";
import { KIND_CHIP_TEXT, KIND_CHIP_COLORS, kindChipColor, kindColor } from "@/lib/kindColors";
import { RISK_COLORS } from "@/lib/jevSeverity";

// Resource-kind hues are fixed brand colors from kindColors, so they must NOT
// swap with the primary/secondary palette between light and dark mode.

interface Props {
  resource: BrowseResource;
}

function displayVersion(v: string): string {
  if (!v) return "";
  return v.startsWith("v") ? v : `v${v}`;
}

export default function ResourceCard({ resource }: Props) {
  const href = `/${resource.namespace}/${resource.kind}/${resource.name}`;
  const accent = kindColor(resource.kind);
  const isAgentKind = resource.kind === "skill" || resource.kind === "agent";
  const isBlocked = isAgentKind && !!resource.blocked;
  const nameRef = React.useRef<HTMLDivElement>(null);
  const [nameTruncated, setNameTruncated] = React.useState(false);

  React.useEffect(() => {
    const el = nameRef.current;
    if (el) setNameTruncated(el.scrollWidth > el.clientWidth);
  }, [resource.name]);

  return (
    <Card
      data-testid="resource-card"
      sx={(theme) => ({
        height: "100%",
        display: "flex",
        flexDirection: "column",
        "&:hover": {
          borderColor: `color-mix(in srgb, ${accent} 50%, transparent)`,
          boxShadow: `0 0 0 1px color-mix(in srgb, ${accent} 15%, transparent), 0 4px 16px rgba(0,0,0,0.4)`,
          ...theme.applyStyles("light", {
            boxShadow: `0 0 0 1px color-mix(in srgb, ${accent} 15%, transparent), 0 4px 16px rgba(0,0,0,0.08)`,
          }),
        },
      })}
    >
      <CardActionArea component={Link} href={href} sx={{ flexGrow: 1, alignItems: "flex-start" }}>
        <CardContent sx={{ pb: "12px !important" }}>
          {/* Header row: namespace + visibility */}
          <Box display="flex" alignItems="center" gap={0.75} mb={0.75}>
            {resource.public ? (
              <PublicIcon sx={{ fontSize: 13, color: "info.main" }} />
            ) : (
              <LockIcon sx={{ fontSize: 13, color: "text.secondary" }} />
            )}
            <Typography
              variant="caption"
              sx={{ color: "text.secondary", fontFamily: "monospace", fontSize: "0.72rem" }}
            >
              {resource.namespace}
            </Typography>
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

          {/* Provider logo + kind + version */}
          <Box display="flex" alignItems="center" gap={1} mb={1} flexWrap="wrap">
            {resource.kind === "provider" ? (
              <ProviderLogo provider={resource.name} size={22} />
            ) : resource.provider ? (
              <ProviderLogo provider={resource.provider} size={22} />
            ) : null}
            <Chip
              size="small"
              label={resource.kind}
              icon={isDepotKind(resource.kind) ? <DepotKindIcon kind={resource.kind} sx={{ fontSize: 14 }} /> : undefined}
              sx={{
                textTransform: "capitalize",
                color: KIND_CHIP_TEXT,
                bgcolor: kindChipColor(resource.kind),
                "& .MuiChip-icon": { color: "inherit !important" },
              }}
            />
            {resource.latestVersion && (
              <Chip
                size="small"
                label={displayVersion(resource.latestVersion)}
                icon={<DepotKindIcon kind="version" sx={{ fontSize: 14 }} />}
                sx={{
                  fontFamily: "monospace",
                  fontSize: "0.72rem",
                  color: KIND_CHIP_TEXT,
                  bgcolor: KIND_CHIP_COLORS.version,
                  "& .MuiChip-icon": { color: "inherit !important" },
                }}
              />
            )}
            {isAgentKind && resource.jevRiskLevel && (
              <Chip
                size="small"
                variant="outlined"
                color={RISK_COLORS[resource.jevRiskLevel] ?? "default"}
                label={`Risk: ${resource.jevRiskLevel}`}
                sx={{ fontSize: "0.72rem" }}
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

          {/* Sync status */}
          <Box display="flex" alignItems="center" gap={0.5} mb={1}>
            {isBlocked || /failed|error/i.test(resource.syncStatus) ? (
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
            {isBlocked ? (
              <Tooltip title={(resource.blockReasons ?? []).join("; ") || "Blocked by Jev"}>
                <Typography variant="caption" color="error.main">
                  Blocked by Jev
                </Typography>
              </Tooltip>
            ) : (
              <Typography variant="caption" color="text.secondary">
                {resource.syncStatus || (resource.synced ? "Synced" : "Not synced")}
              </Typography>
            )}
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
