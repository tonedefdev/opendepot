"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import Typography from "@mui/material/Typography";
import { useColorScheme } from "@mui/material/styles";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import ExpandLessIcon from "@mui/icons-material/ExpandLess";
import DataObjectIcon from "@mui/icons-material/DataObject";
import DescriptionIcon from "@mui/icons-material/Description";
import { load } from "js-yaml";
import { Highlight, type Language, themes } from "prism-react-renderer";
import Prism from "prismjs";
import "prismjs/components/prism-hcl";
import "prismjs/components/prism-yaml";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeSanitize from "rehype-sanitize";
import CopyButton from "@/components/CopyButton";
import { highlightedMarkdownComponents } from "@/components/HighlightedCodeBlock";
import { SectionCard } from "@/components/SectionCard";
import type { AgentMetadata } from "@/lib/api";

// Lets prism-react-renderer use the full prismjs instance that has the HCL grammar registered.
(typeof globalThis !== "undefined" ? globalThis : window).Prism = Prism;

// Heights (px) match the module README so long definitions collapse the same way.
const COLLAPSED_HEIGHT = 360;
const EXPANDED_HEIGHT = 720;

interface AgentBodyProps {
  content: string;
}

export function AgentBody({ content }: AgentBodyProps) {
  const [expanded, setExpanded] = React.useState(false);
  const [overflows, setOverflows] = React.useState(false);
  const scrollRef = React.useRef<HTMLDivElement>(null);

  React.useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;

    setOverflows(el.scrollHeight > el.clientHeight + 1);
  }, [content, expanded]);

  return (
    <Box>
      <Box
        ref={scrollRef}
        sx={{
          maxHeight: expanded ? EXPANDED_HEIGHT : COLLAPSED_HEIGHT,
          overflowY: "auto",
          pr: 0.5,
          "& h1, & h2, & h3": { fontWeight: 600, mt: 2, mb: 1 },
          "& p, & li": { fontSize: "0.9rem", lineHeight: 1.6 },
          "& pre.md-plain-pre": { p: 1.5, borderRadius: 1, overflowX: "auto", bgcolor: "action.hover", fontSize: "0.8125rem" },
          "& code": { fontFamily: "monospace", fontSize: "0.8125rem" },
        }}
      >
        <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeSanitize]} components={highlightedMarkdownComponents({ renderEmbeddedMarkdown: true })}>
          {content}
        </ReactMarkdown>
      </Box>
      {overflows && (
        <Box sx={{ display: "flex", justifyContent: "center", pt: 1 }}>
          <Button
            size="small"
            onClick={() => setExpanded((prev) => !prev)}
            startIcon={expanded ? <ExpandLessIcon fontSize="small" /> : <ExpandMoreIcon fontSize="small" />}
          >
            {expanded ? "Show less" : "Show more"}
          </Button>
        </Box>
      )}
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

// Display-only: a top-level string value that overflows its container is shown as a YAML folded (>-)
// block, wrapped at the measured width. Values that fit stay on one line. The stored definition is
// unchanged, and folding keeps the parsed value the same.
export function foldFrontmatterValues(frontmatter: string, fits: (line: string) => boolean): string {
  const lines = frontmatter.split("\n");
  const out: string[] = [];

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const match = /^([A-Za-z_][\w-]*):[ \t]+(\S.*)$/.exec(line);
    const nextLine = lines[i + 1];
    const continues = nextLine !== undefined && /^\s/.test(nextLine);

    if (fits(line) || !match || continues || match[2].includes(" #")) {
      out.push(line);
      continue;
    }

    let value: unknown;
    try {
      value = load(match[2]);
    } catch {
      out.push(line);
      continue;
    }

    if (typeof value !== "string" || value !== value.trim() || value.includes("  ") || /[\t\r\n]/.test(value)) {
      out.push(line);
      continue;
    }

    const indent = "  ";
    const wrapped: string[] = [];
    let current = "";
    for (const word of value.split(" ")) {
      const candidate = current ? `${current} ${word}` : word;
      if (current && !fits(`${indent}${candidate}`)) {
        wrapped.push(current);
        current = word;
      } else {
        current = candidate;
      }
    }
    wrapped.push(current);

    out.push(`${match[1]}: >-`, ...wrapped.map((part) => `${indent}${part}`));
  }

  return out.join("\n");
}

// Splits a leading YAML frontmatter block (between two --- lines) from the Markdown body.
export function splitFrontmatter(content: string): { frontmatter: string | null; body: string } {
  const match = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/.exec(content);
  if (!match) {
    return { frontmatter: null, body: content };
  }

  return { frontmatter: match[1], body: content.slice(match[0].length) };
}

interface AgentDefinitionProps {
  content: string;
  fileName: string;
  title: string;
}

interface FrontmatterCodeProps {
  frontmatter: string;
  fileName: string;
}

function FrontmatterCode({ frontmatter, fileName }: FrontmatterCodeProps) {
  const { mode, systemMode } = useColorScheme();
  const resolvedMode = mode === "system" ? systemMode : mode;
  const prismTheme = resolvedMode === "light" ? themes.github : themes.nightOwl;
  const preRef = React.useRef<HTMLPreElement>(null);
  const probeRef = React.useRef<HTMLSpanElement>(null);
  const [code, setCode] = React.useState(frontmatter);

  React.useEffect(() => {
    const pre = preRef.current;
    const probe = probeRef.current;
    if (!pre || !probe) return;

    const compute = () => {
      const styles = window.getComputedStyle(pre);
      const inner = pre.clientWidth - parseFloat(styles.paddingLeft) - parseFloat(styles.paddingRight);
      const fits = (text: string) => {
        probe.textContent = text;
        return probe.getBoundingClientRect().width <= inner;
      };
      setCode(foldFrontmatterValues(frontmatter, fits));
    };

    compute();
    if (typeof ResizeObserver === "undefined") return;

    const observer = new ResizeObserver(compute);
    observer.observe(pre);
    return () => observer.disconnect();
  }, [frontmatter]);

  return (
    <Box sx={{ position: "relative", overflow: "hidden" }}>
      <Box
        component="span"
        ref={probeRef}
        aria-hidden
        sx={{
          position: "absolute",
          top: 0,
          left: 0,
          visibility: "hidden",
          whiteSpace: "pre",
          fontFamily: "monospace",
          fontSize: "0.8125rem",
        }}
      />
      <Highlight prism={Prism as typeof Prism} theme={prismTheme} code={code} language={"yaml" as Language}>
        {({ style, tokens, getLineProps, getTokenProps }) => (
          <Box
            component="pre"
            ref={preRef}
            aria-label={`${fileName} frontmatter`}
            sx={{
              m: 0,
              p: 1.5,
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

export function AgentDefinition({ content, fileName, title }: AgentDefinitionProps) {
  const { frontmatter, body } = splitFrontmatter(content);

  return (
    <>
      {frontmatter !== null && (
        <SectionCard icon={<DataObjectIcon fontSize="small" />} title="Frontmatter">
          <FrontmatterCode frontmatter={frontmatter} fileName={fileName} />
        </SectionCard>
      )}
      <SectionCard icon={<DescriptionIcon fontSize="small" />} title={title}>
        <AgentBody content={body} />
      </SectionCard>
    </>
  );
}
