"use client";

import * as React from "react";
import {
  Alert,
  Box,
  Button,
  Chip,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Tab,
  Tabs,
  Tooltip,
  Typography,
  useMediaQuery,
} from "@mui/material";
import { useColorScheme, useTheme } from "@mui/material/styles";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import ExpandLessIcon from "@mui/icons-material/ExpandLess";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Highlight, type Language, type PrismTheme, themes } from "prism-react-renderer";
import { lightCodeTheme } from "@/lib/codeTheme";
import Prism from "prismjs";
import "prismjs/components/prism-hcl";
import CopyButton from "@/components/CopyButton";
import MobileRecordCard, { MobileRecordList, type RecordField } from "@/components/MobileRecordCard";
import type {
  BrowseContract,
  ContractOutput,
  ContractVariable,
  CtyType,
} from "@/lib/api";

// Make prism-react-renderer use the full prismjs instance so it picks up the
// HCL grammar we registered above via the side-effectful import (mirrors
// UsageSnippet.tsx / ResourceReadme.tsx).
(typeof globalThis !== "undefined" ? globalThis : window).Prism = Prism;

// Height (px) of the Inputs/Outputs viewport when collapsed/expanded. Mirrors
// ResourceReadme.tsx so a module with a huge contract never forces the whole
// page to scroll past it before reaching the sections below.
const COLLAPSED_HEIGHT = 420;
const EXPANDED_HEIGHT = 900;

// A pretty-printed type past this many lines is collapsed by default so a
// single deeply-nested `object({...})` variable doesn't dominate the table.
const TYPE_COLLAPSE_LINES = 8;
const TYPE_COLLAPSED_HEIGHT = 160;

// A rendered Description past this height gets its own scrollbar and a
// show more/less toggle, so a long markdown blurb doesn't blow out the row.
const DESCRIPTION_COLLAPSED_HEIGHT = 120;
const DESCRIPTION_EXPANDED_HEIGHT = 400;

interface Props {
  contract: BrowseContract;
}

const GRADE_COLOR: Record<string, "success" | "warning" | "default"> = {
  full: "success",
  partial: "warning",
  unsupported: "default",
};

const GRADE_HELP: Record<string, string> = {
  full: "Every variable is typed and every output type was resolved.",
  partial: "Some types could not be resolved. Untyped variables, child modules, or missing provider schemas degrade a contract.",
  unsupported: "This module could not be parsed, so no contract could be derived.",
};

const CONFIDENCE_COLOR: Record<string, "success" | "info" | "default"> = {
  exact: "success",
  inferred: "info",
  unknown: "default",
};

/**
 * Renders a cty type constraint as OpenTofu source form, pretty-printed across
 * multiple lines so nested `object({...})`/`tuple([...])` types are readable
 * instead of one long unbroken string. The wire format is the same encoding
 * `tofu providers schema -json` uses: a primitive name, or a tuple whose first
 * element is the collection kind. Attribute names present in `optional` are
 * wrapped in `optional(...)`, matching how they were declared in HCL.
 */
function renderType(t: CtyType, optional: Record<string, boolean> = {}, depth = 0): string {
  if (typeof t === "string") {
    return t;
  }

  if (!Array.isArray(t) || t.length === 0) {
    return "dynamic";
  }

  const kind = t[0];
  const indent = "  ".repeat(depth + 1);
  const closeIndent = "  ".repeat(depth);

  if (kind === "list" || kind === "set" || kind === "map") {
    return `${kind}(${renderType(t[1] as CtyType, optional, depth)})`;
  }

  if (kind === "object") {
    const attrs = (t[1] ?? {}) as Record<string, CtyType>;
    const keys = Object.keys(attrs);

    if (keys.length === 0) {
      return "object({})";
    }

    const entries = keys.map((key) => {
      const rendered = renderType(attrs[key], optional, depth + 1);
      const value = optional[key] ? `optional(${rendered})` : rendered;

      return `${indent}${key} = ${value}`;
    });

    return `object({\n${entries.join("\n")}\n${closeIndent}})`;
  }

  if (kind === "tuple") {
    const elements = (t[1] ?? []) as CtyType[];

    if (elements.length === 0) {
      return "tuple([])";
    }

    const entries = elements.map((el) => `${indent}${renderType(el, optional, depth + 1)}`);

    return `tuple([\n${entries.join(",\n")}\n${closeIndent}])`;
  }

  return String(kind);
}

/**
 * Renders a default value as OpenTofu/HCL source form (mirrors renderType's
 * pretty-printing) rather than JSON — object/array defaults use `key = value`
 * and bare brackets instead of JSON's quoted keys and colons.
 */
function renderDefaultHCL(value: unknown, depth = 0): string {
  if (value === null || value === undefined) return "null";
  if (typeof value === "string") return `"${value.replace(/"/g, '\\"')}"`;
  if (typeof value === "boolean" || typeof value === "number") return String(value);

  const indent = "  ".repeat(depth + 1);
  const closeIndent = "  ".repeat(depth);

  if (Array.isArray(value)) {
    if (value.length === 0) return "[]";

    const entries = value.map((el) => `${indent}${renderDefaultHCL(el, depth + 1)}`);

    return `[\n${entries.join(",\n")}\n${closeIndent}]`;
  }

  if (typeof value === "object") {
    const entries = Object.entries(value as Record<string, unknown>);

    if (entries.length === 0) return "{}";

    const lines = entries.map(([key, val]) => `${indent}${key} = ${renderDefaultHCL(val, depth + 1)}`);

    return `{\n${lines.join("\n")}\n${closeIndent}}`;
  }

  return String(value);
}

function DefaultCode({
  value,
  prismTheme,
  lightBg,
  copyable = true,
}: {
  value: unknown;
  prismTheme: PrismTheme;
  lightBg: boolean;
  copyable?: boolean;
}) {
  const code = React.useMemo(() => (value === undefined ? "" : renderDefaultHCL(value)), [value]);

  if (value === undefined) {
    return (
      <Typography variant="body2" color="text.secondary">
        —
      </Typography>
    );
  }

  return (
    <Box sx={{ position: "relative", display: "inline-block", maxWidth: "100%" }}>
      <Highlight prism={Prism as typeof Prism} theme={prismTheme} code={code} language={"hcl" as Language}>
        {({ style, tokens, getLineProps, getTokenProps }) => (
          <Box
            component="pre"
            sx={{
              m: 0,
              pl: 1,
              pr: copyable ? 4 : 1,
              py: 0.5,
              display: "inline-block",
              width: "fit-content",
              maxWidth: "100%",
              borderRadius: 1,
              border: "1px solid",
              borderColor: "divider",
              fontFamily: "var(--od-font-mono)",
              fontSize: "0.75rem",
              lineHeight: 1.5,
              overflowX: "auto",
              whiteSpace: "pre",
              ...style,
              ...(lightBg && { backgroundColor: "#f0f7ff" }),
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
      {copyable && (
        <Box sx={{ position: "absolute", top: 2, right: 2 }}>
          <CopyButton value={code} />
        </Box>
      )}
    </Box>
  );
}

function TypeCode({
  type,
  optionalAttributes,
  prismTheme,
  lightBg,
  copyable = true,
}: {
  type: CtyType;
  optionalAttributes?: Record<string, boolean>;
  prismTheme: PrismTheme;
  lightBg: boolean;
  copyable?: boolean;
}) {
  const code = React.useMemo(
    () => renderType(type, optionalAttributes ?? {}),
    [type, optionalAttributes],
  );
  const lineCount = code.split("\n").length;
  const collapsible = lineCount > TYPE_COLLAPSE_LINES;
  const [expanded, setExpanded] = React.useState(false);

  return (
    <Box>
      <Box sx={{ position: "relative", display: "inline-block", maxWidth: "100%" }}>
        <Highlight prism={Prism as typeof Prism} theme={prismTheme} code={code} language={"hcl" as Language}>
          {({ style, tokens, getLineProps, getTokenProps }) => (
            <Box
              component="pre"
              sx={{
                m: 0,
                pl: 1,
                pr: copyable ? 4 : 1,
                py: 1,
                display: "inline-block",
                width: "fit-content",
                maxWidth: "100%",
                borderRadius: 1,
                border: "1px solid",
                borderColor: "divider",
                fontFamily: "var(--od-font-mono)",
                fontSize: "0.75rem",
                lineHeight: 1.5,
                overflowX: "auto",
                overflowY: collapsible && !expanded ? "hidden" : "visible",
                maxHeight: collapsible && !expanded ? TYPE_COLLAPSED_HEIGHT : "none",
                whiteSpace: "pre",
                ...style,
                ...(lightBg && { backgroundColor: "#f0f7ff" }),
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
        {copyable && (
          <Box sx={{ position: "absolute", top: 2, right: 2 }}>
            <CopyButton value={code} />
          </Box>
        )}
      </Box>
      {collapsible && (
        <Button
          size="small"
          onClick={() => setExpanded((prev) => !prev)}
          startIcon={expanded ? <ExpandLessIcon fontSize="small" /> : <ExpandMoreIcon fontSize="small" />}
          sx={{ mt: 0.5, fontSize: "0.6875rem", minWidth: 0 }}
        >
          {expanded ? "Collapse type" : `Show full type (${lineCount} lines)`}
        </Button>
      )}
    </Box>
  );
}

function MarkdownDescription({ text }: { text?: string }) {
  const contentRef = React.useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = React.useState(false);
  const [overflows, setOverflows] = React.useState(false);

  React.useEffect(() => {
    const el = contentRef.current;
    if (!el) return;

    setOverflows(el.scrollHeight > DESCRIPTION_COLLAPSED_HEIGHT + 1);
  }, [text]);

  if (!text) {
    return (
      <Typography variant="body2" color="text.secondary">
        —
      </Typography>
    );
  }

  return (
    <Box>
      <Box
        ref={contentRef}
        sx={{
          fontSize: "0.8125rem",
          maxHeight: expanded ? DESCRIPTION_EXPANDED_HEIGHT : DESCRIPTION_COLLAPSED_HEIGHT,
          overflowY: "auto",
          pr: 0.5,
          "& p": { m: 0 },
          "& p:not(:last-child)": { mb: 1 },
          "& ul, & ol": { m: 0, pl: 2.5 },
          "& ul:not(:last-child), & ol:not(:last-child)": { mb: 1 },
          "& li": { mb: 0.25 },
          "& a": { color: "primary.main", textDecoration: "none" },
          "& a:hover": { textDecoration: "underline" },
          "& code": {
            fontFamily: "var(--od-font-mono)",
            fontSize: "0.75rem",
            color: "warning.main",
            bgcolor: "action.hover",
            px: 0.5,
            py: 0.125,
            borderRadius: 0.5,
          },
        }}
      >
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{text}</ReactMarkdown>
      </Box>
      {overflows && (
        <Button
          size="small"
          onClick={() => setExpanded((prev) => !prev)}
          startIcon={expanded ? <ExpandLessIcon fontSize="small" /> : <ExpandMoreIcon fontSize="small" />}
          sx={{ mt: 0.5, fontSize: "0.6875rem", minWidth: 0 }}
        >
          {expanded ? "Show less" : "Show more"}
        </Button>
      )}
    </Box>
  );
}

function VariablesTable({
  variables,
  prismTheme,
  lightBg,
}: {
  variables: ContractVariable[];
  prismTheme: PrismTheme;
  lightBg: boolean;
}) {
  return (
    <TableContainer sx={{ overflowX: "visible", overflowY: "visible" }}>
      <Table size="small" stickyHeader>
        <TableHead>
          <TableRow>
            <TableCell>Name</TableCell>
            <TableCell>Type</TableCell>
            <TableCell>Required</TableCell>
            <TableCell>Default</TableCell>
            <TableCell>Description</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {variables.map((v) => (
            <TableRow key={v.name} hover>
              <TableCell sx={{ fontFamily: "var(--od-font-mono)", fontSize: "0.8125rem", whiteSpace: "nowrap", verticalAlign: "top" }}>
                {v.name}
                {v.sensitive && (
                  <Chip label="sensitive" size="small" color="warning" variant="outlined" sx={{ ml: 1 }} />
                )}
              </TableCell>
              <TableCell sx={{ verticalAlign: "top", minWidth: 220 }}>
                <TypeCode
                  type={v.type}
                  optionalAttributes={v.optionalAttributes}
                  prismTheme={prismTheme}
                  lightBg={lightBg}
                  copyable={false}
                />
              </TableCell>
              <TableCell sx={{ verticalAlign: "top" }}>
                <Chip
                  label={v.required ? "required" : "optional"}
                  size="small"
                  color={v.required ? "primary" : "default"}
                  variant="outlined"
                />
              </TableCell>
              <TableCell sx={{ verticalAlign: "top" }}>
                <DefaultCode value={v.default} prismTheme={prismTheme} lightBg={lightBg} copyable={false} />
              </TableCell>
              <TableCell sx={{ verticalAlign: "top", minWidth: 200 }}>
                <MarkdownDescription text={v.description} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
  );
}

function OutputsTable({
  outputs,
  prismTheme,
  lightBg,
}: {
  outputs: ContractOutput[];
  prismTheme: PrismTheme;
  lightBg: boolean;
}) {
  return (
    <TableContainer sx={{ overflowX: "visible", overflowY: "visible" }}>
      <Table size="small" stickyHeader>
        <TableHead>
          <TableRow>
            <TableCell>Name</TableCell>
            <TableCell>Type</TableCell>
            <TableCell>Confidence</TableCell>
            <TableCell>Description</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {outputs.map((o) => (
            <TableRow key={o.name} hover>
              <TableCell sx={{ fontFamily: "var(--od-font-mono)", fontSize: "0.8125rem", whiteSpace: "nowrap", verticalAlign: "top" }}>
                {o.name}
                {o.sensitive && (
                  <Chip label="sensitive" size="small" color="warning" variant="outlined" sx={{ ml: 1 }} />
                )}
              </TableCell>
              <TableCell sx={{ verticalAlign: "top", minWidth: 220 }}>
                <TypeCode type={o.type} prismTheme={prismTheme} lightBg={lightBg} copyable={false} />
              </TableCell>
              <TableCell sx={{ verticalAlign: "top" }}>
                <Tooltip title={o.confidence === "exact" ? "" : o.reason || ""} placement="top">
                  <Chip
                    label={o.confidence}
                    size="small"
                    color={CONFIDENCE_COLOR[o.confidence] ?? "default"}
                    variant="outlined"
                  />
                </Tooltip>
              </TableCell>
              <TableCell sx={{ verticalAlign: "top", minWidth: 200 }}>
                <MarkdownDescription text={o.description} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
  );
}

function VariableCard({
  v,
  prismTheme,
  lightBg,
}: {
  v: ContractVariable;
  prismTheme: PrismTheme;
  lightBg: boolean;
}) {
  const fields: RecordField[] = [
    {
      label: "Type",
      wide: true,
      value: <TypeCode type={v.type} optionalAttributes={v.optionalAttributes} prismTheme={prismTheme} lightBg={lightBg} copyable={false} />,
    },
    {
      label: "Required",
      value: (
        <Chip
          label={v.required ? "required" : "optional"}
          size="small"
          color={v.required ? "primary" : "default"}
          variant="outlined"
        />
      ),
    },
    {
      label: "Default",
      value: <DefaultCode value={v.default} prismTheme={prismTheme} lightBg={lightBg} copyable={false} />,
    },
    { label: "Description", wide: true, value: <MarkdownDescription text={v.description} /> },
  ];

  return (
    <MobileRecordCard
      title={
        <Box display="flex" alignItems="center" flexWrap="wrap" gap={1}>
          <Typography variant="body2" sx={{ fontFamily: "var(--od-font-mono)", fontWeight: 600 }}>
            {v.name}
          </Typography>
          {v.sensitive && <Chip label="sensitive" size="small" color="warning" variant="outlined" />}
        </Box>
      }
      fields={fields}
    />
  );
}

function OutputCard({
  o,
  prismTheme,
  lightBg,
}: {
  o: ContractOutput;
  prismTheme: PrismTheme;
  lightBg: boolean;
}) {
  const fields: RecordField[] = [
    {
      label: "Type",
      wide: true,
      value: <TypeCode type={o.type} prismTheme={prismTheme} lightBg={lightBg} copyable={false} />,
    },
    {
      label: "Confidence",
      value: (
        <Tooltip title={o.confidence === "exact" ? "" : o.reason || ""} placement="top">
          <Chip
            label={o.confidence}
            size="small"
            color={CONFIDENCE_COLOR[o.confidence] ?? "default"}
            variant="outlined"
          />
        </Tooltip>
      ),
    },
    { label: "Description", wide: true, value: <MarkdownDescription text={o.description} /> },
  ];

  return (
    <MobileRecordCard
      title={
        <Box display="flex" alignItems="center" flexWrap="wrap" gap={1}>
          <Typography variant="body2" sx={{ fontFamily: "var(--od-font-mono)", fontWeight: 600 }}>
            {o.name}
          </Typography>
          {o.sensitive && <Chip label="sensitive" size="small" color="warning" variant="outlined" />}
        </Box>
      }
      fields={fields}
    />
  );
}

export default function ContractTable({ contract }: Props) {
  const [tab, setTab] = React.useState(0);
  const [expanded, setExpanded] = React.useState(false);
  const [overflows, setOverflows] = React.useState(false);
  const scrollRef = React.useRef<HTMLDivElement>(null);
  const theme = useTheme();
  // Below lg the table is replaced by cards, so there is no scroll viewport to measure.
  const compact = useMediaQuery(theme.breakpoints.down("lg"));

  const { mode, systemMode } = useColorScheme();
  const resolvedMode = mode === "system" ? systemMode : mode;
  const prismTheme = resolvedMode === "light" ? lightCodeTheme : themes.nightOwl;
  const lightBg = resolvedMode === "light";

  const variables = React.useMemo(() => contract.variables ?? [], [contract.variables]);
  const outputs = React.useMemo(() => contract.outputs ?? [], [contract.outputs]);
  const warnings = contract.compatibility.warnings ?? [];
  const grade = contract.compatibility.grade;
  const activeCount = tab === 0 ? variables.length : outputs.length;

  React.useEffect(() => {
    const el = scrollRef.current;
    if (!el || compact) {
      setOverflows(false);
      return;
    }

    setOverflows(el.scrollHeight > el.clientHeight + 1);
  }, [tab, variables, outputs, expanded, compact]);

  return (
    <Box>
      <Box display="flex" alignItems="center" gap={1} flexWrap="wrap" mb={2}>
        <Tooltip title={GRADE_HELP[grade] ?? ""} placement="top">
          <Chip label={grade} size="small" color={GRADE_COLOR[grade] ?? "default"} variant="outlined" />
        </Tooltip>
        <Typography variant="caption" color="text.secondary">
          Derived from {contract.module.version.startsWith("v") ? contract.module.version : `v${contract.module.version}`}
        </Typography>
      </Box>

      {warnings.length > 0 && (
        <Alert severity="info" sx={{ mb: 2 }}>
          <Box component="ul" sx={{ m: 0, pl: 2 }}>
            {warnings.map((warning) => (
              <li key={warning}>
                <Typography variant="body2">{warning}</Typography>
              </li>
            ))}
          </Box>
        </Alert>
      )}

      <Tabs value={tab} onChange={(_, next: number) => setTab(next)} sx={{ mb: 2 }}>
        <Tab label={`Inputs (${variables.length})`} />
        <Tab label={`Outputs (${outputs.length})`} />
      </Tabs>

      {activeCount === 0 ? (
        <Typography variant="body2" color="text.secondary">
          {tab === 0 ? "This module declares no input variables." : "This module declares no outputs."}
        </Typography>
      ) : (
        <>
          {/* Desktop only. The scroll container carries the rounded outline. It already clips overflow, so the sticky header's blue fill stays inside the corners. */}
          <Box
            ref={scrollRef}
            sx={{
              display: { xs: "none", lg: "block" },
              maxHeight: expanded ? EXPANDED_HEIGHT : COLLAPSED_HEIGHT,
              overflowY: "auto",
              overflowX: "auto",
              border: 1,
              borderColor: "divider",
              borderRadius: 1,
            }}
          >
            {tab === 0 ? (
              <VariablesTable variables={variables} prismTheme={prismTheme} lightBg={lightBg} />
            ) : (
              <OutputsTable outputs={outputs} prismTheme={prismTheme} lightBg={lightBg} />
            )}
          </Box>
          <MobileRecordList>
            {tab === 0
              ? variables.map((v) => (
                  <VariableCard key={v.name} v={v} prismTheme={prismTheme} lightBg={lightBg} />
                ))
              : outputs.map((o) => (
                  <OutputCard key={o.name} o={o} prismTheme={prismTheme} lightBg={lightBg} />
                ))}
          </MobileRecordList>
        </>
      )}
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
