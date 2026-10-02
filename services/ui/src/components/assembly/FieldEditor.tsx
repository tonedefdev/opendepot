"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import TextField from "@mui/material/TextField";
import Autocomplete from "@mui/material/Autocomplete";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";
import ToggleButton from "@mui/material/ToggleButton";
import Typography from "@mui/material/Typography";
import type { CtyType } from "@/lib/api";
import type { FieldValue, ReferenceOption } from "./types";

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

/**
 * A single input field editor: type a literal value directly, or pick
 * another module's output to wire it as a reference — picking a reference
 * IS the connect gesture, there is no separate drag-to-wire action. When the
 * picked reference is a repeated module (count/for_each), a follow-up
 * control appears to choose the whole collection vs. one instance.
 */
export default function FieldEditor({ type, value, referenceOptions, metaOptions = [], placeholder, error, onChange }: Props) {
  const currentValue = currentAutocompleteValue(value, referenceOptions);
  const options: Array<string | ReferenceOption> = [
    ...(type === "bool" ? ["true", "false"] : []),
    ...metaOptions,
    ...referenceOptions,
  ];

  const matchedOption =
    value?.mode === "reference"
      ? referenceOptions.find((o) => o.nodeId === value.refNodeId && o.output === value.refOutput)
      : undefined;
  const isRepeatedSource = matchedOption ? matchedOption.sourceMultiplicity !== "none" : false;
  const selectorKind = value?.mode === "reference" ? value.refSelector?.kind : undefined;
  const outputSelectorKind = matchedOption ? outputCollectionKind(matchedOption.type) : undefined;

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
      <Autocomplete
        freeSolo
        size="small"
        options={options}
        value={currentValue}
        getOptionLabel={(opt) => (typeof opt === "string" ? opt : opt.label)}
        isOptionEqualToValue={(opt, val) =>
          typeof opt === "string" || typeof val === "string"
            ? opt === val
            : opt.nodeId === val.nodeId && opt.output === val.output
        }
        onChange={(_event, newValue) => {
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
        }}
        onInputChange={(_event, newInputValue, reason) => {
          if (reason === "input") {
            onChange({ mode: "literal", literal: newInputValue });
          }
        }}
        renderInput={(params) => <TextField {...params} placeholder={placeholder} error={!!error} helperText={error} />}
      />
      {isRepeatedSource && value?.mode === "reference" && matchedOption && (
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

              if (next === "all") {
                onChange({ ...value, refSelector: { kind: "all" } });
              } else {
                const selKind = matchedOption.sourceMultiplicity === "for_each" ? "key" : "index";
                onChange({ ...value, refSelector: { kind: selKind, expr: { mode: "literal", literal: "" } } });
              }
            }}
          >
            <ToggleButton value="all" sx={{ fontSize: "0.65rem", py: 0.25, px: 1 }}>
              All instances
            </ToggleButton>
            <ToggleButton value="one" sx={{ fontSize: "0.65rem", py: 0.25, px: 1 }}>
              One instance
            </ToggleButton>
          </ToggleButtonGroup>
          {value.refSelector && value.refSelector.kind !== "all" && (
            <Autocomplete
              freeSolo
              size="small"
              options={value.refSelector.kind === "key" ? (matchedOption.forEachKeys ?? []) : []}
              value={value.refSelector.expr.literal}
              onChange={(_event, newValue) => {
                const selector = value.refSelector as { kind: "index" | "key"; expr: FieldValue };
                onChange({
                  ...value,
                  refSelector: { kind: selector.kind, expr: { mode: "literal", literal: newValue ?? "" } },
                });
              }}
              onInputChange={(_event, newInputValue, reason) => {
                if (reason === "input") {
                  const selector = value.refSelector as { kind: "index" | "key"; expr: FieldValue };
                  onChange({
                    ...value,
                    refSelector: { kind: selector.kind, expr: { mode: "literal", literal: newInputValue } },
                  });
                }
              }}
              sx={{ width: 160 }}
              renderInput={(params) => (
                <TextField
                  {...params}
                  placeholder={selectorKind === "key" ? "key expression" : "index expression"}
                />
              )}
            />
          )}
        </Box>
      )}
      {outputSelectorKind && value?.mode === "reference" && matchedOption && (
        <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
          <Typography variant="caption" color="text.secondary">
            {matchedOption.output} is a {outputSelectorKind === "key" ? "map" : "list"} —
          </Typography>
          <ToggleButtonGroup
            size="small"
            exclusive
            value={!value.refOutputSelector || value.refOutputSelector.kind === "all" ? "all" : "one"}
            onChange={(_e, next) => {
              if (!next) return;
              if (next === "all") {
                onChange({ ...value, refOutputSelector: { kind: "all" } });
              } else {
                onChange({ ...value, refOutputSelector: { kind: outputSelectorKind, expr: { mode: "literal", literal: "" } } });
              }
            }}
          >
            <ToggleButton value="all" sx={{ fontSize: "0.65rem", py: 0.25, px: 1 }}>
              All instances
            </ToggleButton>
            <ToggleButton value="one" sx={{ fontSize: "0.65rem", py: 0.25, px: 1 }}>
              One instance
            </ToggleButton>
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
              sx={{ width: 160 }}
              renderInput={(params) => <TextField {...params} placeholder={outputSelectorKind === "key" ? "key expression" : "index expression"} />}
            />
          )}
        </Box>
      )}
    </Box>
  );
}
