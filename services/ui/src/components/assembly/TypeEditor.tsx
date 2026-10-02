"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import TextField from "@mui/material/TextField";
import FormControl from "@mui/material/FormControl";
import InputLabel from "@mui/material/InputLabel";
import Select from "@mui/material/Select";
import MenuItem from "@mui/material/MenuItem";
import IconButton from "@mui/material/IconButton";
import Button from "@mui/material/Button";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import Typography from "@mui/material/Typography";
import Accordion from "@mui/material/Accordion";
import AccordionDetails from "@mui/material/AccordionDetails";
import AccordionSummary from "@mui/material/AccordionSummary";
import AddIcon from "@mui/icons-material/Add";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import type { ObjectTypeSpec, TypeSpec } from "./types";
import { emptyValueSpec, valueSpecForType } from "./typeSpec";
import ValueEditor from "./ValueEditor";

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

function emptyObject(): ObjectTypeSpec {
  return { kind: "object", attributes: [] };
}

function attributeHasDefault(attribute: ObjectTypeSpec["attributes"][number]): boolean {
  return attribute.hasDefault ?? attribute.default !== undefined;
}

function emptyType(kind: TypeSpec["kind"]): TypeSpec {
  if (kind === "string" || kind === "number" || kind === "bool" || kind === "any") return { kind };
  if (kind === "list" || kind === "set") return { kind, element: { kind: "string" } };
  if (kind === "map") return { kind, element: { kind: "string" } };
  if (kind === "object") return emptyObject();
  return { kind: "tuple", elements: [] };
}

interface Props {
  value: TypeSpec;
  onChange: (t: TypeSpec) => void;
}

export default function TypeEditor({ value, onChange }: Props) {
  return <RecursiveTypeEditor value={value} onChange={onChange} />;
}

function RecursiveTypeEditor({ value, onChange }: Props) {
  const kindId = React.useId();
  const objectId = React.useId().replaceAll(":", "");
  const [objectExpanded, setObjectExpanded] = React.useState(true);

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <FormControl size="small" fullWidth variant="filled">
        <InputLabel id={kindId}>Type</InputLabel>
        <Select
          disableUnderline
          labelId={kindId}
          value={value.kind}
          onChange={(e) => onChange(emptyType(e.target.value as TypeSpec["kind"]))}
          sx={{ borderRadius: 1 }}
        >
          <MenuItem value="string">string</MenuItem>
          <MenuItem value="number">number</MenuItem>
          <MenuItem value="bool">bool</MenuItem>
          <MenuItem value="any">any</MenuItem>
          <MenuItem value="list">list</MenuItem>
          <MenuItem value="set">set</MenuItem>
          <MenuItem value="map">map</MenuItem>
          <MenuItem value="object">object</MenuItem>
          <MenuItem value="tuple">tuple</MenuItem>
        </Select>
      </FormControl>

      {(value.kind === "list" || value.kind === "set") && (
        <Box sx={nestedBranchSx}>
          <Typography variant="caption" color="text.secondary" sx={{ display: "block", mb: 0.75 }}>
            {value.kind === "list" ? "List" : "Set"} elements
          </Typography>
          <RecursiveTypeEditor value={value.element} onChange={(element) => onChange({ ...value, element })} />
        </Box>
      )}

      {value.kind === "map" && (
        <Box sx={nestedBranchSx}>
          <Typography variant="caption" color="text.secondary" sx={{ display: "block", mb: 0.75 }}>
            Map values
          </Typography>
          <RecursiveTypeEditor value={value.element} onChange={(element) => onChange({ ...value, element })} />
        </Box>
      )}

      {value.kind === "object" && (
        <Box>
          <Accordion
            disableGutters
            expanded={objectExpanded}
            onChange={(_, expanded) => setObjectExpanded(expanded)}
            sx={nestedAccordionSx}
          >
            <AccordionSummary expandIcon={<ExpandMoreIcon />} aria-controls={`${objectId}-object-definition`}>
              <Typography noWrap>
                Object definition ({value.attributes.length} {value.attributes.length === 1 ? "attribute" : "attributes"})
              </Typography>
            </AccordionSummary>
            <AccordionDetails id={`${objectId}-object-definition`}>
              <ObjectAttributesEditor spec={value} onChange={onChange} />
            </AccordionDetails>
          </Accordion>
        </Box>
      )}

      {value.kind === "tuple" && (
        <Box sx={{ ...nestedBranchSx, display: "flex", flexDirection: "column", gap: 1 }}>
          <Typography variant="caption" color="text.secondary">
            Elements, in order:
          </Typography>
          {value.elements.map((el, i) => (
            <Box key={i} sx={{ display: "flex", alignItems: "flex-start", gap: 0.5 }}>
              <Box sx={{ flex: 1 }}>
                <RecursiveTypeEditor
                  value={el}
                  onChange={(t) => {
                    const elements = [...value.elements];
                    elements[i] = t;
                    onChange({ ...value, elements });
                  }}
                />
              </Box>
              <IconButton
                size="small"
                onClick={() => onChange({ ...value, elements: value.elements.filter((_, idx) => idx !== i) })}
                aria-label="Remove element"
              >
                <DeleteOutlineIcon fontSize="small" />
              </IconButton>
            </Box>
          ))}
          <Button
            size="small"
            startIcon={<AddIcon />}
            onClick={() => onChange({ ...value, elements: [...value.elements, { kind: "string" }] })}
          >
            Add element
          </Button>
        </Box>
      )}
    </Box>
  );
}

function ObjectAttributesEditor({
  spec,
  onChange,
}: {
  spec: ObjectTypeSpec;
  onChange: (spec: ObjectTypeSpec) => void;
}) {
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      {spec.attributes.map((attr, i) => (
        <Box
          key={i}
          sx={{ pb: 1.25, mb: 1.25, borderBottom: "1px solid", borderColor: "divider", "&:last-of-type": { mb: 0 } }}
        >
          <Box sx={{ display: "flex", alignItems: "center", gap: 1.25, mb: 1 }}>
            <TextField
              variant="filled"
              size="small"
              label="Name"
              value={attr.name}
              onChange={(e) => {
                const attributes = [...spec.attributes];
                attributes[i] = { ...attr, name: e.target.value };
                onChange({ ...spec, attributes });
              }}
              sx={{ ...filledFieldSx, flex: 1, minWidth: 0 }}
            />
            <FormControlLabel
              sx={{ m: 0, flexShrink: 0 }}
              control={
                <Checkbox
                  size="small"
                  checked={!!attr.optional}
                  onChange={(e) => {
                    const attributes = [...spec.attributes];
                    attributes[i] = { ...attr, optional: e.target.checked, hasDefault: e.target.checked ? attr.hasDefault : false };
                    onChange({ ...spec, attributes });
                  }}
                />
              }
              label={<Typography variant="caption">Optional</Typography>}
            />
            <IconButton
              size="small"
              onClick={() => onChange({ ...spec, attributes: spec.attributes.filter((_, idx) => idx !== i) })}
              aria-label="Remove attribute"
            >
              <DeleteOutlineIcon fontSize="small" />
            </IconButton>
          </Box>
          <RecursiveTypeEditor
            value={attr.type}
            onChange={(type) => {
              const attributes = [...spec.attributes];
              attributes[i] = {
                ...attr,
                type,
                default: attributeHasDefault(attr) ? emptyValueSpec(type) : undefined,
              };
              onChange({ ...spec, attributes });
            }}
          />
          {attr.optional && (
            <Box sx={{ ...nestedBranchSx, mt: 1 }}>
              <FormControlLabel
                control={
                  <Checkbox
                    size="small"
                    checked={attributeHasDefault(attr)}
                    onChange={(e) => {
                      const attributes = [...spec.attributes];
                      attributes[i] = {
                        ...attr,
                        hasDefault: e.target.checked,
                        default: e.target.checked ? valueSpecForType(attr.type, attr.default) : undefined,
                      };
                      onChange({ ...spec, attributes });
                    }}
                  />
                }
                label={<Typography variant="caption">Has a default value</Typography>}
              />
              {attributeHasDefault(attr) && (
                <ValueEditor
                  type={attr.type}
                  value={valueSpecForType(attr.type, attr.default)}
                  onChange={(defaultValue) => {
                    const attributes = [...spec.attributes];
                    attributes[i] = { ...attr, hasDefault: true, default: defaultValue };
                    onChange({ ...spec, attributes });
                  }}
                />
              )}
            </Box>
          )}
        </Box>
      ))}
      <Button
        size="small"
        startIcon={<AddIcon />}
        onClick={() => onChange({ ...spec, attributes: [...spec.attributes, { name: "", type: { kind: "string" } }] })}
      >
        Add attribute
      </Button>
    </Box>
  );
}
