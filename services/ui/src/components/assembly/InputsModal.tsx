"use client";

import * as React from "react";
import { useEffect, useMemo, useState } from "react";
import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import DialogActions from "@mui/material/DialogActions";
import IconButton from "@mui/material/IconButton";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Chip from "@mui/material/Chip";
import Tooltip from "@mui/material/Tooltip";
import TextField from "@mui/material/TextField";
import FormControl from "@mui/material/FormControl";
import InputLabel from "@mui/material/InputLabel";
import Select from "@mui/material/Select";
import MenuItem from "@mui/material/MenuItem";
import Button from "@mui/material/Button";
import CloseIcon from "@mui/icons-material/Close";
import CloseFullscreenIcon from "@mui/icons-material/CloseFullscreen";
import ChevronLeftIcon from "@mui/icons-material/ChevronLeft";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";
import OpenInFullIcon from "@mui/icons-material/OpenInFull";
import WidgetsIcon from "@mui/icons-material/Widgets";
import WidgetsOutlinedIcon from "@mui/icons-material/WidgetsOutlined";
import { useColorScheme } from "@mui/material/styles";
import { Highlight, type Language, themes } from "prism-react-renderer";
import Prism from "prismjs";
import "prismjs/components/prism-hcl";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { ContractVariable } from "@/lib/api";
import { renderTypeCompact } from "@/lib/ctyType";
import CopyButton from "@/components/CopyButton";
import { ReferenceSelectorControls } from "./FieldEditor";
import FieldEditor from "./FieldEditor";
import ModuleInputEditor, { ComplexInputReferenceControl, isComplexType } from "./ModuleInputEditor";
import { typeSpecFromCtyType } from "./typeSpec";
import { extendHclGrammar } from "./hclConditionHighlight";
import { renderModulePreview } from "./modulePreview";
import { moduleInputPath, type FieldValue, type ModuleInputValue, type Multiplicity, type ProviderBinding, type ProviderOption, type ReferenceOption, type VariableOption } from "./types";

(typeof globalThis !== "undefined" ? globalThis : window).Prism = Prism;
extendHclGrammar(Prism.languages.hcl);

const PAGE_SIZE_OPTIONS = [5, 10, 20, 50] as const;
const DEFAULT_PAGE_SIZE = 10;

type Section = "provided" | "required" | "optional";

const SECTION_RANK: Record<Section, number> = { provided: 0, required: 1, optional: 2 };
const SECTION_LABEL: Record<Section, string> = { provided: "Provided", required: "Required", optional: "Optional" };

/** Whether an input has a selected reference, a non-blank scalar, or entries. */
function isInputProvided(value: ModuleInputValue | undefined): boolean {
  if (!value) return false;
  if (value.kind === "scalar") {
    return value.value.mode === "reference" ? !!value.value.refNodeId : value.value.literal.trim() !== "";
  }
  if (value.kind === "list") return value.items.length > 0;
  return value.entries.length > 0;
}

function sectionOf(value: ModuleInputValue | undefined, variable: ContractVariable): Section {
  if (isInputProvided(value)) return "provided";
  return variable.required ? "required" : "optional";
}

function sectionField(value: ModuleInputValue | undefined): FieldValue | undefined {
  return value?.kind === "scalar" ? value.value : undefined;
}

function configuredRegistryHost(): string {
  const baseUrl = process.env.NEXT_PUBLIC_BASE_URL;
  if (!baseUrl) return "your-opendepot-host";
  try {
    return new URL(baseUrl).host;
  } catch {
    return baseUrl;
  }
}

/** Renders a variable's description as markdown (module authors document inputs with
 * inline code, links, and lists) rather than plain text, mirroring ContractTable's
 * MarkdownDescription but without its table-cell collapse/expand behavior — these
 * descriptions are short enough to just render inline in the modal. */
function MarkdownDescription({ text }: { text: string }) {
  return (
    <Box
      sx={{
        fontSize: "0.75rem",
        color: "text.secondary",
        mb: 0.5,
        "& p": { m: 0 },
        "& p:not(:last-child)": { mb: 0.5 },
        "& ul, & ol": { m: 0, pl: 2.5 },
        "& li": { mb: 0.25 },
        "& a": { color: "primary.main", textDecoration: "none" },
        "& a:hover": { textDecoration: "underline" },
        "& code": {
          fontFamily: "monospace",
          fontSize: "0.6875rem",
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
  );
}

interface Props {
  open: boolean;
  onClose: () => void;
  instanceName: string;
  namespace: string;
  moduleName: string;
  system?: string;
  version: string;
  multiplicity: Multiplicity;
  variableOptions: VariableOption[];
  providerBindings: Record<string, ProviderBinding>;
  providerOptions: ProviderOption[];
  variables: ContractVariable[];
  values: Record<string, ModuleInputValue>;
  optionalFieldVisibility: Record<string, boolean>;
  fieldErrors: Record<string, string>;
  referenceOptions: ReferenceOption[];
  /** This node's own each.key/each.value/count.index options, if repeated. */
  metaOptions?: string[];
  onFieldChange: (variableName: string, value: ModuleInputValue) => void;
  onOptionalFieldVisibilityChange: (path: string, visible: boolean) => void;
}

/**
 * A per-module form for filling in every input, opened from the node's
 * "Inputs" summary row rather than rendered inline — large modules
 * (30-50+ variables) would otherwise force every node to an unusable height.
 * Inputs are grouped Provided -> Required -> Optional, so anything the user
 * has already filled in is easy to find again rather than being buried
 * amongst everything still outstanding; the combined list is then paginated.
 */
export default function InputsModal({
  open,
  onClose,
  instanceName,
  namespace,
  moduleName,
  system,
  version,
  multiplicity,
  variableOptions,
  providerBindings,
  providerOptions,
  variables,
  values,
  optionalFieldVisibility,
  fieldErrors,
  referenceOptions,
  metaOptions = [],
  onFieldChange,
  onOptionalFieldVisibilityChange,
}: Props) {
  const { mode, systemMode } = useColorScheme();
  const resolvedMode = mode === "system" ? systemMode : mode;
  const ModuleIcon = resolvedMode === "light" ? WidgetsIcon : WidgetsOutlinedIcon;
  const prismTheme = resolvedMode === "light" ? themes.github : themes.nightOwl;
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState<number>(DEFAULT_PAGE_SIZE);
  const [search, setSearch] = useState("");
  const [previewExpanded, setPreviewExpanded] = useState(false);

  // Already-provided inputs float to the top, then required, then optional —
  // a stable sort so ties within a section keep the contract's own order.
  const sorted = useMemo(
    () =>
      [...variables].sort(
        (a, b) => SECTION_RANK[sectionOf(values[a.name], a)] - SECTION_RANK[sectionOf(values[b.name], b)],
      ),
    [variables, values],
  );

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return sorted;

    return sorted.filter(
      (v) => v.name.toLowerCase().includes(q) || (v.description ?? "").toLowerCase().includes(q),
    );
  }, [sorted, search]);

  const pageCount = Math.max(1, Math.ceil(filtered.length / pageSize));

  useEffect(() => {
    if (open) {
      setPage(1);
      setSearch("");
    }
  }, [open]);

  useEffect(() => {
    setPage(1);
  }, [search, pageSize]);

  useEffect(() => {
    if (page > pageCount) {
      setPage(pageCount);
    }
  }, [page, pageCount]);

  const pageItems = filtered.slice((page - 1) * pageSize, page * pageSize);
  const errorCount = Object.keys(fieldErrors).length;
  const previewInputs = useMemo(
    () => variables.flatMap((variable) => {
      const type = typeSpecFromCtyType(variable.type, variable.optionalAttributePaths, variable.optionalAttributes);
      return type ? [{ name: variable.name, type, value: values[variable.name] }] : [];
    }),
    [variables, values],
  );
  const code = useMemo(
    () => renderModulePreview({
      instanceName,
      namespace,
      moduleName,
      system,
      version,
      registryHost: configuredRegistryHost(),
      inputs: previewInputs,
      referenceOptions,
      multiplicity,
      variableOptions,
      providerBindings,
      providerOptions,
      optionalFieldVisibility,
    }),
    [instanceName, namespace, moduleName, system, version, previewInputs, referenceOptions, multiplicity, variableOptions, providerBindings, providerOptions, optionalFieldVisibility],
  );

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="lg"
      fullWidth
      slotProps={{ paper: { sx: { height: { xs: "calc(100vh - 32px)", sm: "min(860px, calc(100vh - 64px))" } } }}}
    >
      <DialogTitle component="div" sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          <Box sx={{ width: 24, height: 24, display: "grid", placeItems: "center", flexShrink: 0 }}>
            <ModuleIcon sx={{ fontSize: 20, color: "#03deb8" }} />
          </Box>
          <Typography component="h2" variant="subtitle1" fontWeight={700} noWrap sx={{ flex: 1, minWidth: 0 }}>
            module.{instanceName} — Inputs
          </Typography>
          <IconButton size="small" onClick={onClose} aria-label="Close">
            <CloseIcon fontSize="small" />
          </IconButton>
        </Box>
        <Typography variant="caption" color="text.secondary">
          {search.trim() ? `${filtered.length} of ${variables.length}` : variables.length} input
          {variables.length === 1 ? "" : "s"}
          {errorCount > 0 ? ` · ${errorCount} need${errorCount === 1 ? "s" : ""} attention` : ""}
        </Typography>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          <TextField
            size="small"
            placeholder="Search inputs…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            sx={{ flex: 1 }}
          />
          <FormControl size="small" sx={{ minWidth: 96 }}>
            <InputLabel id="assembly-inputs-page-size">Per page</InputLabel>
            <Select
              labelId="assembly-inputs-page-size"
              label="Per page"
              value={pageSize}
              onChange={(e) => setPageSize(Number(e.target.value))}
            >
              {PAGE_SIZE_OPTIONS.map((n) => (
                <MenuItem key={n} value={n}>
                  {n}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        </Box>
      </DialogTitle>
      <DialogContent
        dividers
        sx={{
          minHeight: 0,
          overflow: "hidden",
          display: "grid",
          gridTemplateColumns: {
            xs: "minmax(0, 1fr)",
            md: previewExpanded ? "minmax(0, 1fr) minmax(320px, 2fr)" : "minmax(0, 3fr) minmax(320px, 2fr)",
          },
          gridTemplateRows: {
            xs: previewExpanded ? "minmax(120px, 1fr) minmax(0, 2fr)" : "minmax(0, 1fr) minmax(180px, 32%)",
            md: "minmax(0, 1fr)",
          },
          gap: 1.5,
        }}
      >
        <Box sx={{ minHeight: 0, overflowY: "auto", display: "flex", flexDirection: "column", gap: 1.5, pr: 0.5 }}>
          {pageItems.length === 0 && (
            <Typography variant="body2" color="text.secondary">
              {variables.length === 0 ? "This module has no inputs." : "No inputs match your search."}
            </Typography>
          )}
          {pageItems.map((v, idx) => {
            const fieldError = fieldErrors[v.name];
            const inputType = typeSpecFromCtyType(v.type, v.optionalAttributePaths, v.optionalAttributes);
            const inputValue = values[v.name];
            const inputReference = inputValue?.kind === "scalar" ? inputValue.value : undefined;
            const scalarValue = sectionField(inputValue);
            const moveReferenceToCollectionControls = inputReference?.mode === "reference" && inputReference.refOutputSelector !== undefined;
            const section = sectionOf(values[v.name], v);
            const showSectionHeader = idx === 0 || sectionOf(values[pageItems[idx - 1].name], pageItems[idx - 1]) !== section;

            return (
              <React.Fragment key={v.name}>
                {showSectionHeader && (
                  <Typography
                    variant="caption"
                    fontWeight={700}
                    color="text.secondary"
                    sx={{ display: "block", textTransform: "uppercase", letterSpacing: "0.05em" }}
                  >
                    {SECTION_LABEL[section]}
                  </Typography>
                )}
                <Box>
                  <Box sx={{ display: "flex", alignItems: "flex-start", gap: 0.75, mb: 0.5 }}>
                    <Typography variant="body2" fontWeight={600}>
                      {v.name}
                    </Typography>
                    {v.required ? (
                      <Chip label="required" size="small" color="error" variant="outlined" sx={{ height: 18, fontSize: "0.65rem" }} />
                    ) : (
                      <Chip label="optional" size="small" variant="outlined" sx={{ height: 18, fontSize: "0.65rem" }} />
                    )}
                    <Box sx={{ display: "flex", alignItems: "flex-start", gap: 0.75, ml: "auto", minWidth: 0 }}>
                      {!moveReferenceToCollectionControls && inputType && isComplexType(inputType) ? (
                        <ComplexInputReferenceControl
                          type={inputType}
                          value={values[v.name]}
                          referenceOptions={referenceOptions}
                          fullWidth={false}
                          compact
                          onChange={(value) => onFieldChange(v.name, value)}
                        />
                      ) : !moveReferenceToCollectionControls ? (
                        <FieldEditor
                          type={v.type}
                          value={scalarValue}
                          referenceOptions={referenceOptions}
                          metaOptions={metaOptions}
                          referenceOnly
                          referenceOnlyFullWidth={false}
                          referenceOnlyCompact
                          onChange={(value) => onFieldChange(v.name, { kind: "scalar", value })}
                        />
                      ) : null}
                      <Chip
                        label={renderTypeCompact(v.type)}
                        size="small"
                        variant="outlined"
                        sx={{ height: 18, fontSize: "0.65rem", flexShrink: 0 }}
                      />
                    </Box>
                  </Box>
                  {v.description && <MarkdownDescription text={v.description} />}
                  {inputReference?.mode === "reference" && (
                    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5, mb: 0.5 }}>
                      <ReferenceSelectorControls
                        value={inputReference}
                        referenceOptions={referenceOptions}
                        compact
                        leadingControl={moveReferenceToCollectionControls ? (
                          <FieldEditor
                            type={v.type}
                            value={inputReference}
                            referenceOptions={referenceOptions}
                            metaOptions={metaOptions}
                            referenceOnly
                            referenceOnlyFullWidth={false}
                            referenceOnlyCompact
                            onChange={(value) => onFieldChange(v.name, { kind: "scalar", value })}
                          />
                        ) : undefined}
                        onChange={(value) => onFieldChange(v.name, { kind: "scalar", value })}
                      />
                    </Box>
                  )}
                  {(() => {
                    const scalarValue = sectionField(values[v.name]);
                    return inputType ? (
                      <ModuleInputEditor
                        type={inputType}
                        value={values[v.name]}
                        referenceOptions={referenceOptions}
                        metaOptions={metaOptions}
                        showReferenceControl={false}
                        referenceControlPlacement="header"
                        path={moduleInputPath("", `variable:${v.name}`)}
                        optionalFieldVisibility={optionalFieldVisibility}
                        onOptionalFieldVisibilityChange={onOptionalFieldVisibilityChange}
                        error={fieldError}
                        onChange={(value) => onFieldChange(v.name, value)}
                      />
                    ) : (
                      <FieldEditor
                        type={v.type}
                        value={scalarValue}
                        referenceOptions={referenceOptions}
                        metaOptions={metaOptions}
                        multilineHcl
                        showReferenceControl={false}
                        placeholder={v.default !== undefined ? `default: ${JSON.stringify(v.default)}` : "value or module.…"}
                        error={fieldError}
                        onChange={(value) => onFieldChange(v.name, { kind: "scalar", value })}
                      />
                    );
                  })()}
                </Box>
              </React.Fragment>
            );
          })}
        </Box>
        <Box data-testid="module-hcl-preview" sx={{ position: "relative", minHeight: 0, display: "flex", flexDirection: "column" }}>
          <Highlight prism={Prism as typeof Prism} theme={prismTheme} code={code} language={"hcl" as Language}>
            {({ style, tokens, getLineProps, getTokenProps }) => (
              <Box
                component="pre"
                aria-label="Module HCL preview"
                sx={{
                  m: 0,
                  pl: 1,
                  pr: 9,
                  py: 0.75,
                  minHeight: 0,
                  flex: 1,
                  borderRadius: 1,
                  border: "1px solid",
                  borderColor: "divider",
                  fontFamily: "monospace",
                  fontSize: "0.75rem",
                  lineHeight: 1.5,
                  overflow: "auto",
                  whiteSpace: "pre",
                  ...style,
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
          <Box sx={{ position: "absolute", top: 6, right: 6, display: "flex", alignItems: "center", gap: 0.25 }}>
            <Tooltip title={previewExpanded ? "Restore input pane space" : "Expand HCL preview"}>
              <IconButton
                size="small"
                onClick={() => setPreviewExpanded((expanded) => !expanded)}
                aria-label={previewExpanded ? "Restore HCL preview" : "Expand HCL preview"}
                aria-pressed={previewExpanded}
                sx={{ color: "text.secondary", transition: "color 0.2s", p: 0.4 }}
              >
                {previewExpanded ? <CloseFullscreenIcon sx={{ fontSize: 14 }} /> : <OpenInFullIcon sx={{ fontSize: 14 }} />}
              </IconButton>
            </Tooltip>
            <CopyButton value={code} />
          </Box>
        </Box>
      </DialogContent>
      <DialogActions sx={{ justifyContent: "space-between", px: 3, flexWrap: "wrap", gap: 1 }}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          <IconButton size="small" disabled={page <= 1} onClick={() => setPage((p) => Math.max(1, p - 1))} aria-label="Previous page">
            <ChevronLeftIcon fontSize="small" />
          </IconButton>
          <FormControl size="small" sx={{ minWidth: 84 }}>
            <InputLabel id="assembly-inputs-page-jump">Page</InputLabel>
            <Select
              labelId="assembly-inputs-page-jump"
              label="Page"
              value={page}
              onChange={(e) => setPage(Number(e.target.value))}
            >
              {Array.from({ length: pageCount }, (_, i) => i + 1).map((n) => (
                <MenuItem key={n} value={n}>
                  {n}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
          <Typography variant="caption" color="text.secondary">
            of {pageCount}
          </Typography>
          <IconButton
            size="small"
            disabled={page >= pageCount}
            onClick={() => setPage((p) => Math.min(pageCount, p + 1))}
            aria-label="Next page"
          >
            <ChevronRightIcon fontSize="small" />
          </IconButton>
        </Box>

        <Button onClick={onClose}>Done</Button>
      </DialogActions>
    </Dialog>
  );
}
