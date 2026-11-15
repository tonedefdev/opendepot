"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import { Highlight, type Language, type PrismTheme, themes } from "prism-react-renderer";
import { lightCodeTheme } from "@/lib/codeTheme";
import Prism from "prismjs";
import "@/lib/hclGrammar";
import CopyButton from "@/components/CopyButton";
import { buildCanonicalProviderSource, buildModuleSource, buildProviderMirrorUrl, stripV } from "@/lib/registrySource";

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
  /** Comma-separated constraint string (e.g. "~> 3.0.0"). Takes precedence over latestVersion when set. */
  versionConstraints?: string;
  /** Host + port derived from NEXT_PUBLIC_BASE_URL (e.g. "opendepot.localtest.me:8080"). */
  registryHost: string;
  /** External OpenDepot base URL including scheme. */
  registryBaseUrl: string;
  /** Provider only — canonical upstream namespace (e.g. "hashicorp"). */
  providerNamespace?: string;
  /** Provider only — canonical upstream registry hostname. */
  upstreamRegistry?: string;
}

// ── Snippet builders ──────────────────────────────────────────────────────────

function resolveVersion(latestVersion?: string, versionConstraints?: string): string | undefined {
  return versionConstraints || (latestVersion ? stripV(latestVersion) : undefined);
}

function buildProviderSnippet(
	upstreamRegistry: string,
  providerNamespace: string,
  name: string,
  latestVersion?: string,
  versionConstraints?: string,
): string {
  const source = buildCanonicalProviderSource(upstreamRegistry, providerNamespace, name);
  const version = resolveVersion(latestVersion, versionConstraints);
  const versionLine = version ? `\n      version = "${version}"` : "";
  return `terraform {
  required_providers {
    ${name} = {
      source  = "${source}"${versionLine}
    }
  }
}`;
}

function buildProviderCLIConfig(registryBaseUrl: string, namespace: string, providerSource: string): string {
  const mirrorUrl = buildProviderMirrorUrl(registryBaseUrl, namespace);
  return `provider_installation {
  network_mirror {
    url     = "${mirrorUrl}"
    include = ["${providerSource}"]
  }

  direct {
    exclude = ["${providerSource}"]
  }
}`;
}

function buildModuleSnippet(
  registryHost: string,
  namespace: string,
  name: string,
  provider?: string,
  latestVersion?: string,
  versionConstraints?: string,
): string {
  const sourcePath = buildModuleSource(registryHost, namespace, name, provider);
  const version = resolveVersion(latestVersion, versionConstraints);
  const versionLine = version ? `\n  version = "${version}"` : "";
  return `module "${name}" {
  source  = "${sourcePath}"${versionLine}
}`;
}



function SnippetCode({
  code,
  prismTheme,
  background,
}: {
  code: string;
  prismTheme: PrismTheme;
  background?: string;
}) {
  return (
    <Highlight prism={Prism as typeof Prism} theme={prismTheme} code={code} language={"hcl" as Language}>
      {({ style, tokens, getLineProps, getTokenProps }) => (
        <Box component="pre" sx={{ m: 0, p: 2, pr: 6, borderRadius: 1.5, border: "1px solid", borderColor: "divider", fontFamily: "var(--od-font-mono)", fontSize: "0.8125rem", lineHeight: 1.65, overflowX: "auto", whiteSpace: "pre", ...style, ...(background && { backgroundColor: background }) }}>
          {tokens.map((line, i) => (
            <div key={i} {...getLineProps({ line })}>
              {line.map((token, key) => <span key={key} {...getTokenProps({ token })} />)}
            </div>
          ))}
        </Box>
      )}
    </Highlight>
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

export default function UsageSnippet({
  kind,
  namespace,
  name,
  provider,
  latestVersion,
  versionConstraints,
  registryHost,
  registryBaseUrl,
  providerNamespace = "hashicorp",
	upstreamRegistry = "registry.opentofu.org",
}: UsageSnippetProps) {
  const providerSource = buildCanonicalProviderSource(upstreamRegistry, providerNamespace, name);
  const snippets = kind === "provider"
    ? [
        { label: "CLI configuration", code: buildProviderCLIConfig(registryBaseUrl, namespace, providerSource) },
        { label: "Provider requirement", code: buildProviderSnippet(upstreamRegistry, providerNamespace, name, latestVersion, versionConstraints) },
      ]
    : [{ label: "Module requirement", code: buildModuleSnippet(registryHost, namespace, name, provider, latestVersion, versionConstraints) }];

  return (
    // minmax(0, 1fr) keeps the column at card width so long lines scroll inside the <pre> instead of widening the card.
    <Box sx={{ display: "grid", gridTemplateColumns: "minmax(0, 1fr)", gap: 2 }}>
      {snippets.map((snippet) => (
        <Box key={snippet.label}>
          <Box sx={{ mb: 0.75, color: "text.secondary", fontSize: "0.75rem", fontWeight: 600 }}>{snippet.label}</Box>
          <Box sx={{ position: "relative" }}>
            {/* Both schemes are rendered and CSS shows the active one, so the code never paints in the wrong palette before React resolves the color mode. */}
            <Box sx={{ "[data-mui-color-scheme='light'] &": { display: "none" } }}>
              <SnippetCode code={snippet.code} prismTheme={themes.nightOwl} background="background.default" />
            </Box>
            <Box sx={{ display: "none", "[data-mui-color-scheme='light'] &": { display: "block" } }}>
              <SnippetCode code={snippet.code} prismTheme={lightCodeTheme} background="background.default" />
            </Box>
            <Box sx={{ position: "absolute", top: 6, right: 6 }}>
              <CopyButton value={snippet.code} size="small" />
            </Box>
          </Box>
        </Box>
      ))}
    </Box>
  );
}
