"use client";

import * as React from "react";
import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import IconButton from "@mui/material/IconButton";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import FormControl from "@mui/material/FormControl";
import InputLabel from "@mui/material/InputLabel";
import Select from "@mui/material/Select";
import MenuItem from "@mui/material/MenuItem";
import CloseIcon from "@mui/icons-material/Close";
import WidgetsIcon from "@mui/icons-material/Widgets";
import WidgetsOutlinedIcon from "@mui/icons-material/WidgetsOutlined";
import { useColorScheme } from "@mui/material/styles";
import type { ContractRequiredProvider } from "@/lib/api";
import type { ProviderBinding, ProviderOption } from "./types";

interface Props {
  open: boolean;
  onClose: () => void;
  instanceName: string;
  requiredProviders: ContractRequiredProvider[];
  providerOptions: ProviderOption[];
  providerBindings: Record<string, ProviderBinding>;
  onProviderBindingChange: (localName: string, providerNodeId: string) => void;
}

function providerIdentity(source: string): string {
  const parts = source.split("/");
  return parts.slice(-2).join("/").toLowerCase();
}

export default function ProvidersModal({
  open,
  onClose,
  instanceName,
  requiredProviders,
  providerOptions,
  providerBindings,
  onProviderBindingChange,
}: Props) {
  const { mode, systemMode } = useColorScheme();
  const resolvedMode = mode === "system" ? systemMode : mode;
  const ModuleIcon = resolvedMode === "light" ? WidgetsIcon : WidgetsOutlinedIcon;
  const reactId = React.useId();
  const configuredCount = requiredProviders.filter(
    (provider) => providerBindings[provider.localName]?.providerNodeId,
  ).length;

  return (
    <Dialog open={open} onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle component="div" sx={{ display: "flex", flexDirection: "column", gap: 0.25 }}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          <Box sx={{ width: 24, height: 24, display: "grid", placeItems: "center", flexShrink: 0 }}>
            <ModuleIcon sx={{ fontSize: 20, color: "#03deb8" }} />
          </Box>
          <Typography component="h2" variant="subtitle1" fontWeight={700} noWrap sx={{ flex: 1, minWidth: 0 }}>
            module.{instanceName} — Providers
          </Typography>
          <IconButton size="small" onClick={onClose} aria-label="Close">
            <CloseIcon fontSize="small" />
          </IconButton>
        </Box>
        <Typography variant="caption" color="text.secondary">
          {configuredCount} of {requiredProviders.length} configured
        </Typography>
      </DialogTitle>
      <DialogContent dividers sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
        {requiredProviders.map((required) => {
          const compatible = providerOptions.filter(
            (provider) =>
              `${provider.providerNamespace}/${provider.providerName}`.toLowerCase() ===
              providerIdentity(required.source),
          );

          return (
            <Box key={required.localName}>
              <FormControl size="small" fullWidth>
                <InputLabel id={`${reactId}-provider-${required.localName}`}>{required.localName}</InputLabel>
                <Select
                  labelId={`${reactId}-provider-${required.localName}`}
                  label={required.localName}
                  value={providerBindings[required.localName]?.providerNodeId ?? ""}
                  onChange={(event) =>
                    onProviderBindingChange(required.localName, event.target.value as string)
                  }
                >
                  <MenuItem value="">
                    <em>Unbound</em>
                  </MenuItem>
                  {compatible.map((provider) => (
                    <MenuItem key={provider.nodeId} value={provider.nodeId}>
                      {provider.label}
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>
              {compatible.length === 0 && (
                <Typography variant="caption" color="text.secondary" sx={{ display: "block", mt: 0.5 }}>
                  Add a compatible {required.localName} provider to the canvas first.
                </Typography>
              )}
            </Box>
          );
        })}
      </DialogContent>
    </Dialog>
  );
}