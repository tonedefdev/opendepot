"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import IconButton from "@mui/material/IconButton";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import Accordion from "@mui/material/Accordion";
import AccordionDetails from "@mui/material/AccordionDetails";
import AccordionSummary from "@mui/material/AccordionSummary";
import AddIcon from "@mui/icons-material/Add";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import type { TypeSpec } from "./types";
import type { FieldValue, ModuleInputValue, ReferenceOption } from "./types";
import { emptyValueSpec, orderedObjectAttributes, typeSpecToCtyType } from "./typeSpec";
import FieldEditor from "./FieldEditor";

interface Props {
  type: TypeSpec;
  value: ModuleInputValue | undefined;
  referenceOptions: ReferenceOption[];
  metaOptions?: string[];
  error?: string;
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

function MapInputEditor({
  type,
  value,
  referenceOptions,
  metaOptions,
  error,
  onChange,
}: {
  type: Extract<TypeSpec, { kind: "map" }>;
  value: ModuleInputValue | undefined;
  referenceOptions: ReferenceOption[];
  metaOptions: string[];
  error?: string;
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
      <Typography variant="caption" color="text.secondary">{labelFor(type)}</Typography>
      {entries.map((entry, index) => (
        <Accordion
          key={index}
          disableGutters
          expanded={expanded === index}
          onChange={(_, isExpanded) => setExpanded(isExpanded ? index : false)}
          sx={{
            bgcolor: "transparent",
            boxShadow: "none",
            border: "1px solid",
            borderColor: conflictingKeys.has(entry.key.trim()) ? "error.main" : "divider",
            borderLeftWidth: 2,
            "&:before": { display: "none" },
            "& .MuiAccordionSummary-root": { minHeight: 40, px: 1 },
            "& .MuiAccordionSummary-content": { my: 0.75 },
            "& .MuiAccordionDetails-root": { px: 1, pt: 0.5 },
          }}
        >
          <AccordionSummary expandIcon={<ExpandMoreIcon />}>
            <Typography noWrap>{entry.key.trim() || "New Entry"}</Typography>
          </AccordionSummary>
          <AccordionDetails>
            <Box sx={{ display: "flex", alignItems: "flex-start", gap: 0.5, mb: 1 }}>
              <TextField
                size="small"
                label="Key"
                value={entry.key}
                onChange={(event) => {
                  const nextEntries = entries.slice();
                  nextEntries[index] = { ...entry, key: event.target.value };
                  onChange({ kind: "map", entries: nextEntries });
                }}
                fullWidth
              />
              <IconButton
                size="small"
                aria-label="Remove map entry"
                onClick={() => {
                  const nextEntries = entries.filter((_, itemIndex) => itemIndex !== index);
                  setExpanded((current) => {
                    if (current === index) return false;
                    return current !== false && current > index ? current - 1 : current;
                  });
                  onChange({ kind: "map", entries: nextEntries });
                }}
              >
                <DeleteOutlineIcon fontSize="small" />
              </IconButton>
            </Box>
            <ModuleInputEditor
              type={type.element}
              value={entry.value}
              referenceOptions={referenceOptions}
              metaOptions={metaOptions}
              onChange={(next) => {
                const nextEntries = entries.slice();
                nextEntries[index] = { ...entry, value: next };
                onChange({ kind: "map", entries: nextEntries });
              }}
            />
          </AccordionDetails>
        </Accordion>
      ))}
      <Button
        size="small"
        startIcon={<AddIcon />}
        onClick={() => {
          onChange({ kind: "map", entries: [...entries, { key: "", value: emptyInputValue(type.element) }] });
          setExpanded(entries.length);
        }}
      >
        Add entry
      </Button>
      {error && (
        <Typography variant="caption" sx={{ color: "#f85149", fontWeight: 600 }}>
          {error}
        </Typography>
      )}
    </Box>
  );
}

export default function ModuleInputEditor({ type, value, referenceOptions, metaOptions = [], error, onChange }: Props) {
  if (type.kind === "string" || type.kind === "number" || type.kind === "bool" || type.kind === "any") {
    return (
      <FieldEditor
        type={typeSpecToCtyType(type)}
        value={scalarField(value)}
        referenceOptions={referenceOptions}
        metaOptions={metaOptions}
        error={error}
        onChange={(next) => onChange({ kind: "scalar", value: next })}
      />
    );
  }

  if (type.kind === "object") {
    const entries = value?.kind === "object" ? value.entries : [];
    const entryNames = new Set(entries.map((entry) => entry.name));
    const optional = type.attributes.filter((attribute) => attribute.optional);
    const [showOptional, setShowOptional] = React.useState(optional.some((attribute) => entryNames.has(attribute.name)));
    const visibleAttributes = orderedObjectAttributes(
      type.attributes.filter((attribute) => !attribute.optional || showOptional || entryNames.has(attribute.name)),
    );

    return (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 1, pl: 1, borderLeft: "2px solid", borderColor: "divider" }}>
        {optional.length > 0 && (
          <FormControlLabel
            control={<Checkbox size="small" checked={showOptional} onChange={(event) => setShowOptional(event.target.checked)} />}
            label={<Typography variant="caption">Show optional fields</Typography>}
          />
        )}
        {visibleAttributes.map((attribute) => {
          const current = entries.find((entry) => entry.name === attribute.name)?.value;
          return (
            <Box key={attribute.name}>
              <Typography variant="caption" fontWeight={600} sx={{ display: "block", mb: 0.25 }}>
                {attribute.name}
                {attribute.optional && " (optional)"}
              </Typography>
              <ModuleInputEditor
                type={attribute.type}
                value={current}
                referenceOptions={referenceOptions}
                metaOptions={metaOptions}
                onChange={(next) => {
                  const nextEntries = entries.filter((entry) => entry.name !== attribute.name).concat({ name: attribute.name, value: next });
                  onChange({ kind: "object", entries: nextEntries });
                }}
              />
            </Box>
          );
        })}
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
        onChange={onChange}
      />
    );
  }

  if (type.kind !== "list" && type.kind !== "set" && type.kind !== "tuple") {
    return null;
  }

  const items = value?.kind === "list" ? value.items : [];
  const itemType = type.kind === "tuple" ? undefined : type.element;
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1, pl: 1, borderLeft: "2px solid", borderColor: "divider" }}>
      <Typography variant="caption" color="text.secondary">{labelFor(type)}</Typography>
      {items.map((item, index) => {
        const currentType = type.kind === "tuple" ? type.elements[index] : itemType;
        if (!currentType) return null;
        return (
          <Box key={index} sx={{ display: "flex", alignItems: "flex-start", gap: 0.5 }}>
            <Box sx={{ flex: 1, minWidth: 0 }}>
              <ModuleInputEditor type={currentType} value={item} referenceOptions={referenceOptions} metaOptions={metaOptions} onChange={(next) => {
                const nextItems = items.slice();
                nextItems[index] = next;
                onChange({ kind: "list", items: nextItems });
              }} />
            </Box>
            {type.kind !== "tuple" && <IconButton size="small" onClick={() => onChange({ kind: "list", items: items.filter((_, itemIndex) => itemIndex !== index) })} aria-label="Remove list item"><DeleteOutlineIcon fontSize="small" /></IconButton>}
          </Box>
        );
      })}
      {type.kind !== "tuple" && <Button size="small" startIcon={<AddIcon />} onClick={() => onChange({ kind: "list", items: [...items, emptyInputValue(type.element)] })}>Add item</Button>}
    </Box>
  );
}