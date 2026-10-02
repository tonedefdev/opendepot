"use client";

import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import Divider from "@mui/material/Divider";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import AddIcon from "@mui/icons-material/Add";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import type { ProviderSchemaBlock } from "@/lib/api";
import { renderTypeCompact } from "@/lib/ctyType";
import FieldEditor from "./FieldEditor";
import type { ProviderConfiguration, ReferenceOption } from "./types";

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
          return (
            <Box key={name} sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
              <Box sx={{ display: "flex", alignItems: "center", gap: 0.75 }}>
                <Typography variant="body2" fontWeight={600}>{name}</Typography>
                <Chip
                  label={attribute.required ? "required" : "optional"}
                  size="small"
                  color={attribute.required ? "error" : "default"}
                  variant="outlined"
                  sx={{ height: 18, fontSize: "0.65rem" }}
                />
                <Chip
                  label={renderTypeCompact(attribute.type)}
                  size="small"
                  variant="outlined"
                  sx={{ height: 18, fontSize: "0.65rem", ml: "auto" }}
                />
              </Box>
              <FieldEditor
                type={attribute.type}
                value={value.arguments[name]}
                referenceOptions={referenceOptions}
                placeholder={attribute.required ? "Required" : "Optional"}
                onChange={(field) => onChange({ ...value, arguments: { ...value.arguments, [name]: field } })}
              />
            </Box>
          );
        }

        const nested = schema.blocks?.[name];
        if (!nested) return null;
        const instances = value.blocks[name] ?? [];
        return (
          <Box key={name} sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
            {depth > 0 && <Divider />}
            <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 1 }}>
              <Box sx={{ display: "flex", alignItems: "center", gap: 0.75 }}>
                <Typography variant="body2" fontWeight={600}>{name}</Typography>
                <Chip
                  label={(nested.minItems ?? 0) > 0 ? "required block" : "optional block"}
                  size="small"
                  color={(nested.minItems ?? 0) > 0 ? "error" : "default"}
                  variant="outlined"
                  sx={{ height: 18, fontSize: "0.65rem" }}
                />
              </Box>
              <Button
                size="small"
                startIcon={<AddIcon />}
                disabled={!!nested.maxItems && instances.length >= nested.maxItems}
                onClick={() => onChange({ ...value, blocks: { ...value.blocks, [name]: [...instances, emptyProviderConfiguration()] } })}
              >
                Add
              </Button>
            </Box>
            {instances.map((instance, index) => (
              <Box key={`${name}-${index}`} sx={{ display: "flex", alignItems: "flex-start", gap: 0.5 }}>
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
                <IconButton
                  size="small"
                  aria-label={`Remove ${name} ${index + 1}`}
                  onClick={() => onChange({ ...value, blocks: { ...value.blocks, [name]: instances.filter((_, itemIndex) => itemIndex !== index) } })}
                >
                  <DeleteOutlineIcon fontSize="small" />
                </IconButton>
              </Box>
            ))}
          </Box>
        );
      })}

      {orderedEntries.length === 0 && (
        <Typography variant="caption" color="text.secondary">No configurable fields.</Typography>
      )}
    </Box>
  );
}
