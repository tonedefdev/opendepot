"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Collapse from "@mui/material/Collapse";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import type { ScalarTypeSpec, TypeSpec, ValueSpec } from "./types";
import { emptyValueSpec, valueSpecForType } from "./typeSpec";
import {
  AddButton,
  RemoveButton,
  RowSpacer,
  RowToggle,
  childBranchSx,
  compactFieldSx,
  nameColumnSx,
  rowActionsSx,
  rowLabelSx,
  rowSx,
  rowsSx,
  valueBranchSx,
} from "./editorRows";

interface Props {
  type: TypeSpec;
  value: ValueSpec;
  onChange: (v: ValueSpec) => void;
}

function isScalar(type: TypeSpec): type is ScalarTypeSpec {
  return type.kind === "string" || type.kind === "number" || type.kind === "bool" || type.kind === "any";
}

function scalarPlaceholder(type: ScalarTypeSpec): string {
  if (type.kind === "bool") return "true or false";
  if (type.kind === "number") return "e.g. 3";
  return "value";
}

function valueSummary(value: ValueSpec): string {
  if (value.kind === "list") return `${value.items.length} ${value.items.length === 1 ? "item" : "items"}`;
  if (value.kind === "map") return `${value.entries.length} ${value.entries.length === 1 ? "entry" : "entries"}`;
  if (value.kind === "object") return "{ … }";
  return "";
}

function ScalarInput({ type, value, onChange, label }: Props & { type: ScalarTypeSpec; label?: string }) {
  return (
    <TextField
      variant="filled"
      size="small"
      hiddenLabel
      fullWidth
      placeholder={scalarPlaceholder(type)}
      value={value.kind === "scalar" ? value.literal : ""}
      onChange={(e) => onChange({ kind: "scalar", literal: e.target.value })}
      slotProps={{ input: { disableUnderline: true }, htmlInput: { "aria-label": label } }}
      sx={compactFieldSx}
    />
  );
}

/** One `label  value` line; collection/object values nest beneath it behind a collapse toggle. */
export function ValueRow({
  label,
  toggleLabel,
  type,
  value,
  onChange,
  actions,
  expanded: controlledExpanded,
  onExpandedChange,
  alignToValueColumn = false,
}: Props & {
  label: React.ReactNode;
  toggleLabel: string;
  actions?: React.ReactNode;
  expanded?: boolean;
  onExpandedChange?: (expanded: boolean) => void;
  alignToValueColumn?: boolean;
}) {
  const [localExpanded, setLocalExpanded] = React.useState(true);
  const expanded = controlledExpanded ?? localExpanded;
  const setExpanded = onExpandedChange ?? setLocalExpanded;

  if (isScalar(type)) {
    return (
      <Box sx={rowSx}>
        <RowSpacer />
        {label}
        <Box sx={{ flex: "1 1 200px", minWidth: 0 }}>
          <ScalarInput type={type} value={value} onChange={onChange} label={`${toggleLabel} value`} />
        </Box>
        {actions && <Box sx={rowActionsSx}>{actions}</Box>}
      </Box>
    );
  }

  return (
    <Box>
      <Box sx={rowSx}>
        <RowToggle label={toggleLabel} expanded={expanded} onToggle={() => setExpanded(!expanded)} />
        {label}
        {!expanded && (
          <Typography variant="caption" color="text.secondary">
            {valueSummary(value)}
          </Typography>
        )}
        {actions && <Box sx={rowActionsSx}>{actions}</Box>}
      </Box>
      <Collapse in={expanded} unmountOnExit>
        <Box sx={alignToValueColumn ? { ...childBranchSx, ...valueBranchSx } : childBranchSx}>
          <ValueEditor type={type} value={value} onChange={onChange} />
        </Box>
      </Collapse>
    </Box>
  );
}

export default function ValueEditor({ type, value, onChange }: Props) {
  if (isScalar(type)) return <ScalarInput type={type} value={value} onChange={onChange} />;

  if (type.kind === "list" || type.kind === "set") {
    const items = value.kind === "list" ? value.items : [];
    const setItems = (next: ValueSpec[]) => onChange({ kind: "list", items: next });
    return (
      <Box sx={rowsSx}>
        {items.map((item, i) => (
          <ValueRow
            key={i}
            label={<Typography sx={rowLabelSx}>[{i}]</Typography>}
            toggleLabel={`Item ${i}`}
            type={type.element}
            value={valueSpecForType(type.element, item)}
            onChange={(itemValue) => {
              const next = [...items];
              next[i] = itemValue;
              setItems(next);
            }}
            actions={<RemoveButton label="Remove item" onClick={() => setItems(items.filter((_, idx) => idx !== i))} />}
          />
        ))}
        <AddButton onClick={() => setItems([...items, emptyValueSpec(type.element)])}>Add item</AddButton>
      </Box>
    );
  }

  if (type.kind === "tuple") {
    const items = value.kind === "list" ? value.items : [];
    return (
      <Box sx={rowsSx}>
        {type.elements.map((elType, i) => (
          <ValueRow
            key={i}
            label={<Typography sx={rowLabelSx}>[{i}]</Typography>}
            toggleLabel={`Element ${i}`}
            type={elType}
            value={valueSpecForType(elType, items[i])}
            onChange={(itemValue) => {
              const next = [...items];
              while (next.length <= i) {
                next.push(emptyValueSpec(type.elements[next.length]));
              }
              next[i] = itemValue;
              onChange({ kind: "list", items: next });
            }}
          />
        ))}
      </Box>
    );
  }

  if (type.kind === "map") {
    return <MapValueEditor type={type} value={value} onChange={onChange} />;
  }

  if (type.kind !== "object") return null;

  const entries = value.kind === "object" ? value.entries : [];
  const attributes = type.attributes.filter((attr) => attr.name.trim());
  if (attributes.length === 0) {
    return (
      <Typography variant="caption" color="text.secondary" sx={{ display: "block", px: 0.5, py: 0.75 }}>
        Name this object&apos;s attributes to set its values.
      </Typography>
    );
  }
  return (
    <Box sx={rowsSx}>
      {attributes.map((attr) => {
        const existing = entries.find((e) => e.name === attr.name);
        return (
          <ValueRow
            key={attr.name}
            label={<Typography sx={rowLabelSx}>{attr.name}</Typography>}
            toggleLabel={attr.name}
            type={attr.type}
            value={valueSpecForType(attr.type, existing?.value)}
            onChange={(entryValue) => {
              const next = entries.filter((entry) => entry.name !== attr.name);
              next.push({ name: attr.name, value: entryValue });
              onChange({ kind: "object", entries: next });
            }}
          />
        );
      })}
    </Box>
  );
}

function MapValueEditor({
  type,
  value,
  onChange,
}: {
  type: Extract<TypeSpec, { kind: "map" }>;
  value: ValueSpec;
  onChange: (value: ValueSpec) => void;
}) {
  const entries = value.kind === "map" ? value.entries : [];
  const [expanded, setExpanded] = React.useState<number | false>(false);

  return (
    <Box sx={rowsSx}>
      {entries.map((entry, index) => (
        <ValueRow
          key={index}
          label={
            <TextField
              variant="filled"
              size="small"
              hiddenLabel
              placeholder="key"
              value={entry.key}
              onChange={(event) => {
                const next = [...entries];
                next[index] = { ...entry, key: event.target.value };
                onChange({ kind: "map", entries: next });
              }}
              slotProps={{ input: { disableUnderline: true }, htmlInput: { "aria-label": "Key" } }}
              sx={{ ...compactFieldSx, ...nameColumnSx }}
            />
          }
          toggleLabel={entry.key.trim() || "New entry"}
          type={type.element}
          value={valueSpecForType(type.element, entry.value)}
          onChange={(entryValue) => {
            const next = [...entries];
            next[index] = { ...entry, value: entryValue };
            onChange({ kind: "map", entries: next });
          }}
          expanded={expanded === index}
          onExpandedChange={(isExpanded) => setExpanded(isExpanded ? index : false)}
          actions={
            <RemoveButton
              label="Remove entry"
              onClick={() => {
                setExpanded((current) => {
                  if (current === index) return false;
                  return current !== false && current > index ? current - 1 : current;
                });
                onChange({ kind: "map", entries: entries.filter((_, entryIndex) => entryIndex !== index) });
              }}
            />
          }
        />
      ))}
      <AddButton
        onClick={() => {
          onChange({ kind: "map", entries: [...entries, { key: "", value: emptyValueSpec(type.element) }] });
          setExpanded(entries.length);
        }}
      >
        Add entry
      </AddButton>
    </Box>
  );
}
