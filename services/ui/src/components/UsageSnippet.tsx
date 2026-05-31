"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import CopyButton from "@/components/CopyButton";

interface UsageSnippetProps {
  kind: "module" | "provider";
  namespace: string;
  name: string;
  /** Module only — the Terraform provider name (e.g. "azurerm"). */
  provider?: string;
  /** Semver string — leading "v" is stripped for the HCL version attribute. */
  latestVersion?: string;
  /** Host + port derived from NEXT_PUBLIC_BASE_URL (e.g. "opendepot.localtest.me:8080"). */
  registryHost: string;
}

function stripV(v: string): string {
  return v.startsWith("v") ? v.slice(1) : v;
}

function buildProviderSnippet(registryHost: string, namespace: string, name: string, latestVersion?: string): string {
  const source = `${registryHost}/${namespace}/${name}`;
  const versionLine = latestVersion ? `\n      version = "${stripV(latestVersion)}"` : "";
  return `terraform {
  required_providers {
    ${name} = {
      source  = "${source}"${versionLine}
    }
  }
}`;
}

function buildModuleSnippet(
  registryHost: string,
  namespace: string,
  name: string,
  provider?: string,
  latestVersion?: string,
): string {
  const sourcePath = provider
    ? `${registryHost}/${namespace}/${name}/${provider}`
    : `${registryHost}/${namespace}/${name}`;
  const versionLine = latestVersion ? `\n  version = "${stripV(latestVersion)}"` : "";
  return `module "${name}" {
  source  = "${sourcePath}"${versionLine}
}`;
}

export default function UsageSnippet({
  kind,
  namespace,
  name,
  provider,
  latestVersion,
  registryHost,
}: UsageSnippetProps) {
  const snippet =
    kind === "provider"
      ? buildProviderSnippet(registryHost, namespace, name, latestVersion)
      : buildModuleSnippet(registryHost, namespace, name, provider, latestVersion);

  return (
    <Box sx={{ position: "relative" }}>
      <Box
        component="pre"
        sx={{
          m: 0,
          p: 2,
          pr: 6,
          borderRadius: 1.5,
          background: "rgba(0,0,0,0.35)",
          border: "1px solid rgba(240,246,252,0.08)",
          fontFamily: "monospace",
          fontSize: "0.8125rem",
          lineHeight: 1.65,
          color: "text.primary",
          overflowX: "auto",
          whiteSpace: "pre",
        }}
      >
        <Typography
          component="code"
          sx={{ fontFamily: "inherit", fontSize: "inherit", color: "inherit" }}
        >
          {snippet}
        </Typography>
      </Box>
      <Box sx={{ position: "absolute", top: 6, right: 6 }}>
        <CopyButton value={snippet} size="small" />
      </Box>
    </Box>
  );
}
