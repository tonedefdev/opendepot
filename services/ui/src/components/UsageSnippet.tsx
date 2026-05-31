"use client";

import * as React from "react";
import Box from "@mui/material/Box";
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

// ── Prism theme override using brand palette ──────────────────────────────────
// We start from the dracula base and replace colours with OpenDepot's palette.

const brandTheme: typeof themes.dracula = {
  ...themes.dracula,
  plain: {
    backgroundColor: "transparent",
    color: "rgba(240,246,252,0.87)",
  },
  styles: [
    // keywords: terraform, module, required_providers
    { types: ["keyword", "builtin"], style: { color: "#04cfd0" } },
    // attribute keys (property names)
    { types: ["property", "attr-name"], style: { color: "#047df1" } },
    // string values
    { types: ["string", "attr-value"], style: { color: "#03deb8" } },
    // punctuation — braces, equals, commas
    { types: ["punctuation", "operator"], style: { color: "rgba(240,246,252,0.45)" } },
    // fallback for anything else
    { types: ["plain"], style: { color: "rgba(240,246,252,0.87)" } },
  ],
};

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

  return (
    <Box sx={{ position: "relative" }}>
      <Highlight
        prism={Prism as typeof Prism}
        theme={brandTheme}
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
              background: "rgba(0,0,0,0.35)",
              border: "1px solid rgba(240,246,252,0.08)",
              fontFamily: "monospace",
              fontSize: "0.8125rem",
              lineHeight: 1.65,
              overflowX: "auto",
              whiteSpace: "pre",
              ...style,
              backgroundColor: undefined, // use our bg above, not the theme's
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
