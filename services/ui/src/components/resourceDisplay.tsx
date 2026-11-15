import * as React from "react";
import Box from "@mui/material/Box";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import ErrorIcon from "@mui/icons-material/Error";
import WarningAmberIcon from "@mui/icons-material/WarningAmber";
import ProviderLogo from "./ProviderLogo";
import { DepotKindIcon, isDepotKind } from "./DepotKindIcon";
import { kindColor } from "@/lib/kindColors";
import type { BrowseResource } from "@/lib/api";

export function displayVersion(v: string): string {
  if (!v) return "";
  return v.startsWith("v") ? v : `v${v}`;
}

export function isAgentKind(kind: string): boolean {
  return kind === "skill" || kind === "agent";
}

interface ResourceSync {
  tone: "success" | "warning" | "error";
  label: string;
  detail: string;
}

// A healthy sync collapses to "Synced"; failures keep the controller's message because it explains the problem.
export function describeSync(resource: BrowseResource): ResourceSync {
  const blocked = isAgentKind(resource.kind) && !!resource.blocked;
  const failed = /failed|error/i.test(resource.syncStatus);

  if (blocked) {
    return { tone: "error", label: "Blocked by Jev", detail: (resource.blockReasons ?? []).join("; ") || "Blocked by Jev" };
  }

  const label = resource.synced && !failed ? "Synced" : resource.syncStatus || "Not synced";
  const detail = resource.syncStatus && resource.syncStatus !== label ? resource.syncStatus : "";

  return { tone: failed ? "error" : resource.synced ? "success" : "warning", label, detail };
}

// Healthy rows get a small dot and grey text. Only a failure or a block takes an icon and color, so a
// column of statuses shows the exceptions first.
export function SyncStatus({ resource, testId }: { resource: BrowseResource; testId?: string }) {
  const sync = describeSync(resource);

  return (
    <Box display="flex" alignItems="center" gap={0.75} minWidth={0}>
      {sync.tone === "error" ? (
        <ErrorIcon sx={{ fontSize: 14, color: "error.main", flexShrink: 0 }} />
      ) : (
        <Box
          component="span"
          sx={{ width: 6, height: 6, mx: "4px", borderRadius: "50%", bgcolor: `${sync.tone}.main`, flexShrink: 0 }}
        />
      )}
      {resource.hasUnsyncedVersions && (
        <Tooltip title="Some versions are out of sync">
          <WarningAmberIcon sx={{ fontSize: 14, color: "warning.main", flexShrink: 0, position: "relative", zIndex: 1 }} />
        </Tooltip>
      )}
      <Tooltip title={sync.detail} placement="top" enterDelay={300} disableInteractive>
        <Typography
          variant="body2"
          noWrap
          color={sync.tone === "error" ? "error.main" : sync.tone === "warning" ? "text.primary" : "text.secondary"}
          sx={{ position: "relative", zIndex: 1 }}
          data-testid={testId}
        >
          {sync.label}
        </Typography>
      </Tooltip>
    </Box>
  );
}

// Kind is plain text with its icon. Modules and providers also show the cloud they target, unless the
// caller already shows that logo next to the name.
export function KindLabel({ resource, showLogo = true }: { resource: BrowseResource; showLogo?: boolean }) {
  const logo = showLogo ? (resource.kind === "provider" ? resource.name : resource.provider) : "";

  return (
    <Box display="flex" alignItems="center" gap={0.75} minWidth={0} sx={{ color: "text.secondary" }}>
      {isDepotKind(resource.kind) && (
        <DepotKindIcon kind={resource.kind} sx={{ fontSize: 16, color: kindColor(resource.kind), flexShrink: 0 }} />
      )}
      <Typography variant="body2" noWrap sx={{ textTransform: "capitalize" }}>
        {resource.kind}
      </Typography>
      {logo && (
        <Box sx={{ position: "relative", zIndex: 1, display: "flex" }}>
          <ProviderLogo provider={logo} size={18} />
        </Box>
      )}
    </Box>
  );
}
