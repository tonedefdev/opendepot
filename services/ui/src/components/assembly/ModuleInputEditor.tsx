"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Checkbox from "@mui/material/Checkbox";
import Collapse from "@mui/material/Collapse";
import FormControlLabel from "@mui/material/FormControlLabel";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import type { TypeSpec } from "./types";
import { moduleInputPath, type FieldValue, type ModuleInputValue, type ReferenceOption } from "./types";
import { emptyValueSpec, orderedObjectAttributes, typeSpecToCtyType } from "./typeSpec";
import FieldEditor from "./FieldEditor";
import {
  AddButton,
  compactFieldSx,
  nameColumnSx,
  RemoveButton,
  RowSpacer,
  RowToggle,
  rowActionsSx,
  rowLabelSx,
  rowSx,
  rowsSx,
} from "./editorRows";

interface Props {
  type: TypeSpec;
  value: ModuleInputValue | undefined;
  referenceOptions: ReferenceOption[];
  metaOptions?: string[];
  error?: string;
  showReferenceControl?: boolean;
  referenceControlPlacement?: "field" | "header";
  path?: string;
  optionalFieldVisibility?: Record<string, boolean>;
  onOptionalFieldVisibilityChange?: (path: string, visible: boolean) => void;
  onChange: (value: ModuleInputValue) => void;
}

function emptyInputValue(type: TypeSpec): ModuleInputValue {
  const value = emptyValueSpec(type);
  if (value.kind === "scalar") return { kind: "scalar", value: { mode: "literal", literal: value.literal } };
  if (value.kind === "list") return { kind: "list", items: [] };
  if (value.kind === "map") return { kind: "map", entries: [] };
  return { kind: "object", entries: [] };
}

function scalarField(value: ModuleInputValue | undefined): FieldValue {
  return value?.kind === "scalar" ? value.value : { mode: "literal", literal: "" };
}

function childValue(type: TypeSpec, value: ModuleInputValue | undefined): ModuleInputValue {
  return value ?? emptyInputValue(type);
}

function labelFor(type: TypeSpec): string {
  return type.kind === "object" ? "Object" : type.kind === "map" ? "Map entries" : type.kind === "list" || type.kind === "set" ? "Items" : "Value";
}

export function isComplexType(type: TypeSpec): boolean {
  return type.kind === "object" || type.kind === "map" || type.kind === "list" || type.kind === "set" || type.kind === "tuple";
}

function valueSummary(value: ModuleInputValue | undefined): string {
  if (value?.kind === "list") return `${value.items.length} ${value.items.length === 1 ? "item" : "items"}`;
  if (value?.kind === "map") return `${value.entries.length} ${value.entries.length === 1 ? "entry" : "entries"}`;
  if (value?.kind === "object") return "{ … }";
  return "";
}

export function ComplexInputReferenceControl({
  type,
  value,
  referenceOptions,
  error,
  fullWidth = true,
  compact = false,
  onChange,
}: {
  type: TypeSpec;
  value: ModuleInputValue | undefined;
  referenceOptions: ReferenceOption[];
  error?: string;
  fullWidth?: boolean;
  compact?: boolean;
  onChange: (value: ModuleInputValue) => void;
}) {
  const selected = value?.kind === "scalar" && value.value.mode === "reference";
  return (
    <FieldEditor
      type={typeSpecToCtyType(type)}
      value={selected ? value.value : { mode: "literal", literal: "" }}
      referenceOptions={referenceOptions}
      referenceOnly
      referenceOnlyFullWidth={fullWidth}
      referenceOnlyCompact={compact}
      error={error}
      onChange={(next) => onChange(next.mode === "reference" ? { kind: "scalar", value: next } : emptyInputValue(type))}
    />
  );
}

function ModuleInputRow({
  label,
  toggleLabel,
  complex,
  summary,
  expanded: controlledExpanded,
  onExpandedChange,
  actions,
  children,
}: {
  label: React.ReactNode;
  toggleLabel: string;
  complex: boolean;
  summary?: string;
  expanded?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
  actions?: React.ReactNode;
  children: React.ReactNode;
}) {
  const [localExpanded, setLocalExpanded] = React.useState(true);
  const expanded = controlledExpanded ?? localExpanded;
  const setExpanded = onExpandedChange ?? setLocalExpanded;

  return (
    <Box>
      <Box sx={rowSx}>
        {complex ? <RowToggle label={toggleLabel} expanded={expanded} onToggle={() => setExpanded(!expanded)} /> : <RowSpacer />}
        {label}
        {complex ? (
          !expanded && summary ? (
            <Typography variant="caption" color="text.secondary">
              {summary}
            </Typography>
          ) : null
        ) : (
          <Box sx={{ flex: "1 1 200px", minWidth: 0 }}>{children}</Box>
        )}
        {actions && <Box data-testid="module-input-row-actions" sx={rowActionsSx}>{actions}</Box>}
      </Box>
      {complex && (
        <Collapse in={expanded} unmountOnExit>
          <Box sx={{ ml: 1.5 }}>{children}</Box>
        </Collapse>
      )}
    </Box>
  );
}

function MapInputEditor({
  type,
  value,
  referenceOptions,
  metaOptions,
  error,
  showReferenceControl,
  path,
  optionalFieldVisibility,
  onOptionalFieldVisibilityChange,
  onChange,
}: {
  type: Extract<TypeSpec, { kind: "map" }>;
  value: ModuleInputValue | undefined;
  referenceOptions: ReferenceOption[];
  metaOptions: string[];
  error?: string;
  showReferenceControl: boolean;
  path: string;
  optionalFieldVisibility: Record<string, boolean>;
  onOptionalFieldVisibilityChange?: (path: string, visible: boolean) => void;
  onChange: (value: ModuleInputValue) => void;
}) {
  const entries = value?.kind === "map" ? value.entries : [];
  const [expanded, setExpanded] = React.useState<number | false>(false);
  const keyCounts = new Map<string, number>();
  entries.forEach((entry) => {
    const key = entry.key.trim();
    keyCounts.set(key, (keyCounts.get(key) ?? 0) + 1);
  });
  const conflictingKeys = new Set(
    Array.from(keyCounts.entries())
      .filter(([, count]) => count > 1)
      .map(([key]) => key),
  );

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75, pl: 1, borderLeft: "2px solid", borderColor: "divider" }}>
      <Box sx={{ display: "flex", alignItems: "center", minWidth: 0 }}>
        <Typography variant="caption" color="text.secondary">{labelFor(type)}</Typography>
        {showReferenceControl && (
          <Box sx={{ flex: "1 1 auto", minWidth: 0 }}>
            <ComplexInputReferenceControl type={type} value={value} referenceOptions={referenceOptions} onChange={onChange} />
          </Box>
        )}
      </Box>
      <Box sx={rowsSx}>
        {entries.map((entry, index) => (
          <ModuleInputRow
            key={index}
            label={
              <TextField
                variant="filled"
                size="small"
                hiddenLabel
                placeholder="key"
                value={entry.key}
                error={conflictingKeys.has(entry.key.trim())}
                onChange={(event) => {
                  const nextEntries = entries.slice();
                  nextEntries[index] = { ...entry, key: event.target.value };
                  onChange({ kind: "map", entries: nextEntries });
                }}
                slotProps={{ input: { disableUnderline: true }, htmlInput: { "aria-label": "Key" } }}
                sx={{ ...compactFieldSx, ...nameColumnSx }}
              />
            }
            toggleLabel={entry.key.trim() || "New entry"}
            complex={isComplexType(type.element)}
            summary={valueSummary(entry.value)}
            expanded={expanded === index}
            onExpandedChange={(isExpanded) => setExpanded(isExpanded ? index : false)}
            actions={
              <>
                {isComplexType(type.element) && (
                  <ComplexInputReferenceControl
                    type={type.element}
                    value={entry.value}
                    referenceOptions={referenceOptions}
                    fullWidth={false}
                    onChange={(next) => {
                      const nextEntries = entries.slice();
                      nextEntries[index] = { ...entry, value: next };
                      onChange({ kind: "map", entries: nextEntries });
                    }}
                  />
                )}
                <RemoveButton
                  label="Remove entry"
                  onClick={() => {
                    setExpanded((current) => {
                      if (current === index) return false;
                      return current !== false && current > index ? current - 1 : current;
                    });
                    onChange({ kind: "map", entries: entries.filter((_, itemIndex) => itemIndex !== index) });
                  }}
                />
              </>
            }
          >
            <ModuleInputEditor
              type={type.element}
              value={entry.value}
              referenceOptions={referenceOptions}
              metaOptions={metaOptions}
              showReferenceControl={false}
              path={moduleInputPath(path, `map-entry:${index}:${entry.key}`)}
              optionalFieldVisibility={optionalFieldVisibility}
              onOptionalFieldVisibilityChange={onOptionalFieldVisibilityChange}
              onChange={(next) => {
                const nextEntries = entries.slice();
                nextEntries[index] = { ...entry, value: next };
                onChange({ kind: "map", entries: nextEntries });
              }}
            />
          </ModuleInputRow>
        ))}
      </Box>
      <AddButton
        onClick={() => {
          onChange({ kind: "map", entries: [...entries, { key: "", value: emptyInputValue(type.element) }] });
          setExpanded(entries.length);
        }}
      >
        Add entry
      </AddButton>
      {error && (
        <Typography variant="caption" sx={{ color: "#f85149", fontWeight: 600 }}>
          {error}
        </Typography>
      )}
    </Box>
  );
}

export default function ModuleInputEditor({
  type,
  value,
  referenceOptions,
  metaOptions = [],
  error,
  path = "",
  referenceControlPlacement = "field",
  optionalFieldVisibility = {},
  onOptionalFieldVisibilityChange,
  showReferenceControl = true,
  onChange,
}: Props) {
  if (type.kind === "string" || type.kind === "number" || type.kind === "bool" || type.kind === "any") {
    return (
      <FieldEditor
        type={typeSpecToCtyType(type)}
        value={scalarField(value)}
        referenceOptions={referenceOptions}
        metaOptions={metaOptions}
        multilineHcl
        showReferenceControl={referenceControlPlacement !== "header"}
        error={error}
        onChange={(next) => onChange({ kind: "scalar", value: next })}
      />
    );
  }

  if (value?.kind === "scalar" && value.value.mode === "reference") {
    return showReferenceControl
      ? <ComplexInputReferenceControl type={type} value={value} referenceOptions={referenceOptions} error={error} onChange={onChange} />
      : null;
  }

  if (type.kind === "object") {
    const entries = value?.kind === "object" ? value.entries : [];
    const entryNames = new Set(entries.map((entry) => entry.name));
    const optional = type.attributes.filter((attribute) => attribute.optional);
    const [localShowOptional, setLocalShowOptional] = React.useState(optional.some((attribute) => entryNames.has(attribute.name)));
    const showOptional = optionalFieldVisibility[path] ?? localShowOptional;
    const visibleAttributes = orderedObjectAttributes(
      type.attributes.filter((attribute) => !attribute.optional || showOptional),
    );

    return (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75, pl: 1, borderLeft: "2px solid", borderColor: "divider" }}>
        {showReferenceControl && <ComplexInputReferenceControl type={type} value={value} referenceOptions={referenceOptions} onChange={onChange} />}
        {optional.length > 0 && (
          <FormControlLabel
            control={
              <Checkbox
                size="small"
                checked={showOptional}
                onChange={(event) => {
                  const visible = event.target.checked;
                  setLocalShowOptional(visible);
                  onOptionalFieldVisibilityChange?.(path, visible);
                }}
              />
            }
            label={<Typography variant="caption">Show optional fields</Typography>}
          />
        )}
        <Box sx={rowsSx}>
          {visibleAttributes.map((attribute) => {
            const current = entries.find((entry) => entry.name === attribute.name)?.value;
            return (
              <ModuleInputRow
                key={attribute.name}
                label={
                  <Typography sx={{ ...rowLabelSx, flex: { xs: "1 1 120px", sm: "0 0 120px" }, fontWeight: 600 }}>
                    {attribute.name}
                    {attribute.optional && " (optional)"}
                  </Typography>
                }
                toggleLabel={`${attribute.name} value`}
                complex={isComplexType(attribute.type)}
                summary={valueSummary(current)}
                actions={isComplexType(attribute.type) ? (
                  <ComplexInputReferenceControl
                    type={attribute.type}
                    value={current}
                    referenceOptions={referenceOptions}
                    fullWidth={false}
                    onChange={(next) => {
                      const nextEntries = entries
                        .filter((entry) => entry.name !== attribute.name)
                        .concat({ name: attribute.name, value: next });
                      onChange({ kind: "object", entries: nextEntries });
                    }}
                  />
                ) : undefined}
              >
                <ModuleInputEditor
                  type={attribute.type}
                  value={current}
                  referenceOptions={referenceOptions}
                  metaOptions={metaOptions}
                  showReferenceControl={false}
                  path={moduleInputPath(path, `attribute:${attribute.name}`)}
                  optionalFieldVisibility={optionalFieldVisibility}
                  onOptionalFieldVisibilityChange={onOptionalFieldVisibilityChange}
                  onChange={(next) => {
                    const nextEntries = entries
                      .filter((entry) => entry.name !== attribute.name)
                      .concat({ name: attribute.name, value: next });
                    onChange({ kind: "object", entries: nextEntries });
                  }}
                />
              </ModuleInputRow>
            );
          })}
        </Box>
        {error && <Typography variant="caption" color="error">{error}</Typography>}
      </Box>
    );
  }

  if (type.kind === "map") {
    return (
      <MapInputEditor
        type={type}
        value={value}
        referenceOptions={referenceOptions}
        metaOptions={metaOptions}
        error={error}
        path={path}
        optionalFieldVisibility={optionalFieldVisibility}
        onOptionalFieldVisibilityChange={onOptionalFieldVisibilityChange}
        showReferenceControl={showReferenceControl}
        onChange={onChange}
      />
    );
  }

  if (type.kind !== "list" && type.kind !== "set" && type.kind !== "tuple") {
    return null;
  }

  const storedItems = value?.kind === "list" ? value.items : [];
  const items =
    type.kind === "tuple" ? type.elements.map((element, index) => storedItems[index] ?? emptyInputValue(element)) : storedItems;
  const itemType = type.kind === "tuple" ? undefined : type.element;
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75, pl: 1, borderLeft: "2px solid", borderColor: "divider" }}>
      {showReferenceControl && <ComplexInputReferenceControl type={type} value={value} referenceOptions={referenceOptions} onChange={onChange} />}
      <Typography variant="caption" color="text.secondary">{labelFor(type)}</Typography>
      <Box sx={rowsSx}>
        {items.map((item, index) => {
          const currentType = type.kind === "tuple" ? type.elements[index] : itemType;
          if (!currentType) return null;
          return (
            <ModuleInputRow
              key={index}
              label={<Typography sx={rowLabelSx}>[{index}]</Typography>}
              toggleLabel={`${type.kind === "tuple" ? "Element" : "Item"} ${index}`}
              complex={isComplexType(currentType)}
              summary={valueSummary(item)}
              actions={type.kind !== "tuple" || isComplexType(currentType) ? (
                <>
                  {isComplexType(currentType) && (
                    <ComplexInputReferenceControl
                      type={currentType}
                      value={item}
                      referenceOptions={referenceOptions}
                      fullWidth={false}
                      onChange={(next) => {
                        const nextItems = items.slice();
                        nextItems[index] = next;
                        onChange({ kind: "list", items: nextItems });
                      }}
                    />
                  )}
                  {type.kind !== "tuple" && (
                    <RemoveButton
                      label="Remove item"
                      onClick={() => onChange({ kind: "list", items: items.filter((_, itemIndex) => itemIndex !== index) })}
                    />
                  )}
                </>
              ) : undefined}
            >
              <ModuleInputEditor
                type={currentType}
                value={item}
                referenceOptions={referenceOptions}
                metaOptions={metaOptions}
                showReferenceControl={false}
                path={moduleInputPath(path, `item:${index}`)}
                optionalFieldVisibility={optionalFieldVisibility}
                onOptionalFieldVisibilityChange={onOptionalFieldVisibilityChange}
                onChange={(next) => {
                  const nextItems = items.slice();
                  nextItems[index] = next;
                  onChange({ kind: "list", items: nextItems });
                }}
              />
            </ModuleInputRow>
          );
        })}
      </Box>
      {type.kind !== "tuple" && <AddButton onClick={() => onChange({ kind: "list", items: [...items, emptyInputValue(type.element)] })}>Add item</AddButton>}
    </Box>
  );
}