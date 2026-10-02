"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import TextField from "@mui/material/TextField";
import IconButton from "@mui/material/IconButton";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import Accordion from "@mui/material/Accordion";
import AccordionDetails from "@mui/material/AccordionDetails";
import AccordionSummary from "@mui/material/AccordionSummary";
import AddIcon from "@mui/icons-material/Add";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import type { ScalarTypeSpec, TypeSpec, ValueSpec } from "./types";
import { emptyValueSpec, valueSpecForType } from "./typeSpec";

const filledFieldSx = { "& .MuiFilledInput-root": { borderRadius: 1 } };
const nestedBranchSx = {
  py: 0.5,
  pl: 1,
  borderLeft: "2px solid",
  borderColor: "divider",
};
const nestedAccordionSx = {
  bgcolor: "transparent",
  boxShadow: "none",
  borderLeft: "2px solid",
  borderColor: "divider",
  "&:before": { display: "none" },
  "& .MuiAccordionSummary-root": { minHeight: 40, px: 1 },
  "& .MuiAccordionSummary-content": { my: 0.75 },
  "& .MuiAccordionDetails-root": { px: 1, pt: 0.5 },
};

interface Props {
  type: TypeSpec;
  value: ValueSpec;
  onChange: (v: ValueSpec) => void;
}

function scalarPlaceholder(type: ScalarTypeSpec): string {
  if (type.kind === "bool") return "true or false";
  if (type.kind === "number") return "e.g. 3";
  return "value";
}

export default function ValueEditor({ type, value, onChange }: Props) {
  if (type.kind === "string" || type.kind === "number" || type.kind === "bool" || type.kind === "any") {
    const literal = value.kind === "scalar" ? value.literal : "";
    return (
      <TextField
        variant="filled"
        size="small"
        fullWidth
        placeholder={scalarPlaceholder(type)}
        value={literal}
        onChange={(e) => onChange({ kind: "scalar", literal: e.target.value })}
        sx={filledFieldSx}
      />
    );
  }

  if (type.kind === "list" || type.kind === "set") {
    const items = value.kind === "list" ? value.items : [];
    return (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
        {items.map((item, i) => (
          <Box key={i} sx={{ display: "flex", alignItems: "flex-start", gap: 0.5 }}>
            <Box sx={{ flex: 1 }}>
              <ValueEditor
                type={type.element}
                value={valueSpecForType(type.element, item)}
                onChange={(itemValue) => {
                  const next = [...items];
                  next[i] = itemValue;
                  onChange({ kind: "list", items: next });
                }}
              />
            </Box>
            <IconButton
              size="small"
              aria-label="Remove item"
              onClick={() => {
                const next = [...items];
                next.splice(i, 1);
                onChange({ kind: "list", items: next });
              }}
            >
              <DeleteOutlineIcon fontSize="small" />
            </IconButton>
          </Box>
        ))}
        <Button
          size="small"
          startIcon={<AddIcon />}
          onClick={() => onChange({ kind: "list", items: [...items, emptyValueSpec(type.element)] })}
        >
          Add item
        </Button>
      </Box>
    );
  }

  if (type.kind === "tuple") {
    const items = value.kind === "list" ? value.items : [];
    return (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
        {type.elements.map((elType, i) => (
          <Box key={i}>
            <Typography variant="caption" color="text.secondary">
              Element {i + 1}
            </Typography>
            <ValueEditor
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
          </Box>
        ))}
      </Box>
    );
  }

  if (type.kind === "map") {
    return <MapValueEditor type={type} value={value} onChange={onChange} />;
  }

  if (type.kind === "object") {
    const entries = value.kind === "object" ? value.entries : [];
    return (
      <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
        {type.attributes.map((attr) => {
          const existing = entries.find((e) => e.name === attr.name);
          return (
            <Box key={attr.name} sx={nestedBranchSx}>
              <Typography variant="caption" color="text.secondary" sx={{ display: "block", mb: 0.5 }}>
                {attr.name}
                {attr.optional ? " (optional)" : ""}
              </Typography>
            <ValueEditor
                type={attr.type}
                value={valueSpecForType(attr.type, existing?.value)}
                onChange={(entryValue) => {
                  const next = entries.filter((entry) => entry.name !== attr.name);
                  next.push({ name: attr.name, value: entryValue });
                  onChange({ kind: "object", entries: next });
              }}
            />
            </Box>
          );
        })}
      </Box>
    );
  }

  return null;
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
  const idPrefix = React.useId().replaceAll(":", "");

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
      {entries.map((entry, index) => (
        <Accordion
          key={index}
          disableGutters
          expanded={expanded === index}
          onChange={(_, isExpanded) => setExpanded(isExpanded ? index : false)}
          sx={nestedAccordionSx}
        >
          <AccordionSummary expandIcon={<ExpandMoreIcon />} aria-controls={`${idPrefix}-map-entry-${index}-content`}>
            <Typography noWrap>{entry.key.trim() || "New entry"}</Typography>
          </AccordionSummary>
          <AccordionDetails id={`${idPrefix}-map-entry-${index}-content`} sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
            <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
              <TextField
                variant="filled"
                size="small"
                label="Key"
                value={entry.key}
                onChange={(event) => {
                  const next = [...entries];
                  next[index] = { ...entry, key: event.target.value };
                  onChange({ kind: "map", entries: next });
                }}
                sx={{ ...filledFieldSx, flex: 1 }}
              />
              <IconButton
                size="small"
                aria-label="Remove entry"
                onClick={() => {
                  const next = entries.filter((_, entryIndex) => entryIndex !== index);
                  setExpanded((current) => {
                    if (current === index) return false;
                    return current !== false && current > index ? current - 1 : current;
                  });
                  onChange({ kind: "map", entries: next });
                }}
              >
                <DeleteOutlineIcon fontSize="small" />
              </IconButton>
            </Box>
            <ValueEditor
              type={type.element}
              value={valueSpecForType(type.element, entry.value)}
              onChange={(entryValue) => {
                const next = [...entries];
                next[index] = { ...entry, value: entryValue };
                onChange({ kind: "map", entries: next });
              }}
            />
          </AccordionDetails>
        </Accordion>
      ))}
      <Button
        size="small"
        startIcon={<AddIcon />}
        onClick={() => {
          onChange({ kind: "map", entries: [...entries, { key: "", value: emptyValueSpec(type.element) }] });
          setExpanded(entries.length);
        }}
      >
        Add entry
      </Button>
    </Box>
  );
}
