"use client";

import * as React from "react";
import Box from "@mui/material/Box";
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

// ── HCL tokenizer ────────────────────────────────────────────────────────────

type TokenKind = "keyword" | "attr" | "string" | "brace" | "operator" | "plain";

interface HCLToken {
  kind: TokenKind;
  value: string;
}

const BLOCK_KEYWORDS = new Set(["terraform", "required_providers", "module"]);

function tokenizeHCL(input: string): HCLToken[] {
  const tokens: HCLToken[] = [];
  let i = 0;

  while (i < input.length) {
    const ch = input[i];

    // String literal — consume everything up to the closing unescaped quote.
    if (ch === '"') {
      let j = i + 1;
      while (j < input.length && input[j] !== '"') {
        if (input[j] === "\\") j++; // skip escaped character
        j++;
      }
      j++; // include closing quote
      tokens.push({ kind: "string", value: input.slice(i, j) });
      i = j;
      continue;
    }

    // Identifier — classify as keyword, attribute key, or plain.
    if (/[a-zA-Z_]/.test(ch)) {
      let j = i + 1;
      while (j < input.length && /[a-zA-Z0-9_-]/.test(input[j])) j++;
      const word = input.slice(i, j);
      const rest = input.slice(j);
      let kind: TokenKind = "plain";
      if (BLOCK_KEYWORDS.has(word)) {
        kind = "keyword";
      } else if (/^\s*=/.test(rest)) {
        kind = "attr";
      }
      tokens.push({ kind, value: word });
      i = j;
      continue;
    }

    // Braces
    if (ch === "{" || ch === "}") {
      tokens.push({ kind: "brace", value: ch });
      i++;
      continue;
    }

    // Equals operator
    if (ch === "=") {
      tokens.push({ kind: "operator", value: ch });
      i++;
      continue;
    }

    // Anything else (whitespace, newlines, punctuation) — batch until next special char.
    let j = i + 1;
    while (
      j < input.length &&
      input[j] !== '"' &&
      input[j] !== "{" &&
      input[j] !== "}" &&
      input[j] !== "=" &&
      !/[a-zA-Z_]/.test(input[j])
    ) {
      j++;
    }
    tokens.push({ kind: "plain", value: input.slice(i, j) });
    i = j;
  }

  return tokens;
}

// Brand palette token colours
const TOKEN_COLORS: Record<TokenKind, string> = {
  keyword:  "#04cfd0",              // secondary teal
  attr:     "#047df1",              // primary blue
  string:   "#03deb8",              // accent mint
  brace:    "rgba(240,246,252,0.5)",
  operator: "rgba(240,246,252,0.5)",
  plain:    "rgba(240,246,252,0.87)",
};

// ── Snippet builders ──────────────────────────────────────────────────────────

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

  const tokens = tokenizeHCL(snippet);

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
          overflowX: "auto",
          whiteSpace: "pre",
        }}
      >
        {tokens.map((tok, idx) => (
          <span key={idx} style={{ color: TOKEN_COLORS[tok.kind] }}>
            {tok.value}
          </span>
        ))}
      </Box>
      <Box sx={{ position: "absolute", top: 6, right: 6 }}>
        <CopyButton value={snippet} size="small" />
      </Box>
    </Box>
  );
}


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
