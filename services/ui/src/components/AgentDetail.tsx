"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import ToggleButton from "@mui/material/ToggleButton";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";
import Typography from "@mui/material/Typography";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import ExpandLessIcon from "@mui/icons-material/ExpandLess";
import DataObjectIcon from "@mui/icons-material/DataObject";
import DescriptionIcon from "@mui/icons-material/Description";
import TuneIcon from "@mui/icons-material/Tune";
import { SiClaude, SiGithubcopilot, SiOpenai } from "react-icons/si";
import { load } from "js-yaml";
import type { Language } from "prism-react-renderer";
import { ThemedCode } from "@/components/ThemedCode";
import Prism from "prismjs";
import "prismjs/components/prism-hcl";
import "prismjs/components/prism-yaml";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeSanitize from "rehype-sanitize";
import CopyButton from "@/components/CopyButton";
import { highlightedMarkdownComponents } from "@/components/HighlightedCodeBlock";
import { markdownTableSx } from "@/components/MarkdownTable";
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
          "& code": { fontFamily: "var(--od-font-mono)", fontSize: "0.8125rem" },
          ...markdownTableSx,
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
            <Chip label={metadata.model} size="small" variant="outlined" sx={{ fontFamily: "var(--od-font-mono)" }} />
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
              <Chip key={tool} label={tool} size="small" variant="outlined" sx={{ fontFamily: "var(--od-font-mono)" }} />
            ))}
          </Box>
        </Box>
      )}
    </Box>
  );
}

// Values accepted by the CLI's opendepot.hcl targets list.
export type SnippetTarget = "copilot" | "claude" | "codex" | "agentsmd";

const DEFAULT_SNIPPET_TARGETS: readonly SnippetTarget[] = ["claude"];

function GenericTargetIcon({ size }: { size?: number }) {
  return <TuneIcon sx={{ fontSize: size }} />;
}

// Display order for the target buttons. OpenAI uses the codex target and Generic uses the agentsmd target.
const SNIPPET_TARGET_OPTIONS: ReadonlyArray<{
  target: SnippetTarget;
  label: string;
  Icon: React.ComponentType<{ size?: number }>;
}> = [
  { target: "claude", label: "Claude", Icon: SiClaude },
  { target: "copilot", label: "Copilot", Icon: SiGithubcopilot },
  { target: "codex", label: "OpenAI", Icon: SiOpenai },
  { target: "agentsmd", label: "Generic", Icon: GenericTargetIcon },
];

interface AgentConfigSnippetProps {
  kind: string;
  namespace: string;
  name: string;
  registryHost: string;
  latestVersion?: string;
  targets?: readonly SnippetTarget[];
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
export function buildAgentConfigSnippet({ kind, namespace, name, registryHost, latestVersion, targets }: AgentConfigSnippetProps): string {
  const blockType = kind === "skill" ? "agent_skill" : "agent";
  const version = latestVersion ? escapeHclString(latestVersion.replace(/^v/, "")) : "";
  const versionLine = version ? `\n  version = "${version}"` : "";
  const escapedName = escapeHclString(name);
  const escapedNamespace = escapeHclString(namespace);
  const escapedRegistryHost = escapeHclString(registryHost);
  const selected = targets?.length ? targets : DEFAULT_SNIPPET_TARGETS;
  const orderedTargets = SNIPPET_TARGET_OPTIONS.map((option) => option.target).filter((target) => selected.includes(target));
  const targetsList = orderedTargets.map((target) => `"${target}"`).join(", ");

  return `opendepot {
  targets = [${targetsList}]
}

${blockType} "${escapedName}" {
  source = "${escapedRegistryHost}/${escapedNamespace}/${escapedName}"${versionLine}
}
`;
}

export function AgentConfigSnippet(props: AgentConfigSnippetProps) {
  const [selectedTargets, setSelectedTargets] = React.useState<SnippetTarget[]>([...DEFAULT_SNIPPET_TARGETS]);
  const snippet = buildAgentConfigSnippet({ ...props, targets: selectedTargets });
  return (
    <Box>
      <ToggleButtonGroup
        size="small"
        value={selectedTargets}
        aria-label="Target platforms"
        onChange={(_event, value: SnippetTarget[]) => {
          // Keep at least one target so the generated targets list is never empty.
          if (value.length > 0) setSelectedTargets(value);
        }}
        sx={{ mb: 1.5, flexWrap: { xs: "nowrap", sm: "wrap" }, width: { xs: "100%", sm: "auto" } }}
      >
        {SNIPPET_TARGET_OPTIONS.map(({ target, label, Icon }) => (
          <ToggleButton
            key={target}
            value={target}
            sx={{
              textTransform: "none",
              gap: { xs: 0.5, sm: 1 },
              px: { xs: 0.5, sm: 2 },
              flex: { xs: 1, sm: "0 1 auto" },
              minWidth: 0,
              whiteSpace: "nowrap",
            }}
          >
            <Icon size={16} />
            {label}
          </ToggleButton>
        ))}
      </ToggleButtonGroup>
      <Box sx={{ position: "relative" }}>
        <Box sx={{ position: "absolute", top: 8, right: 8, zIndex: 1 }}>
          <CopyButton value={snippet} />
        </Box>
        <ThemedCode
          code={snippet}
          language={"hcl" as Language}
          preSx={(scheme) => ({
            pr: 5,
            lineHeight: 1.6,
            borderRadius: 1,
            border: 0,
            ...(scheme === "light" && { backgroundColor: "background.default" }),
          })}
        />
      </Box>
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

// Matches the p: 1.5 padding in the frontmatter preSx below.
const FRONTMATTER_PADDING_X = 12;

interface FrontmatterCodeProps {
  frontmatter: string;
  fileName: string;
}

function FrontmatterCode({ frontmatter, fileName }: FrontmatterCodeProps) {
  // The folding width is measured on this wrapper. Each copy of the code is a <pre>, and the hidden copy has no width.
  const containerRef = React.useRef<HTMLDivElement>(null);
  const probeRef = React.useRef<HTMLSpanElement>(null);
  const [code, setCode] = React.useState(frontmatter);

  React.useEffect(() => {
    const container = containerRef.current;
    const probe = probeRef.current;
    if (!container || !probe) return;

    const compute = () => {
      const inner = container.clientWidth - FRONTMATTER_PADDING_X * 2;
      if (!Number.isFinite(inner) || inner <= 0) {
        setCode(frontmatter);
        return;
      }
      const fits = (text: string) => {
        probe.textContent = text;
        return probe.getBoundingClientRect().width <= inner;
      };
      setCode(foldFrontmatterValues(frontmatter, fits));
    };

    compute();
    if (typeof ResizeObserver === "undefined") return;

    const observer = new ResizeObserver(compute);
    observer.observe(container);
    return () => observer.disconnect();
  }, [frontmatter]);

  return (
    <Box ref={containerRef} sx={{ position: "relative", overflow: "hidden" }}>
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
          fontFamily: "var(--od-font-mono)",
          fontSize: "0.8125rem",
        }}
      />
      <ThemedCode
        code={code}
        language={"yaml" as Language}
        ariaLabel={`${fileName} frontmatter`}
        preSx={(scheme) => ({ p: 1.5, pr: 1.5, lineHeight: 1.6, borderRadius: 1, border: 0, ...(scheme === "light" && { backgroundColor: "background.default" }) })}
      />
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
