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
import type { BrowseResource } from "@/lib/api";

interface Props {
  resource: BrowseResource;
}

function displayVersion(v: string): string {
  if (!v) return "";
  return v.startsWith("v") ? v : `v${v}`;
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
        ...(resource.kind === "provider"
          ? {
              "&:hover": {
                borderColor: "rgba(4,125,241,0.5)",
                boxShadow: "0 0 0 1px rgba(4,125,241,0.15), 0 4px 16px rgba(0,0,0,0.4)",
              },
            }
          : {}),
      }}
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
            {resource.provider && (
              <ProviderLogo provider={resource.provider} size={22} />
            )}
            <Chip
              size="small"
              label={resource.kind}
              variant="outlined"
              color={resource.kind === "provider" ? "secondary" : "primary"}
              sx={{ textTransform: "capitalize" }}
            />
            {resource.latestVersion && (
              <Chip
                size="small"
                label={displayVersion(resource.latestVersion)}
                sx={{ fontFamily: "monospace", fontSize: "0.72rem" }}
              />
            )}
          </Box>

          {/* Sync status */}
          <Box display="flex" alignItems="center" gap={0.5} mb={1}>
            {/failed|error/i.test(resource.syncStatus) ? (
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
            <Typography variant="caption" color="text.secondary">
              {resource.syncStatus || (resource.synced ? "Synced" : "Not synced")}
            </Typography>
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
