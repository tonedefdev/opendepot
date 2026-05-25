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
import ErrorIcon from "@mui/icons-material/Error";
import Link from "next/link";
import SeverityBadge from "./SeverityBadge";
import type { BrowseResource } from "@/lib/api";

interface Props {
  resource: BrowseResource;
}

export default function ResourceCard({ resource }: Props) {
  const href = `/${resource.namespace}/${resource.kind}/${resource.name}`;

  return (
    <Card data-testid="resource-card" sx={{ height: "100%", display: "flex", flexDirection: "column" }}>
      <CardActionArea component={Link} href={href} sx={{ flexGrow: 1 }}>
        <CardContent>
          <Box display="flex" alignItems="center" gap={1} mb={0.5}>
            {resource.public ? (
              <PublicIcon sx={{ fontSize: 16, color: "info.main" }} />
            ) : (
              <LockIcon sx={{ fontSize: 16, color: "text.secondary" }} />
            )}
            <Typography variant="caption" color="text.secondary">
              {resource.namespace}
            </Typography>
          </Box>

          <Typography variant="h6" component="div" gutterBottom noWrap>
            {resource.name}
          </Typography>

          <Box display="flex" alignItems="center" gap={1} mb={1}>
            <Chip
              size="small"
              label={resource.kind}
              variant="outlined"
              color="primary"
            />
            {resource.provider && (
              <Chip size="small" label={resource.provider} variant="outlined" />
            )}
            {resource.latestVersion && (
              <Chip size="small" label={`v${resource.latestVersion}`} />
            )}
          </Box>

          <Box display="flex" alignItems="center" gap={0.5} mb={1}>
            {resource.synced ? (
              <CheckCircleIcon sx={{ fontSize: 14, color: "success.main" }} />
            ) : (
              <ErrorIcon sx={{ fontSize: 14, color: "warning.main" }} />
            )}
            <Typography variant="caption" color="text.secondary">
              {resource.syncStatus || (resource.synced ? "Synced" : "Not synced")}
            </Typography>
          </Box>

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
