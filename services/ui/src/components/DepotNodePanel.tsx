"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Chip from "@mui/material/Chip";
import IconButton from "@mui/material/IconButton";
import Button from "@mui/material/Button";
import Divider from "@mui/material/Divider";
import CloseIcon from "@mui/icons-material/Close";
import OpenInNewIcon from "@mui/icons-material/OpenInNew";
import HubIcon from "@mui/icons-material/Hub";
import StorageIcon from "@mui/icons-material/Storage";
import Link from "next/link";

type NodeKind = "depot" | "module" | "provider";

interface DepotNodePanelProps {
  node: {
    kind: NodeKind;
    namespace: string;
    name: string;
    synced?: boolean;
    provider?: string;
    providerNamespace?: string;
    storageBackend?: string;
  };
  onClose: () => void;
}

function FieldRow({ label, value }: { label: string; value?: React.ReactNode }) {
  if (!value && value !== 0) return null;
  return (
    <Box sx={{ display: "flex", flexDirection: "column", mb: 1.25 }}>
      <Typography
        variant="caption"
        sx={{
          color: "text.secondary",
          textTransform: "uppercase",
          letterSpacing: "0.06em",
          fontWeight: 600,
          fontSize: "0.68rem",
        }}
      >
        {label}
      </Typography>
      <Typography variant="body2" sx={{ fontFamily: "monospace", fontSize: "0.8rem", mt: 0.25, wordBreak: "break-all" }}>
        {value}
      </Typography>
    </Box>
  );
}

const PANEL_WIDTH = 280;

export default function DepotNodePanel({ node, onClose }: DepotNodePanelProps) {
  const kindLabel =
    node.kind === "depot" ? "Depot" : node.kind === "module" ? "Module" : "Provider";

  const kindColor =
    node.kind === "depot"
      ? "primary.main"
      : node.kind === "module"
        ? "secondary.main"
        : "secondary.light";

  // Link to the detail page (only for modules and providers)
  const detailHref =
    node.kind !== "depot"
      ? `/${encodeURIComponent(node.namespace)}/${encodeURIComponent(node.kind)}/${encodeURIComponent(node.name)}`
      : null;

  return (
    <Box
      sx={{
        width: PANEL_WIDTH,
        flexShrink: 0,
        bgcolor: "background.paper",
        borderLeft: "1px solid rgba(240,246,252,0.12)",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
      }}
    >
      {/* Header */}
      <Box
        sx={{
          px: 2,
          py: 1.5,
          display: "flex",
          alignItems: "flex-start",
          gap: 1,
          borderBottom: "1px solid rgba(240,246,252,0.08)",
        }}
      >
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, mb: 0.5 }}>
            {node.kind === "depot" ? (
              <HubIcon sx={{ fontSize: 14, color: kindColor, flexShrink: 0 }} />
            ) : (
              <StorageIcon sx={{ fontSize: 14, color: kindColor, flexShrink: 0 }} />
            )}
            <Chip
              label={kindLabel}
              size="small"
              sx={{
                fontSize: "0.65rem",
                height: 18,
                color: kindColor,
                borderColor: kindColor,
                "& .MuiChip-label": { px: 0.75 },
              }}
              variant="outlined"
            />
          </Box>
          <Typography
            variant="subtitle2"
            sx={{
              fontWeight: 700,
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
              fontSize: "0.875rem",
            }}
          >
            {node.name}
          </Typography>
        </Box>
        <IconButton size="small" onClick={onClose} aria-label="Close detail panel" sx={{ flexShrink: 0, mt: -0.5 }}>
          <CloseIcon sx={{ fontSize: 16 }} />
        </IconButton>
      </Box>

      {/* Details */}
      <Box sx={{ px: 2, py: 1.5, flex: 1, overflowY: "auto" }}>
        <FieldRow label="Namespace" value={node.namespace} />
        <FieldRow label="Name" value={node.name} />

        {node.kind === "depot" && node.storageBackend && (
          <FieldRow label="Storage Backend" value={node.storageBackend} />
        )}

        {node.kind === "module" && node.provider && (
          <FieldRow label="Provider" value={node.provider} />
        )}

        {node.kind === "provider" && node.providerNamespace && (
          <FieldRow label="Provider Namespace" value={node.providerNamespace} />
        )}

        {node.kind !== "depot" && (
          <>
            <Divider sx={{ my: 1 }} />
            <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
              <Box
                sx={{
                  width: 8,
                  height: 8,
                  borderRadius: "50%",
                  bgcolor: node.synced ? "#3fb950" : "#f85149",
                  boxShadow: node.synced ? "0 0 6px #3fb950" : "0 0 6px #f85149",
                }}
              />
              <Typography variant="caption" sx={{ color: node.synced ? "#3fb950" : "#f85149" }}>
                {node.synced ? "Synced" : "Not Synced"}
              </Typography>
            </Box>
          </>
        )}
      </Box>

      {/* Actions */}
      {detailHref && (
        <Box sx={{ px: 2, pb: 1.5, borderTop: "1px solid rgba(240,246,252,0.08)", pt: 1.25 }}>
          <Button
            component={Link}
            href={detailHref}
            variant="outlined"
            size="small"
            fullWidth
            endIcon={<OpenInNewIcon sx={{ fontSize: 14 }} />}
            sx={{ fontSize: "0.78rem" }}
          >
            View Details
          </Button>
        </Box>
      )}
    </Box>
  );
}
