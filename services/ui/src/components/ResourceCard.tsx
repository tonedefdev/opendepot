"use client";

import * as React from "react";
import Card from "@mui/material/Card";
import CardContent from "@mui/material/CardContent";
import CardActionArea from "@mui/material/CardActionArea";
import Typography from "@mui/material/Typography";
import Box from "@mui/material/Box";
import PublicIcon from "@mui/icons-material/Public";
import Tooltip from "@mui/material/Tooltip";
import Link from "next/link";
import SeverityBadge from "./SeverityBadge";
import SourceRefChip from "./SourceRefChip";
import RiskMeter from "./RiskMeter";
import { KindLabel, SyncStatus, displayVersion, isAgentKind } from "./resourceDisplay";
import type { BrowseResource } from "@/lib/api";
import { formatFullDateTime, formatRelativeTime } from "@/lib/relativeTime";

interface Props {
  resource: BrowseResource;
}

export default function ResourceCard({ resource }: Props) {
  const href = `/${resource.namespace}/${resource.kind}/${resource.name}`;
  const nameRef = React.useRef<HTMLDivElement>(null);
  const [nameTruncated, setNameTruncated] = React.useState(false);

  React.useEffect(() => {
    const el = nameRef.current;
    if (el) setNameTruncated(el.scrollWidth > el.clientWidth);
  }, [resource.name]);

  return (
    <Card
      data-testid="resource-card"
      sx={{
        height: "100%",
        display: "flex",
        flexDirection: "column",
        position: "relative",
        "&::before": {
          content: '""',
          position: "absolute",
          top: 0,
          left: 0,
          right: 0,
          height: 4,
          bgcolor: "primary.main",
          zIndex: 1,
          pointerEvents: "none",
        },
        "[data-mui-color-scheme='dark'] &::before": {
          display: "none",
        },
      }}
    >
      <CardActionArea component={Link} href={href} sx={{ flexGrow: 1, alignItems: "flex-start" }}>
        <CardContent sx={{ display: "flex", flexDirection: "column", gap: 1.25, pb: "16px !important" }}>
          <Box display="flex" alignItems="center" justifyContent="space-between" gap={1} minHeight={20}>
            <KindLabel resource={resource} />
            {isAgentKind(resource.kind) && resource.jevRiskLevel && <RiskMeter level={resource.jevRiskLevel} />}
          </Box>

          <Box minWidth={0}>
            <Box display="flex" alignItems="center" gap={0.5} minWidth={0}>
              <Typography variant="caption" noWrap>
                {resource.namespace}/
              </Typography>
              {resource.public && (
                <Tooltip title="Public: readable without signing in">
                  <PublicIcon sx={{ fontSize: 13, color: "info.main", flexShrink: 0 }} />
                </Tooltip>
              )}
            </Box>
            <Tooltip title={nameTruncated ? resource.name : ""} placement="top" enterDelay={300} disableInteractive>
              <Typography ref={nameRef} component="div" noWrap sx={{ fontSize: "0.9375rem", fontWeight: 600, lineHeight: 1.35 }}>
                {resource.name}
              </Typography>
            </Tooltip>
          </Box>

          <Box display="flex" alignItems="center" gap={1.5} minWidth={0} minHeight={20}>
            {resource.latestVersion && (
              <Typography variant="body2" sx={{ fontFamily: "var(--od-font-mono)", fontSize: "0.8125rem", flexShrink: 0 }}>
                {displayVersion(resource.latestVersion)}
              </Typography>
            )}
            <SourceRefChip
              sourceRef={resource.sourceRef}
              sourceCommit={resource.sourceCommit}
              sourcePath={resource.sourcePath}
              repoUrl={resource.repoUrl}
              linked={false}
              plain
            />
          </Box>

          <SyncStatus resource={resource} testId="resource-card-sync-status" />

          <Tooltip title={formatFullDateTime(resource.lastUpdated)} placement="top" enterDelay={300} disableInteractive>
            <Typography variant="caption" color="text.secondary" noWrap sx={{ fontVariantNumeric: "tabular-nums" }}>
              Updated {formatRelativeTime(resource.lastUpdated)}
            </Typography>
          </Tooltip>

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
