"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import { useColorScheme } from "@mui/material/styles";
import { Highlight, type Language, themes } from "prism-react-renderer";
import Prism from "prismjs";
import "prismjs/components/prism-hcl";
import CopyButton from "@/components/CopyButton";

// Make prism-react-renderer use the full prismjs instance so it picks up the
// HCL grammar we registered above via the side-effectful import.
(typeof globalThis !== "undefined" ? globalThis : window).Prism = Prism;

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

// ── Snippet builders ──────────────────────────────────────────────────────────

function stripV(v: string): string {
  return v.startsWith("v") ? v.slice(1) : v;
}

function buildProviderSnippet(
  registryHost: string,
  namespace: string,
  name: string,
  latestVersion?: string,
): string {
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



// ── Component ─────────────────────────────────────────────────────────────────

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

  const { mode, systemMode } = useColorScheme();
  const resolvedMode = mode === "system" ? systemMode : mode;
  const prismTheme = resolvedMode === "light" ? themes.github : themes.nightOwl;

  return (
    <Box sx={{ position: "relative" }}>
      <Highlight
        prism={Prism as typeof Prism}
        theme={prismTheme}
        code={snippet}
        language={"hcl" as Language}
      >
        {({ style, tokens, getLineProps, getTokenProps }) => (
          <Box
            component="pre"
            sx={{
              m: 0,
              p: 2,
              pr: 6,
              borderRadius: 1.5,
              border: "1px solid",
              borderColor: "divider",
              fontFamily: "monospace",
              fontSize: "0.8125rem",
              lineHeight: 1.65,
              overflowX: "auto",
              whiteSpace: "pre",
              ...style,
              ...(resolvedMode === "light" && { backgroundColor: "#f0f7ff" }),
            }}
          >
            {tokens.map((line, i) => (
              <div key={i} {...getLineProps({ line })}>
                {line.map((token, key) => (
                  <span key={key} {...getTokenProps({ token })} />
                ))}
              </div>
            ))}
          </Box>
        )}
      </Highlight>
      <Box sx={{ position: "absolute", top: 6, right: 6 }}>
        <CopyButton value={snippet} size="small" />
      </Box>
    </Box>
  );
}
