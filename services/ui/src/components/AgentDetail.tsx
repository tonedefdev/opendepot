"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Typography from "@mui/material/Typography";
import { useColorScheme } from "@mui/material/styles";
import { Highlight, type Language, themes } from "prism-react-renderer";
import Prism from "prismjs";
import "prismjs/components/prism-hcl";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeSanitize from "rehype-sanitize";
import CopyButton from "@/components/CopyButton";
import type { AgentMetadata } from "@/lib/api";

// Lets prism-react-renderer use the full prismjs instance that has the HCL grammar registered.
(typeof globalThis !== "undefined" ? globalThis : window).Prism = Prism;

interface AgentBodyProps {
  content: string;
}

export function AgentBody({ content }: AgentBodyProps) {
  return (
    <Box
      sx={{
        maxHeight: 720,
        overflow: "auto",
        "& h1, & h2, & h3": { fontWeight: 600, mt: 2, mb: 1 },
        "& p, & li": { fontSize: "0.9rem", lineHeight: 1.6 },
        "& pre": { p: 1.5, borderRadius: 1, overflowX: "auto", bgcolor: "action.hover", fontSize: "0.8125rem" },
        "& code": { fontFamily: "monospace", fontSize: "0.8125rem" },
      }}
    >
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeSanitize]}>
        {content}
      </ReactMarkdown>
    </Box>
  );
}

interface AgentMetadataChipsProps {
  metadata?: AgentMetadata;
}

export function AgentMetadataChips({ metadata }: AgentMetadataChipsProps) {
  if (!metadata?.model && (!metadata?.tools || metadata.tools.length === 0)) {
    return null;
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
      {metadata.model && (
        <Box>
          <Typography variant="caption" color="text.secondary" sx={{ textTransform: "uppercase", letterSpacing: "0.04em", fontWeight: 600 }}>
            Model
          </Typography>
          <Box sx={{ mt: 0.5 }}>
            <Chip label={metadata.model} size="small" variant="outlined" sx={{ fontFamily: "monospace" }} />
          </Box>
        </Box>
      )}
      {metadata.tools && metadata.tools.length > 0 && (
        <Box>
          <Typography variant="caption" color="text.secondary" sx={{ textTransform: "uppercase", letterSpacing: "0.04em", fontWeight: 600 }}>
            Tools
          </Typography>
          <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.75, mt: 0.5 }}>
            {metadata.tools.map((tool) => (
              <Chip key={tool} label={tool} size="small" variant="outlined" sx={{ fontFamily: "monospace" }} />
            ))}
          </Box>
        </Box>
      )}
    </Box>
  );
}

interface AgentConfigSnippetProps {
  kind: string;
  namespace: string;
  name: string;
  registryHost: string;
  latestVersion?: string;
}

// Escapes content for an HCL quoted string so values cannot break the block or open template interpolation.
function escapeHclString(value: string): string {
  return value
    .replace(/\\/g, "\\\\")
    .replace(/"/g, '\\"')
    .replace(/\$\{/g, () => "$${")
    .replace(/%\{/g, () => "%%{")
    .replace(/\n/g, "\\n")
    .replace(/\r/g, "\\r");
}

// Matches the opendepot.hcl block shape: skills use agent_skill, agents use agent.
export function buildAgentConfigSnippet({ kind, namespace, name, registryHost, latestVersion }: AgentConfigSnippetProps): string {
  const blockType = kind === "skill" ? "agent_skill" : "agent";
  const version = latestVersion ? escapeHclString(latestVersion.replace(/^v/, "")) : "";
  const versionLine = version ? `\n  version = "${version}"` : "";
  const escapedName = escapeHclString(name);
  const escapedNamespace = escapeHclString(namespace);
  const escapedRegistryHost = escapeHclString(registryHost);

  return `opendepot {
  targets = ["copilot", "claude"]
}

${blockType} "${escapedName}" {
  source = "${escapedRegistryHost}/${escapedNamespace}/${escapedName}"${versionLine}
}
`;
}

export function AgentConfigSnippet(props: AgentConfigSnippetProps) {
  const snippet = buildAgentConfigSnippet(props);
  const { mode, systemMode } = useColorScheme();
  const resolvedMode = mode === "system" ? systemMode : mode;
  const prismTheme = resolvedMode === "light" ? themes.github : themes.nightOwl;

  return (
    <Box sx={{ position: "relative" }}>
      <Box sx={{ position: "absolute", top: 8, right: 8, zIndex: 1 }}>
        <CopyButton value={snippet} />
      </Box>
      <Highlight prism={Prism as typeof Prism} theme={prismTheme} code={snippet} language={"hcl" as Language}>
        {({ style, tokens, getLineProps, getTokenProps }) => (
          <Box
            component="pre"
            sx={{
              m: 0,
              p: 2,
              pr: 5,
              borderRadius: 1,
              overflowX: "auto",
              fontFamily: "monospace",
              fontSize: "0.8125rem",
              lineHeight: 1.6,
              ...style,
              ...(resolvedMode === "light" && { backgroundColor: "#f0f7ff" }),
            }}
          >
            {tokens.map((line, i) => (
              <div key={i} {...getLineProps({ line })}>
                {line.map((token, key) => <span key={key} {...getTokenProps({ token })} />)}
              </div>
            ))}
          </Box>
        )}
      </Highlight>
    </Box>
  );
}
