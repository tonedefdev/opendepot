"use client";

import { memo, useState } from "react";
import { Handle, Position, type NodeProps } from "reactflow";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import CircularProgress from "@mui/material/CircularProgress";
import IconButton from "@mui/material/IconButton";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";
import CloseIcon from "@mui/icons-material/Close";
import InfoOutlinedIcon from "@mui/icons-material/InfoOutlined";
import ArrowForwardIcon from "@mui/icons-material/ArrowForward";
import SettingsInputComponentIcon from "@mui/icons-material/SettingsInputComponent";
import Link from "next/link";
import type { ProviderSchemaBlock } from "@/lib/api";
import ProviderConfigurationModal from "./ProviderConfigurationModal";
import type { ProviderConfiguration, ReferenceOption } from "./types";
import { areAssemblyNodePropsEqual } from "./nodeMemo";

export interface ProviderNodeData {
  namespace: string;
  name: string;
  version: string;
  providerNamespace: string;
  providerName: string;
  localName: string;
  alias: string;
  localNameError: string | null;
  aliasError: string | null;
  schema: ProviderSchemaBlock;
  configuration: ProviderConfiguration;
  referenceOptions: ReferenceOption[];
  diagnostics?: string[];
  loading: boolean;
  error: string | null;
  onRemove: () => void;
  onAliasChange: (alias: string) => void;
  onConfigurationChange: (configuration: ProviderConfiguration) => void;
}

function ProviderNode({ data }: NodeProps<ProviderNodeData>) {
  const [open, setOpen] = useState(false);
  const configuredCount = Object.keys(data.configuration.arguments).length + Object.values(data.configuration.blocks).flat().length;
  const providerDetailsHref = `/${encodeURIComponent(data.namespace)}/provider/${encodeURIComponent(data.name)}`;

  return (
    <Box sx={{ width: 300, borderRadius: 2, border: "1px solid", borderColor: data.error || data.localNameError || data.aliasError || (data.diagnostics?.length ?? 0) > 0 ? "error.main" : "transparent", bgcolor: "background.paper", boxShadow: 3, overflow: "hidden" }}>
      <Box sx={{ px: 1, py: 0.75, bgcolor: "action.hover" }}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
          <SettingsInputComponentIcon sx={{ fontSize: 16, color: "warning.main" }} />
          <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: "nowrap" }}>
            provider.{data.localName}.
          </Typography>
          <TextField
            variant="standard"
            size="small"
            value={data.alias}
            onChange={(event) => data.onAliasChange(event.target.value)}
            error={!!data.aliasError}
            placeholder="default"
            slotProps={{ htmlInput: { "aria-label": `${data.localName} provider alias` } }}
            sx={{ flex: 1, minWidth: 0, "& input": { fontWeight: 700, fontSize: "0.8125rem" } }}
          />
          <IconButton size="small" onClick={data.onRemove} aria-label={`Remove ${data.localName}`}>
            <CloseIcon sx={{ fontSize: 14 }} />
          </IconButton>
        </Box>
        <Box
          component={Link}
          href={providerDetailsHref}
          className="nodrag nopan"
          aria-label={`Open ${data.providerNamespace}/${data.providerName} provider details`}
          sx={{
            display: "flex",
            alignItems: "center",
            gap: 0.5,
            minWidth: 0,
            mt: 0.5,
            color: "text.secondary",
            textDecoration: "none",
            cursor: "pointer",
            "& .provider-details-arrow": { display: "none" },
            "&:hover, &:focus-visible": { color: "text.primary" },
            "&:hover .provider-details-info, &:focus-visible .provider-details-info": { display: "none" },
            "&:hover .provider-details-arrow, &:focus-visible .provider-details-arrow": { display: "block" },
            "&:focus-visible": { outline: "2px solid", outlineColor: "primary.main", outlineOffset: 2, borderRadius: 0.5 },
          }}
        >
          <Typography variant="caption" noWrap sx={{ minWidth: 0, flex: 1, color: "inherit" }}>
            {data.providerNamespace}/{data.providerName} · {data.version.replace(/^v/, "")}
          </Typography>
          <InfoOutlinedIcon className="provider-details-info" sx={{ fontSize: 14, flexShrink: 0 }} />
          <ArrowForwardIcon className="provider-details-arrow" sx={{ fontSize: 14, flexShrink: 0 }} />
        </Box>
      </Box>

      <Box
        role="button"
        tabIndex={data.loading || !!data.error ? -1 : 0}
        aria-disabled={data.loading || !!data.error}
        onClick={() => {
          if (!data.loading && !data.error) setOpen(true);
        }}
        onKeyDown={(event) => {
          if (!data.loading && !data.error && (event.key === "Enter" || event.key === " ")) {
            event.preventDefault();
            setOpen(true);
          }
        }}
        sx={{
          position: "relative",
          display: "flex",
          alignItems: "center",
          gap: 0.75,
          px: 1,
          py: 0.75,
          cursor: data.loading || data.error ? "default" : "pointer",
          "&:hover": data.loading || data.error ? undefined : { bgcolor: "action.hover" },
        }}
      >
        <Handle type="target" position={Position.Left} id="in" isConnectable={false} style={{ left: -4, width: 8, height: 8, opacity: 0 }} />
        <Handle type="source" position={Position.Right} id="provider" isConnectable={false} style={{ right: -4, width: 8, height: 8, opacity: 0 }} />
        <Typography variant="caption" fontWeight={600}>
          Configuration
        </Typography>
        {data.loading ? (
          <CircularProgress size={14} />
        ) : (
          <Chip
            size="small"
            label={`${configuredCount} configured`}
            color={configuredCount > 0 ? "success" : "default"}
            variant="outlined"
            sx={{ height: 18, fontSize: "0.65rem" }}
          />
        )}
        {!data.loading && !data.error && <ChevronRightIcon sx={{ fontSize: 16, opacity: 0.6, ml: "auto" }} />}
      </Box>
      {data.error && <Typography variant="caption" color="error" sx={{ display: "block", px: 1, pb: 1 }}>{data.error}</Typography>}
      {data.diagnostics?.map((diagnostic, index) => (
        <Typography key={`${index}-${diagnostic}`} variant="caption" sx={{ display: "block", px: 1, pt: index === 0 ? 1 : 0, pb: 0.5, color: "error.main" }}>
          {diagnostic}
        </Typography>
      ))}

      <ProviderConfigurationModal
        open={open}
        onClose={() => setOpen(false)}
        localName={data.localName}
        alias={data.alias}
        schema={data.schema}
        value={data.configuration}
        referenceOptions={data.referenceOptions}
        onChange={data.onConfigurationChange}
      />
    </Box>
  );
}

export default memo(ProviderNode, areAssemblyNodePropsEqual);
