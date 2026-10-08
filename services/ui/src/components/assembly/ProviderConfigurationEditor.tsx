"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Divider from "@mui/material/Divider";
import Typography from "@mui/material/Typography";
import type { ProviderSchemaBlock } from "@/lib/api";
import { renderTypeCompact } from "@/lib/ctyType";
import FieldEditor, { ReferenceSelectorControls } from "./FieldEditor";
import type { ProviderConfiguration, ReferenceOption } from "./types";
import { AddButton, RemoveButton, rowLabelSx, rowSx, rowsSx } from "./editorRows";

interface Props {
  schema: ProviderSchemaBlock;
  value: ProviderConfiguration;
  referenceOptions: ReferenceOption[];
  onChange: (value: ProviderConfiguration) => void;
  depth?: number;
  entryOrder?: string[];
}

export function emptyProviderConfiguration(): ProviderConfiguration {
  return { arguments: {}, blocks: {} };
}

export function providerConfigurationForSchema(schema: ProviderSchemaBlock): ProviderConfiguration {
  const configuration = emptyProviderConfiguration();
  for (const [name, nested] of Object.entries(schema.blocks ?? {})) {
    const count = nested.minItems ?? 0;
    if (count > 0) {
      configuration.blocks[name] = Array.from({ length: count }, () => providerConfigurationForSchema(nested.block));
    }
  }

  return configuration;
}

export default function ProviderConfigurationEditor({ schema, value, referenceOptions, onChange, depth = 0, entryOrder }: Props) {
  const attributes = Object.entries(schema.attributes ?? {})
    .filter(([, attribute]) => attribute.required || attribute.optional)
    .sort(([left], [right]) => left.localeCompare(right));
  const blocks = Object.entries(schema.blocks ?? {}).sort(([left], [right]) => left.localeCompare(right));
  const orderedEntries = entryOrder ?? [...attributes.map(([name]) => name), ...blocks.map(([name]) => name)];

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1, pl: depth ? 1.5 : 0, borderLeft: depth ? "2px solid" : "none", borderColor: "divider" }}>
      {orderedEntries.map((name) => {
        const attribute = schema.attributes?.[name];
        if (attribute && (attribute.required || attribute.optional)) {
          const field = value.arguments[name];
          const moveReferenceToControls = field?.mode === "reference" && field.refOutputSelector !== undefined;
          return (
            <Box key={name} sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
              <Box sx={{ display: "flex", alignItems: "flex-start", gap: 0.75, mb: 0.5 }}>
                <Typography variant="body2" fontWeight={600}>{name}</Typography>
                <Chip
                  label={attribute.required ? "required" : "optional"}
                  size="small"
                  color={attribute.required ? "error" : "default"}
                  variant="outlined"
                  sx={{ height: 18, fontSize: "0.65rem" }}
                />
                <Box sx={{ display: "flex", alignItems: "flex-start", gap: 0.75, ml: "auto", minWidth: 0 }}>
                  {!moveReferenceToControls && (
                    <FieldEditor
                      type={attribute.type}
                      value={field}
                      referenceOptions={referenceOptions}
                      referenceOnly
                      referenceOnlyFullWidth={false}
                      referenceOnlyCompact
                      onChange={(next) => onChange({ ...value, arguments: { ...value.arguments, [name]: next } })}
                    />
                  )}
                  <Chip
                    label={renderTypeCompact(attribute.type)}
                    size="small"
                    variant="outlined"
                    sx={{ height: 18, fontSize: "0.65rem", flexShrink: 0 }}
                  />
                </Box>
              </Box>
              {field?.mode === "reference" && (
                <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5, mb: 0.5 }}>
                  <ReferenceSelectorControls
                    value={field}
                    referenceOptions={referenceOptions}
                    compact
                    leadingControl={moveReferenceToControls ? (
                      <FieldEditor
                        type={attribute.type}
                        value={field}
                        referenceOptions={referenceOptions}
                        referenceOnly
                        referenceOnlyFullWidth={false}
                        referenceOnlyCompact
                        onChange={(next) => onChange({ ...value, arguments: { ...value.arguments, [name]: next } })}
                      />
                    ) : undefined}
                    onChange={(next) => onChange({ ...value, arguments: { ...value.arguments, [name]: next } })}
                  />
                </Box>
              )}
              <FieldEditor
                type={attribute.type}
                value={field}
                referenceOptions={referenceOptions}
                multilineHcl
                showReferenceControl={false}
                onChange={(next) => onChange({ ...value, arguments: { ...value.arguments, [name]: next } })}
              />
            </Box>
          );
        }

        const nested = schema.blocks?.[name];
        if (!nested) return null;
        const instances = value.blocks[name] ?? [];
        return (
          <Box key={name} sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
            {depth > 0 && <Divider />}
            <Box sx={rowSx}>
              <Typography variant="body2" fontWeight={600}>{name}</Typography>
              <Chip
                label={(nested.minItems ?? 0) > 0 ? "required block" : "optional block"}
                size="small"
                color={(nested.minItems ?? 0) > 0 ? "error" : "default"}
                variant="outlined"
                sx={{ height: 18, fontSize: "0.65rem" }}
              />
              <Typography variant="caption" color="text.secondary">
                {instances.length} {instances.length === 1 ? "block" : "blocks"}
              </Typography>
            </Box>
            <Box sx={rowsSx}>
              {instances.map((instance, index) => (
                <Box key={`${name}-${index}`} sx={rowSx}>
                  <Typography sx={rowLabelSx}>{`${name}[${index}]`}</Typography>
                  <Box sx={{ flex: 1, minWidth: 0 }}>
                    <ProviderConfigurationEditor
                      schema={nested.block}
                      value={instance}
                      referenceOptions={referenceOptions}
                      depth={depth + 1}
                      onChange={(next) => {
                        const updated = [...instances];
                        updated[index] = next;
                        onChange({ ...value, blocks: { ...value.blocks, [name]: updated } });
                      }}
                    />
                  </Box>
                  <RemoveButton
                    label={`Remove ${name} ${index + 1}`}
                    onClick={() => onChange({ ...value, blocks: { ...value.blocks, [name]: instances.filter((_, itemIndex) => itemIndex !== index) } })}
                  />
                </Box>
              ))}
            </Box>
            <AddButton
              disabled={!!nested.maxItems && instances.length >= nested.maxItems}
              onClick={() => onChange({ ...value, blocks: { ...value.blocks, [name]: [...instances, emptyProviderConfiguration()] } })}
            >
              Add {name}
            </AddButton>
          </Box>
        );
      })}

      {orderedEntries.length === 0 && (
        <Typography variant="caption" color="text.secondary">No configurable fields.</Typography>
      )}
    </Box>
  );
}
