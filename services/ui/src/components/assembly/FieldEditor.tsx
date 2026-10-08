"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Link from "@mui/material/Link";
import MenuItem from "@mui/material/MenuItem";
import TextField from "@mui/material/TextField";
import Autocomplete from "@mui/material/Autocomplete";
import CodeEditor from "@uiw/react-textarea-code-editor";
import Popover from "@mui/material/Popover";
import ArrowDropDownIcon from "@mui/icons-material/ArrowDropDown";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";
import ToggleButton from "@mui/material/ToggleButton";
import Typography from "@mui/material/Typography";
import { useColorScheme } from "@mui/material/styles";
import type { Theme } from "@mui/material/styles";
import type { CtyType } from "@/lib/api";
import type { FieldValue, ReferenceOption } from "./types";
import { getHclFunctionHint, hclConditionPlugins, hclEditorColorVariables } from "./hclConditionHighlight";
import { monoSx } from "./editorRows";

const expressionSurfaceSx = {
  border: "1px solid",
  borderColor: "divider",
  borderRadius: 1,
  bgcolor: "action.hover",
  transition: "border-color 120ms ease",
  "&:focus-within": { borderColor: "primary.main" },
};

const expressionAutocompleteSx = {
  width: 160,
  "& .MuiOutlinedInput-root": {
    ...expressionSurfaceSx,
    minHeight: 32,
    px: 0.75,
    py: 0.25,
    "& fieldset": { border: 0 },
  },
  "& .MuiAutocomplete-input": { ...monoSx, lineHeight: 1.5, p: "0 !important" },
  "& .MuiAutocomplete-clearIndicator": { p: 0.25 },
};

const selectedReferenceOptionBackground = (theme: Theme) => {
  const primaryChannel = theme.vars?.palette.primary.mainChannel;
  return primaryChannel
    ? `rgba(${primaryChannel} / ${theme.palette.action.selectedOpacity})`
    : theme.palette.action.selected;
};

interface Props {
  /** The receiving variable's declared type — only used to offer true/false
   * options for bool fields. Pass "dynamic" when there's no specific type
   * (e.g. a for_each/count expression, which is a whole collection). */
  type: CtyType;
  value: FieldValue | undefined;
  referenceOptions: ReferenceOption[];
  /** Extra plain-text options offered ahead of cross-module references —
   * used for a repeated node's own `each.key`/`each.value`/`count.index`. */
  metaOptions?: string[];
  placeholder?: string;
  error?: string;
  multilineHcl?: boolean;
  showReferenceControl?: boolean;
  referenceOnly?: boolean;
  referenceOnlyFullWidth?: boolean;
  referenceOnlyCompact?: boolean;
  onChange: (value: FieldValue) => void;
}

function currentAutocompleteValue(
  field: FieldValue | undefined,
  options: ReferenceOption[],
): string | ReferenceOption {
  if (field?.mode === "reference") {
    const match = options.find((o) => o.nodeId === field.refNodeId && o.output === field.refOutput);
    if (match) {
      return match;
    }

    // The referenced module was renamed or removed since this reference was
    // made — keep it visible (rather than silently reverting to blank) so
    // the broken reference is obvious and the accompanying field error makes
    // sense.
    return {
      nodeId: field.refNodeId ?? "",
      instanceName: "?",
      output: field.refOutput ?? "?",
      type: "dynamic",
      label: `module.?.${field.refOutput ?? "?"}`,
      sourceMultiplicity: "none",
      sourceKind: "module",
    };
  }

  return field?.literal ?? "";
}

function outputCollectionKind(type: CtyType): "index" | "key" | undefined {
  if (!Array.isArray(type) || type.length < 2) return undefined;
  if (type[0] === "list" || type[0] === "set") return "index";
  if (type[0] === "map") return "key";
  return undefined;
}

function outputCollectionLabel(type: CtyType): "list" | "set" | "map" | undefined {
  if (!Array.isArray(type)) return undefined;
  if (type[0] === "list" || type[0] === "set" || type[0] === "map") return type[0];
  return undefined;
}

function collectionElementType(type: CtyType): CtyType | undefined {
  if (!Array.isArray(type) || type.length < 2) return undefined;
  return type[0] === "map" || type[0] === "list" || type[0] === "set" ? type[1] as CtyType : undefined;
}

function objectDescendantPaths(type: CtyType, prefix = ""): string[] {
  if (!Array.isArray(type) || type[0] !== "object" || !type[1] || typeof type[1] !== "object" || Array.isArray(type[1])) {
    return [];
  }

  return Object.entries(type[1] as Record<string, CtyType>).flatMap(([name, attributeType]) => {
    const path = prefix ? `${prefix}.${name}` : name;
    return [path, ...objectDescendantPaths(attributeType, path)];
  });
}

export function ReferenceSelectorControls({
  value,
  referenceOptions,
  leadingControl,
  compact = false,
  onChange,
}: {
  value: FieldValue | undefined;
  referenceOptions: ReferenceOption[];
  leadingControl?: React.ReactNode;
  compact?: boolean;
  onChange: (value: FieldValue) => void;
}) {
  const matchedOption = value?.mode === "reference"
    ? referenceOptions.find((option) => option.nodeId === value.refNodeId && option.output === value.refOutput)
    : undefined;
  if (!matchedOption || value?.mode !== "reference") return null;

  const isRepeatedSource = matchedOption.sourceMultiplicity !== "none";
  const selectorKind = value.refSelector?.kind;
  const outputSelectorKind = outputCollectionKind(matchedOption.type);
  const outputLabel = outputCollectionLabel(matchedOption.type);
  const selectedElementType = value.refOutputSelector && value.refOutputSelector.kind !== "all"
    ? collectionElementType(matchedOption.type)
    : undefined;
  const descendantPaths = selectedElementType ? objectDescendantPaths(selectedElementType) : [];
  const [pointerActivated, setPointerActivated] = React.useState(false);

  return (
    <>
      {isRepeatedSource && (
        <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
          <Typography variant="caption" color="text.secondary">
            {matchedOption.instanceName} is repeated —
          </Typography>
          <ToggleButtonGroup
            size="small"
            exclusive
            value={!value.refSelector || value.refSelector.kind === "all" ? "all" : "one"}
            onChange={(_e, next) => {
              if (!next) return;
              if (next === "all") onChange({ ...value, refSelector: { kind: "all" } });
              else {
                const kind = matchedOption.sourceMultiplicity === "for_each" ? "key" : "index";
                onChange({ ...value, refSelector: { kind, expr: { mode: "literal", literal: "" } } });
              }
            }}
          >
            <ToggleButton value="all" sx={{ fontSize: "0.65rem", py: 0.25, px: 1 }}>All instances</ToggleButton>
            <ToggleButton value="one" sx={{ fontSize: "0.65rem", py: 0.25, px: 1 }}>One instance</ToggleButton>
          </ToggleButtonGroup>
          {value.refSelector && value.refSelector.kind !== "all" && (
            <Autocomplete
              freeSolo
              size="small"
              options={value.refSelector.kind === "key" ? (matchedOption.forEachKeys ?? []) : []}
              value={value.refSelector.expr.literal}
              onChange={(_event, newValue) => {
                const selector = value.refSelector as { kind: "index" | "key"; expr: FieldValue };
                onChange({ ...value, refSelector: { kind: selector.kind, expr: { mode: "literal", literal: newValue ?? "" } } });
              }}
              onInputChange={(_event, newInputValue, reason) => {
                if (reason === "input") {
                  const selector = value.refSelector as { kind: "index" | "key"; expr: FieldValue };
                  onChange({ ...value, refSelector: { kind: selector.kind, expr: { mode: "literal", literal: newInputValue } } });
                }
              }}
              sx={expressionAutocompleteSx}
              renderInput={(params) => <TextField {...params} placeholder={selectorKind === "key" ? "key expression" : "index expression"} />}
            />
          )}
        </Box>
      )}
      {outputSelectorKind && (
        <Box
          onKeyDownCapture={(event) => {
            if (event.key !== "Escape") setPointerActivated(false);
          }}
          sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}
        >
          {leadingControl}
          <Typography variant="caption" color="text.secondary">
            {matchedOption.output} is a {outputLabel} —
          </Typography>
          <ToggleButtonGroup
            size="small"
            exclusive
            value={!value.refOutputSelector || value.refOutputSelector.kind === "all" ? "all" : "one"}
            onChange={(_e, next) => {
              if (!next) return;
              if (next === "all") onChange({ ...value, refOutputSelector: { kind: "all" }, refAttributePath: undefined });
              else onChange({ ...value, refOutputSelector: { kind: outputSelectorKind, expr: { mode: "literal", literal: "" } } });
            }}
          >
            <ToggleButton value="all" sx={{ fontSize: "0.65rem", py: 0.25, px: 1 }}>All instances</ToggleButton>
            <ToggleButton value="one" sx={{ fontSize: "0.65rem", py: 0.25, px: 1 }}>One instance</ToggleButton>
          </ToggleButtonGroup>
          {value.refOutputSelector && value.refOutputSelector.kind !== "all" && (
            <Autocomplete
              freeSolo
              size="small"
              options={[]}
              value={value.refOutputSelector.expr.literal}
              onChange={(_event, newValue) => {
                const selector = value.refOutputSelector as { kind: "index" | "key"; expr: FieldValue };
                onChange({ ...value, refOutputSelector: { kind: selector.kind, expr: { mode: "literal", literal: newValue ?? "" } } });
              }}
              onInputChange={(_event, newInputValue, reason) => {
                if (reason === "input") {
                  const selector = value.refOutputSelector as { kind: "index" | "key"; expr: FieldValue };
                  onChange({ ...value, refOutputSelector: { kind: selector.kind, expr: { mode: "literal", literal: newInputValue } } });
                }
              }}
              sx={expressionAutocompleteSx}
              renderInput={(params) => <TextField {...params} placeholder={outputSelectorKind === "key" ? "key expression" : "index expression"} />}
            />
          )}
          {descendantPaths.length > 0 && (
            <TextField
              select
              size="small"
              value={value.refAttributePath ?? ""}
              onMouseDown={() => setPointerActivated(true)}
              onBlur={() => setPointerActivated(false)}
              onChange={(event) => onChange({ ...value, refAttributePath: event.target.value || undefined })}
              inputProps={{ "aria-label": "Select descendant" }}
              SelectProps={{
                displayEmpty: true,
                renderValue: (selected) => (
                  <Chip
                    size="small"
                    color="primary"
                    label={(
                      <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.25 }}>
                        {String(selected) || "Whole value"}
                        <ArrowDropDownIcon aria-hidden="true" sx={{ fontSize: 18 }} />
                      </Box>
                    )}
                    sx={{ height: compact ? 18 : 22, fontSize: compact ? "0.65rem" : "0.7rem" }}
                  />
                ),
              }}
              sx={{
                width: "fit-content",
                "& .MuiOutlinedInput-root": {
                  p: 0,
                  border: 0,
                  bgcolor: "transparent",
                  "& .MuiOutlinedInput-notchedOutline": { border: "0 !important" },
                  "&.Mui-focused": { bgcolor: "transparent" },
                },
                "& .MuiSelect-select": { p: "0 !important", display: "flex", alignItems: "center" },
                "& .MuiSelect-icon": { display: "none" },
                ...(!pointerActivated && {
                  "& .MuiSelect-select:focus-visible .MuiChip-root": {
                  outline: "2px solid",
                  outlineColor: "primary.main",
                  outlineOffset: 1,
                  },
                }),
              }}
            >
              <MenuItem value="">Whole value</MenuItem>
              {descendantPaths.map((path) => <MenuItem key={path} value={path}>{path}</MenuItem>)}
            </TextField>
          )}
        </Box>
      )}
    </>
  );
}

/**
 * A single input field editor: type a literal value directly, or pick
 * another module's output to wire it as a reference — picking a reference
 * IS the connect gesture, there is no separate drag-to-wire action. When the
 * picked reference is a repeated module (count/for_each), a follow-up
 * control appears to choose the whole collection vs. one instance.
 */
export default function FieldEditor({
  type,
  value,
  referenceOptions,
  metaOptions = [],
  placeholder,
  error,
  multilineHcl = false,
  showReferenceControl = true,
  referenceOnly = false,
  referenceOnlyFullWidth = true,
  referenceOnlyCompact = false,
  onChange,
}: Props) {
  const [referenceAnchorEl, setReferenceAnchorEl] = React.useState<HTMLElement | null>(null);
  const [referenceSearch, setReferenceSearch] = React.useState("");
  const [hclCaretPosition, setHclCaretPosition] = React.useState<number | null>(null);
  const [hclEditorFocused, setHclEditorFocused] = React.useState(false);
  const { mode, systemMode } = useColorScheme();
  const editorMode = (mode === "system" ? systemMode : mode) === "light" ? "light" : "dark";
  const currentValue = currentAutocompleteValue(value, referenceOptions);
  const hclLiteral = value?.mode === "literal" ? value.literal : "";
  const activeFunctionHint = hclEditorFocused && hclCaretPosition !== null
    ? getHclFunctionHint(hclLiteral, hclCaretPosition)
    : undefined;
  const options: Array<string | ReferenceOption> = [
    ...(type === "bool" ? ["true", "false"] : []),
    ...metaOptions,
    ...referenceOptions,
  ];

  const pickerLiterals = [...(type === "bool" ? ["true", "false"] : []), ...metaOptions];
  const pickerOptions: Array<string | ReferenceOption> = [...pickerLiterals, ...referenceOptions];
  const codePickerValue = value?.mode === "reference"
    ? currentValue
    : typeof currentValue === "string" && pickerLiterals.includes(currentValue)
      ? currentValue
      : null;
  const selectedReference = value?.mode === "reference" && typeof currentValue !== "string" ? currentValue : undefined;
  const changeFromOption = (newValue: string | ReferenceOption | null) => {
    if (newValue && typeof newValue !== "string") {
      onChange({
        mode: "reference",
        literal: "",
        refNodeId: newValue.nodeId,
        refOutput: newValue.output,
        refSelector: newValue.sourceMultiplicity !== "none" ? { kind: "all" } : undefined,
        refOutputSelector: outputCollectionKind(newValue.type) ? { kind: "all" } : undefined,
      });
    } else {
      onChange({ mode: "literal", literal: newValue ?? "" });
    }
  };
  const moveCollectionChip = referenceOnly && !referenceOnlyCompact && value?.mode === "reference" && value.refOutputSelector !== undefined;
  const referenceChip = (
    <Chip
      size="small"
      label={selectedReference?.label ?? "Use input"}
      color="primary"
      variant={selectedReference ? "filled" : "outlined"}
      onClick={(event) => {
        setReferenceSearch("");
        setReferenceAnchorEl(event.currentTarget);
      }}
      onDelete={selectedReference ? () => changeFromOption(null) : undefined}
      aria-label={selectedReference ? `Module output ${selectedReference.label}` : "Use input"}
      aria-pressed={!!selectedReference}
      sx={{
        flexShrink: 0,
        height: referenceOnlyCompact ? 18 : 22,
        maxWidth: { xs: 112, sm: 160 },
        fontSize: referenceOnlyCompact ? "0.65rem" : "0.7rem",
        ".MuiChip-label": { overflow: "hidden", textOverflow: "ellipsis" },
        ...(referenceOnlyCompact && { ".MuiChip-deleteIcon": { fontSize: 14, mr: 0.5 } }),
      }}
    />
  );

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5, width: referenceOnly && referenceOnlyFullWidth ? "100%" : undefined }}>
      {(multilineHcl || referenceOnly) && (
        <Box
          sx={{
            display: "flex",
            alignItems: referenceOnly ? "center" : "flex-start",
            justifyContent: referenceOnly ? "flex-end" : undefined,
            gap: 1,
            minWidth: 0,
          }}
        >
          {multilineHcl && (
            <Box
              onBlur={(event) => {
                if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setHclEditorFocused(false);
              }}
              sx={{
                flex: "1 1 auto",
                minWidth: 0,
                display: "flex",
                flexDirection: "column",
                gap: 0.5,
              }}
            >
              <Box
                sx={{
                  width: "100%",
                  minWidth: 0,
                px: 0.75,
                py: 0.25,
                ...expressionSurfaceSx,
                borderColor: error ? "error.main" : "divider",
                "&:focus-within": { borderColor: error ? "error.main" : "primary.main" },
                overflow: "hidden",
                "& .w-tc-editor": { ...hclEditorColorVariables(editorMode) },
              }}
            >
              <CodeEditor
                value={value?.mode === "literal" ? value.literal : ""}
                language="hcl"
                rehypePlugins={hclConditionPlugins}
                data-color-mode={editorMode}
                minHeight={20}
                padding={0}
                indentWidth={2}
                placeholder={placeholder ?? "Value or HCL expression"}
                onChange={(event) => {
                  setHclCaretPosition(event.currentTarget.selectionStart);
                  onChange({ mode: "literal", literal: event.target.value });
                }}
                onSelect={(event) => setHclCaretPosition(event.currentTarget.selectionStart)}
                onKeyUp={(event) => setHclCaretPosition(event.currentTarget.selectionStart)}
                onClick={(event) => setHclCaretPosition(event.currentTarget.selectionStart)}
                onFocus={(event) => {
                  setHclEditorFocused(true);
                  setHclCaretPosition(event.currentTarget.selectionStart);
                }}
                aria-label="HCL value"
                aria-invalid={!!error}
                style={{ ...monoSx, width: "100%", backgroundColor: "transparent" }}
              />
              </Box>
              {activeFunctionHint && (
                <Box sx={{ display: "flex", alignItems: "baseline", gap: 0.75, pl: 0.75, flexWrap: "wrap" }}>
                  <Typography variant="caption" color="text.secondary">
                    <Box component="code" sx={{ color: "text.primary", fontWeight: 600 }}>{activeFunctionHint.name}()</Box>
                    {" "}{activeFunctionHint.hint}
                  </Typography>
                  <Link href={activeFunctionHint.docsUrl} target="_blank" rel="noreferrer" variant="caption">
                    OpenTofu docs
                  </Link>
                </Box>
              )}
            </Box>
          )}
          {pickerOptions.length > 0 && showReferenceControl && !moveCollectionChip && referenceChip}
        </Box>
      )}
      {multilineHcl || referenceOnly ? (
        pickerOptions.length > 0 && (
          <Popover
            open={Boolean(referenceAnchorEl)}
            anchorEl={referenceAnchorEl}
            onClose={() => setReferenceAnchorEl(null)}
            anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
            transformOrigin={{ vertical: "top", horizontal: "right" }}
            slotProps={{
              paper: {
                sx: {
                  p: 0,
                  width: 320,
                  maxWidth: "calc(100vw - 32px)",
                  bgcolor: "background.paper",
                  boxShadow: 8,
                  borderRadius: 1,
                  overflow: "hidden",
                },
              },
            }}
          >
            <Autocomplete
              autoFocus
              open={Boolean(referenceAnchorEl)}
              openOnFocus
              disablePortal
              size="small"
              options={pickerOptions}
              value={codePickerValue}
              inputValue={referenceSearch}
              getOptionLabel={(option) => (typeof option === "string" ? option : option.label)}
              isOptionEqualToValue={(option, selected) =>
                typeof option === "string" || typeof selected === "string"
                  ? option === selected
                  : option.nodeId === selected.nodeId && option.output === selected.output
              }
              onChange={(_event, selected) => {
                changeFromOption(selected);
                setReferenceAnchorEl(null);
              }}
              onInputChange={(_event, newInputValue, reason) => {
                if (reason === "input" || reason === "clear") setReferenceSearch(newInputValue);
              }}
              slotProps={{
                popper: { sx: { position: "static !important", transform: "none !important", width: "100% !important" } },
                paper: { sx: { boxShadow: "none", bgcolor: "transparent" } },
                listbox: {
                  sx: {
                    "& .MuiAutocomplete-option[aria-selected='true']": {
                      bgcolor: selectedReferenceOptionBackground,
                      "&.Mui-focused, &:hover": { bgcolor: selectedReferenceOptionBackground },
                    },
                  },
                },
              }}
              renderInput={(params) => (
                <TextField
                  {...params}
                  autoFocus
                  variant="standard"
                  placeholder="Search module outputs"
                  sx={{
                    mx: 2,
                    mt: 1.25,
                    mb: 0.5,
                    width: "calc(100% - 32px)",
                    "& .MuiInput-root:before": { borderBottomColor: "divider" },
                    "& .MuiInput-root:hover:not(.Mui-disabled, .Mui-error):before": { borderBottomColor: "text.secondary" },
                    "& .MuiInput-root:after": { borderBottomColor: "primary.main" },
                  }}
                />
              )}
            />
          </Popover>
        )
      ) : (
        <Autocomplete
          freeSolo
          size="small"
          options={options}
          value={currentValue}
          getOptionLabel={(option) => (typeof option === "string" ? option : option.label)}
          isOptionEqualToValue={(option, selected) =>
            typeof option === "string" || typeof selected === "string"
              ? option === selected
              : option.nodeId === selected.nodeId && option.output === selected.output
          }
          onChange={(_event, selected) => changeFromOption(selected)}
          onInputChange={(_event, newInputValue, reason) => {
            if (reason === "input") {
              onChange({ mode: "literal", literal: newInputValue });
            }
          }}
          renderInput={(params) => <TextField {...params} placeholder={placeholder} error={!!error} helperText={error} />}
        />
      )}
      {(multilineHcl || referenceOnly) && error && <Typography variant="caption" color="error">{error}</Typography>}
      {!referenceOnlyCompact && showReferenceControl && (
        <ReferenceSelectorControls
          value={value}
          referenceOptions={referenceOptions}
          compact={referenceOnlyCompact}
          leadingControl={moveCollectionChip ? referenceChip : undefined}
          onChange={onChange}
        />
      )}
    </Box>
  );
}
