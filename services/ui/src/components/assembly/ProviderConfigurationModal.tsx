"use client";

import { useEffect, useMemo, useState } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import FormControl from "@mui/material/FormControl";
import IconButton from "@mui/material/IconButton";
import InputLabel from "@mui/material/InputLabel";
import MenuItem from "@mui/material/MenuItem";
import Select from "@mui/material/Select";
import TextField from "@mui/material/TextField";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import ChevronLeftIcon from "@mui/icons-material/ChevronLeft";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";
import CloseIcon from "@mui/icons-material/Close";
import CloseFullscreenIcon from "@mui/icons-material/CloseFullscreen";
import OpenInFullIcon from "@mui/icons-material/OpenInFull";
import SettingsInputComponentIcon from "@mui/icons-material/SettingsInputComponent";
import { useColorScheme } from "@mui/material/styles";
import { Highlight, type Language, themes } from "prism-react-renderer";
import Prism from "prismjs";
import "prismjs/components/prism-hcl";
import CopyButton from "@/components/CopyButton";
import type { ProviderSchemaBlock } from "@/lib/api";
import ProviderConfigurationEditor from "./ProviderConfigurationEditor";
import type { FieldValue, ProviderConfiguration, ReferenceOption } from "./types";
import { renderProviderPreview } from "./providerPreview";
import { extendHclGrammar } from "./hclConditionHighlight";

(typeof globalThis !== "undefined" ? globalThis : window).Prism = Prism;
extendHclGrammar(Prism.languages.hcl);

const PAGE_SIZE_OPTIONS = [5, 10, 20, 50] as const;
const DEFAULT_PAGE_SIZE = 10;

type Section = "configured" | "required" | "optional";

interface ConfigurationEntry {
  name: string;
  section: Section;
}

const SECTION_RANK: Record<Section, number> = { configured: 0, required: 1, optional: 2 };
const SECTION_LABEL: Record<Section, string> = { configured: "Configured", required: "Required", optional: "Optional" };

interface Props {
  open: boolean;
  onClose: () => void;
  localName: string;
  alias: string;
  schema: ProviderSchemaBlock;
  value: ProviderConfiguration;
  referenceOptions: ReferenceOption[];
  onChange: (value: ProviderConfiguration) => void;
}

function isFieldConfigured(field: FieldValue | undefined): boolean {
  if (!field) return false;
  return field.mode === "reference" ? !!field.refNodeId : field.literal.trim() !== "";
}

export default function ProviderConfigurationModal({
  open,
  onClose,
  localName,
  alias,
  schema,
  value,
  referenceOptions,
  onChange,
}: Props) {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState<number>(DEFAULT_PAGE_SIZE);
  const [search, setSearch] = useState("");
  const [previewExpanded, setPreviewExpanded] = useState(false);
  const { mode, systemMode } = useColorScheme();
  const resolvedMode = mode === "system" ? systemMode : mode;
  const prismTheme = resolvedMode === "light" ? themes.github : themes.nightOwl;
  const code = useMemo(
    () => renderProviderPreview(localName, alias, schema, value, referenceOptions),
    [localName, alias, schema, value, referenceOptions],
  );

  const entries = useMemo(() => {
    const items: ConfigurationEntry[] = [];
    for (const [name, attribute] of Object.entries(schema.attributes ?? {})) {
      if (!attribute.required && !attribute.optional) continue;
      items.push({
        name,
        section: isFieldConfigured(value.arguments[name]) ? "configured" : attribute.required ? "required" : "optional",
      });
    }
    for (const [name, block] of Object.entries(schema.blocks ?? {})) {
      items.push({
        name,
        section: (value.blocks[name]?.length ?? 0) > 0 ? "configured" : (block.minItems ?? 0) > 0 ? "required" : "optional",
      });
    }
    return items.sort((left, right) =>
      SECTION_RANK[left.section] - SECTION_RANK[right.section] || left.name.localeCompare(right.name),
    );
  }, [schema, value]);

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    return query ? entries.filter((entry) => entry.name.toLowerCase().includes(query)) : entries;
  }, [entries, search]);
  const pageCount = Math.max(1, Math.ceil(filtered.length / pageSize));
  const pageItems = filtered.slice((page - 1) * pageSize, page * pageSize);
  const configuredCount = entries.filter((entry) => entry.section === "configured").length;

  useEffect(() => {
    if (open) {
      setPage(1);
      setSearch("");
    }
  }, [open]);

  useEffect(() => setPage(1), [search, pageSize]);
  useEffect(() => {
    if (page > pageCount) setPage(pageCount);
  }, [page, pageCount]);

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
            <SettingsInputComponentIcon sx={{ fontSize: 20, color: "warning.main" }} />
          </Box>
          <Typography component="h2" variant="subtitle1" fontWeight={700} noWrap sx={{ flex: 1, minWidth: 0 }}>
            provider.{localName}{alias ? `.${alias}` : ""} — Configuration
          </Typography>
          <IconButton size="small" onClick={onClose} aria-label="Close">
            <CloseIcon fontSize="small" />
          </IconButton>
        </Box>
        <Typography variant="caption" color="text.secondary">
          {search.trim() ? `${filtered.length} of ${entries.length}` : entries.length} field
          {entries.length === 1 ? "" : "s"} · {configuredCount} configured
        </Typography>
        <Typography variant="caption" color="warning.main">
          Literal values are not saved in the browser. Use references to externally supplied values for credentials.
        </Typography>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          <TextField
            size="small"
            placeholder="Search configuration…"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            sx={{ flex: 1 }}
          />
          <FormControl size="small" sx={{ minWidth: 96 }}>
            <InputLabel id="assembly-provider-page-size">Per page</InputLabel>
            <Select
              labelId="assembly-provider-page-size"
              label="Per page"
              value={pageSize}
              onChange={(event) => setPageSize(Number(event.target.value))}
            >
              {PAGE_SIZE_OPTIONS.map((size) => <MenuItem key={size} value={size}>{size}</MenuItem>)}
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
        <Box sx={{ minHeight: 0, overflowY: "auto", display: "flex", flexDirection: "column", gap: 2, pr: 0.5 }}>
          {pageItems.length === 0 && (
            <Typography variant="body2" color="text.secondary">
              {entries.length === 0 ? "This provider has no configurable fields." : "No fields match your search."}
            </Typography>
          )}
          {pageItems.map((entry, index) => (
            <Box key={entry.name} sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
              {(index === 0 || pageItems[index - 1].section !== entry.section) && (
              <Typography
                variant="caption"
                fontWeight={700}
                color="text.secondary"
                sx={{ textTransform: "uppercase", letterSpacing: "0.05em" }}
              >
                {SECTION_LABEL[entry.section]}
              </Typography>
              )}
              <ProviderConfigurationEditor
                schema={schema}
                value={value}
                referenceOptions={referenceOptions}
                entryOrder={[entry.name]}
                onChange={onChange}
              />
            </Box>
          ))}
        </Box>
        <Box data-testid="provider-hcl-preview" sx={{ position: "relative", minHeight: 0, display: "flex", flexDirection: "column" }}>
          <Highlight prism={Prism as typeof Prism} theme={prismTheme} code={code} language={"hcl" as Language}>
            {({ style, tokens, getLineProps, getTokenProps }) => (
              <Box
                component="pre"
                aria-label="Provider HCL preview"
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
                {tokens.map((line, lineIndex) => (
                  <div key={lineIndex} {...getLineProps({ line })}>
                    {line.map((token, tokenIndex) => (
                      <span key={tokenIndex} {...getTokenProps({ token })} />
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
          <IconButton size="small" disabled={page <= 1} onClick={() => setPage((current) => Math.max(1, current - 1))} aria-label="Previous page">
            <ChevronLeftIcon fontSize="small" />
          </IconButton>
          <FormControl size="small" sx={{ minWidth: 84 }}>
            <InputLabel id="assembly-provider-page-jump">Page</InputLabel>
            <Select labelId="assembly-provider-page-jump" label="Page" value={page} onChange={(event) => setPage(Number(event.target.value))}>
              {Array.from({ length: pageCount }, (_, index) => index + 1).map((number) => (
                <MenuItem key={number} value={number}>{number}</MenuItem>
              ))}
            </Select>
          </FormControl>
          <Typography variant="caption" color="text.secondary">of {pageCount}</Typography>
          <IconButton size="small" disabled={page >= pageCount} onClick={() => setPage((current) => Math.min(pageCount, current + 1))} aria-label="Next page">
            <ChevronRightIcon fontSize="small" />
          </IconButton>
        </Box>
        <Button onClick={onClose}>Done</Button>
      </DialogActions>
    </Dialog>
  );
}