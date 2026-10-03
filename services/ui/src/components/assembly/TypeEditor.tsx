"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import TextField from "@mui/material/TextField";
import FormControl from "@mui/material/FormControl";
import Select from "@mui/material/Select";
import MenuItem from "@mui/material/MenuItem";
import Chip from "@mui/material/Chip";
import Collapse from "@mui/material/Collapse";
import Typography from "@mui/material/Typography";
import type { ObjectAttributeSpec, ObjectTypeSpec, TypeSpec } from "./types";
import { emptyValueSpec, valueSpecForType } from "./typeSpec";
import { ValueRow } from "./ValueEditor";
import {
  AddButton,
  RemoveButton,
  RowSpacer,
  RowToggle,
  branchSx,
  childBranchSx,
  compactFieldSx,
  monoSx,
  nameColumnSx,
  rowActionsSx,
  rowLabelSx,
  rowSx,
  rowsSx,
} from "./editorRows";

const TYPE_KINDS: TypeSpec["kind"][] = ["string", "number", "bool", "any", "list", "set", "map", "object", "tuple"];

interface ChainLink {
  spec: TypeSpec;
  set: (t: TypeSpec) => void;
}

type CollectionTypeSpec = Extract<TypeSpec, { element: TypeSpec }>;

function isCollection(spec: TypeSpec): spec is CollectionTypeSpec {
  return spec.kind === "list" || spec.kind === "set" || spec.kind === "map";
}

/** Flattens `map(list(object(...)))` into one link per constructor so it can render as a single inline row. */
function typeChain(value: TypeSpec, onChange: (t: TypeSpec) => void): ChainLink[] {
  const links: ChainLink[] = [{ spec: value, set: onChange }];
  let link = links[0];
  while (isCollection(link.spec)) {
    const parent = link.spec;
    const setParent = link.set;
    link = { spec: parent.element, set: (element) => setParent({ ...parent, element }) };
    links.push(link);
  }
  return links;
}

function hasBody(spec: TypeSpec): boolean {
  return spec.kind === "object" || spec.kind === "tuple";
}

function bodySummary(spec: TypeSpec): string {
  if (spec.kind === "object") return `${spec.attributes.length} ${spec.attributes.length === 1 ? "attribute" : "attributes"}`;
  if (spec.kind === "tuple") return `${spec.elements.length} ${spec.elements.length === 1 ? "element" : "elements"}`;
  return "";
}

function emptyObject(): ObjectTypeSpec {
  return { kind: "object", attributes: [] };
}

function attributeHasDefault(attribute: ObjectAttributeSpec): boolean {
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
  const links = typeChain(value, onChange);
  const terminal = links[links.length - 1];

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75 }}>
      <Typography variant="subtitle2">Type</Typography>
      <TypeChain links={links} label="Type" />
      {hasBody(terminal.spec) && (
        <Box sx={branchSx}>
          <TypeBody spec={terminal.spec} onChange={terminal.set} />
        </Box>
      )}
    </Box>
  );
}

function KindSelect({ kind, label, onChange }: { kind: TypeSpec["kind"]; label: string; onChange: (t: TypeSpec) => void }) {
  return (
    <FormControl size="small" variant="filled" hiddenLabel sx={{ minWidth: 96 }}>
      <Select
        disableUnderline
        value={kind}
        onChange={(e) => onChange(emptyType(e.target.value as TypeSpec["kind"]))}
        inputProps={{ "aria-label": label }}
        sx={{ borderRadius: 1, "& .MuiSelect-select": { ...monoSx, py: 0.75 } }}
      >
        {TYPE_KINDS.map((k) => (
          <MenuItem key={k} value={k} sx={monoSx}>
            {k}
          </MenuItem>
        ))}
      </Select>
    </FormControl>
  );
}

function TypeChain({ links, label }: { links: ChainLink[]; label: string }) {
  return (
    <Box sx={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: 0.75, minWidth: 0 }}>
      {links.map((link, i) => (
        <React.Fragment key={i}>
          {i > 0 && (
            <Typography variant="caption" color="text.secondary">
              of
            </Typography>
          )}
          <KindSelect
            kind={link.spec.kind}
            label={i === 0 ? label : `${label} ${links[i - 1].spec.kind} ${links[i - 1].spec.kind === "map" ? "value" : "element"}`}
            onChange={link.set}
          />
        </React.Fragment>
      ))}
    </Box>
  );
}

function TypeBody({ spec, onChange }: { spec: TypeSpec; onChange: (t: TypeSpec) => void }) {
  if (spec.kind === "object") return <ObjectBody spec={spec} onChange={onChange} />;
  if (spec.kind === "tuple") {
    return (
      <Box sx={rowsSx}>
        {spec.elements.map((element, i) => (
          <TypeRow
            key={i}
            label={`Element ${i}`}
            name={<Typography sx={rowLabelSx}>[{i}]</Typography>}
            type={element}
            onTypeChange={(t) => {
              const elements = [...spec.elements];
              elements[i] = t;
              onChange({ ...spec, elements });
            }}
            actions={
              <RemoveButton
                label="Remove element"
                onClick={() => onChange({ ...spec, elements: spec.elements.filter((_, idx) => idx !== i) })}
              />
            }
          />
        ))}
        <AddButton onClick={() => onChange({ ...spec, elements: [...spec.elements, { kind: "string" }] })}>Add element</AddButton>
      </Box>
    );
  }
  return null;
}

function ObjectBody({ spec, onChange }: { spec: ObjectTypeSpec; onChange: (spec: ObjectTypeSpec) => void }) {
  const setAttribute = (i: number, attribute: ObjectAttributeSpec) => {
    const attributes = [...spec.attributes];
    attributes[i] = attribute;
    onChange({ ...spec, attributes });
  };

  return (
    <Box sx={rowsSx}>
      {spec.attributes.map((attr, i) => {
        const hasDefault = attributeHasDefault(attr);
        return (
          <TypeRow
            key={i}
            label={attr.name || "attribute"}
            name={
              <TextField
                variant="filled"
                size="small"
                hiddenLabel
                placeholder="name"
                value={attr.name}
                onChange={(e) => setAttribute(i, { ...attr, name: e.target.value })}
                slotProps={{ input: { disableUnderline: true }, htmlInput: { "aria-label": "Attribute name" } }}
                sx={{ ...compactFieldSx, ...nameColumnSx }}
              />
            }
            type={attr.type}
            onTypeChange={(type) =>
              setAttribute(i, { ...attr, type, default: hasDefault ? emptyValueSpec(type) : undefined })
            }
            actions={
              <>
                <ToggleChip
                  label="optional"
                  selected={!!attr.optional}
                  onToggle={() =>
                    setAttribute(i, { ...attr, optional: !attr.optional, hasDefault: attr.optional ? false : attr.hasDefault })
                  }
                />
                {attr.optional && (
                  <ToggleChip
                    label="default"
                    selected={hasDefault}
                    onToggle={() =>
                      setAttribute(i, {
                        ...attr,
                        hasDefault: !hasDefault,
                        default: hasDefault ? undefined : valueSpecForType(attr.type, attr.default),
                      })
                    }
                  />
                )}
                <RemoveButton
                  label="Remove attribute"
                  onClick={() => onChange({ ...spec, attributes: spec.attributes.filter((_, idx) => idx !== i) })}
                />
              </>
            }
          >
            {attr.optional && hasDefault && (
              <ValueRow
                label={<Typography sx={{ ...rowLabelSx, fontStyle: "italic" }}>default</Typography>}
                toggleLabel={`${attr.name || "attribute"} default`}
                type={attr.type}
                value={valueSpecForType(attr.type, attr.default)}
                onChange={(defaultValue) => setAttribute(i, { ...attr, hasDefault: true, default: defaultValue })}
                alignToValueColumn
              />
            )}
          </TypeRow>
        );
      })}
      <AddButton onClick={() => onChange({ ...spec, attributes: [...spec.attributes, { name: "", type: { kind: "string" } }] })}>
        Add attribute
      </AddButton>
    </Box>
  );
}

function TypeRow({
  label,
  name,
  type,
  onTypeChange,
  actions,
  children,
}: {
  label: string;
  name: React.ReactNode;
  type: TypeSpec;
  onTypeChange: (t: TypeSpec) => void;
  actions: React.ReactNode;
  children?: React.ReactNode;
}) {
  const [expanded, setExpanded] = React.useState(true);
  const links = typeChain(type, onTypeChange);
  const terminal = links[links.length - 1];
  const nested = hasBody(terminal.spec);

  return (
    <Box>
      <Box sx={rowSx}>
        {nested ? (
          <RowToggle label={`${label} definition`} expanded={expanded} onToggle={() => setExpanded((v) => !v)} />
        ) : (
          <RowSpacer />
        )}
        {name}
        <TypeChain links={links} label={`${label} type`} />
        {nested && !expanded && (
          <Typography variant="caption" color="text.secondary">
            {bodySummary(terminal.spec)}
          </Typography>
        )}
        <Box sx={rowActionsSx}>{actions}</Box>
      </Box>
      {nested ? (
        <Collapse in={expanded} unmountOnExit>
          <Box sx={childBranchSx}>
            <TypeBody spec={terminal.spec} onChange={terminal.set} />
            {children}
          </Box>
        </Collapse>
      ) : (
        children && <Box sx={childBranchSx}>{children}</Box>
      )}
    </Box>
  );
}

function ToggleChip({ label, selected, onToggle }: { label: string; selected: boolean; onToggle: () => void }) {
  return (
    <Chip
      size="small"
      label={label}
      color={selected ? "primary" : "default"}
      variant={selected ? "filled" : "outlined"}
      onClick={onToggle}
      aria-pressed={selected}
      sx={{ height: 22, fontSize: "0.7rem" }}
    />
  );
}
