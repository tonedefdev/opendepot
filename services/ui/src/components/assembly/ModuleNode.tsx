"use client";

import * as React from "react";
import { useState } from "react";
import { Handle, Position, type NodeProps } from "reactflow";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Chip from "@mui/material/Chip";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import CircularProgress from "@mui/material/CircularProgress";
import TextField from "@mui/material/TextField";
import Popover from "@mui/material/Popover";
import FormControl from "@mui/material/FormControl";
import InputLabel from "@mui/material/InputLabel";
import Select from "@mui/material/Select";
import MenuItem from "@mui/material/MenuItem";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import ExpandLessIcon from "@mui/icons-material/ExpandLess";
import RepeatIcon from "@mui/icons-material/Repeat";
import CloseIcon from "@mui/icons-material/Close";
import WidgetsIcon from "@mui/icons-material/Widgets";
import WidgetsOutlinedIcon from "@mui/icons-material/WidgetsOutlined";
import InfoOutlinedIcon from "@mui/icons-material/InfoOutlined";
import ArrowForwardIcon from "@mui/icons-material/ArrowForward";
import { useColorScheme } from "@mui/material/styles";
import Link from "next/link";
import type { ContractOutput, ContractRequiredProvider, ContractVariable } from "@/lib/api";
import { GRADE_COLOR, renderTypeCompact, typesRoughlyCompatible } from "@/lib/ctyType";
import type { FieldValue, ModuleInputValue, Multiplicity, ProviderBinding, ProviderOption, ReferenceOption, VariableOption } from "./types";
import { forEachShapeFromTypeSpec, isForEachCompatible } from "./typeSpec";
import InputsModal from "./InputsModal";
import ProvidersModal from "./ProvidersModal";
import FieldEditor from "./FieldEditor";
import { areAssemblyNodePropsEqual } from "./nodeMemo";

export type { FieldValue, Multiplicity, ReferenceOption } from "./types";

export interface ModuleNodeData {
  namespace: string;
  name: string;
  system?: string;
  version: string;
  instanceName: string;
  instanceNameError: string | null;
  grade: "full" | "partial" | "unsupported" | null;
  variables: ContractVariable[];
  outputs: ContractOutput[];
  requiredProviders: ContractRequiredProvider[];
  providerOptions: ProviderOption[];
  providerBindings: Record<string, ProviderBinding>;
  values: Record<string, ModuleInputValue>;
  optionalFieldVisibility: Record<string, boolean>;
  fieldErrors: Record<string, string>;
  diagnostics?: string[];
  referenceOptions: ReferenceOption[];
  variableOptions: VariableOption[];
  multiplicity: Multiplicity;
  warnings?: string[];
  loading: boolean;
  error: string | null;
  onRemove: () => void;
  onRenameInstance: (name: string) => void;
  onFieldChange: (variableName: string, value: ModuleInputValue) => void;
  onOptionalFieldVisibilityChange: (path: string, visible: boolean) => void;
  onMultiplicityChange: (multiplicity: Multiplicity) => void;
  onProviderBindingChange: (localName: string, providerNodeId: string) => void;
}

const NODE_WIDTH = 300;

/** Outputs shown before collapsing into a "show more" toggle on the card. */
const OUTPUTS_COLLAPSED_COUNT = 5;

/** Normalizes a version string to have exactly one leading "v" for display. */
function displayVersion(v: string): string {
  if (!v) return "";
  return v.startsWith("v") ? v : `v${v}`;
}

/** This node's own each.key/each.value/count.index options, offered as plain
 * literal text in its fields' Autocomplete — never a cross-node reference.
 * When for_each's source variable is map(object({...})), each.value.<attr>
 * is also offered for every declared attribute, since the shape is fully
 * known once a variable is picked. */
function metaOptionsFor(multiplicity: Multiplicity, variableOptions: VariableOption[]): string[] {
  if (multiplicity.kind === "count") {
    return ["count.index"];
  }

  if (multiplicity.kind === "for_each") {
    const source = variableOptions.find((v) => v.nodeId === multiplicity.variableNodeId);
    if (!source) {
      return ["each.key", "each.value"];
    }

    const { attributes } = forEachShapeFromTypeSpec(source.type);
    return ["each.key", "each.value", ...attributes.map((a) => `each.value.${a}`)];
  }

  return [];
}

function ModuleNode({ data }: NodeProps<ModuleNodeData>) {
  const {
    namespace,
    name,
    version,
    instanceName,
    instanceNameError,
    grade,
    variables,
    outputs,
    requiredProviders,
    providerOptions,
    providerBindings,
    values,
    optionalFieldVisibility,
    fieldErrors,
    diagnostics = [],
    referenceOptions,
    variableOptions,
    multiplicity,
    warnings,
    loading,
    error,
    onRemove,
    onRenameInstance,
    onFieldChange,
    onOptionalFieldVisibilityChange,
    onMultiplicityChange,
    onProviderBindingChange,
  } = data;

  const reactId = React.useId();
  const [inputsOpen, setInputsOpen] = useState(false);
  const [providersOpen, setProvidersOpen] = useState(false);
  const [outputsExpanded, setOutputsExpanded] = useState(false);
  const [repeatAnchor, setRepeatAnchor] = useState<HTMLElement | null>(null);
  const identityTextRef = React.useRef<HTMLSpanElement>(null);
  const [identityTruncated, setIdentityTruncated] = useState(false);
  const { mode, systemMode } = useColorScheme();
  const resolvedMode = mode === "system" ? systemMode : mode;
  const WidgetIcon = resolvedMode === "light" ? WidgetsIcon : WidgetsOutlinedIcon;
  const errorCount = Object.keys(fieldErrors).length;
  const configuredProviderCount = requiredProviders.filter(
    (provider) => providerBindings[provider.localName]?.providerNodeId,
  ).length;
  const hiddenOutputs = outputsExpanded ? [] : outputs.slice(OUTPUTS_COLLAPSED_COUNT);
  const visibleOutputs = outputsExpanded ? outputs : outputs.slice(0, OUTPUTS_COLLAPSED_COUNT);
  const isRepeated = multiplicity.kind !== "none";
  const moduleDetailsHref = `/${encodeURIComponent(namespace)}/module/${encodeURIComponent(name)}`;
  const moduleIdentity = `${namespace}/${name} · ${displayVersion(version)}`;

  React.useEffect(() => {
    const identityText = identityTextRef.current;
    if (!identityText) return;

    const updateTruncation = () => setIdentityTruncated(identityText.scrollWidth > identityText.clientWidth);
    updateTruncation();

    const resizeObserver = new ResizeObserver(updateTruncation);
    resizeObserver.observe(identityText);

    return () => resizeObserver.disconnect();
  }, [moduleIdentity]);

  // count's conditional mode expects a bool — when a reference is wired in
  // that resolves to some other type, flag it both on the popover's field
  // and on the "count" badge itself so the problem is visible without
  // opening the popover.
  const countMatched =
    multiplicity.kind === "count" && multiplicity.expr.mode === "reference"
      ? referenceOptions.find(
          (o) => o.nodeId === multiplicity.expr.refNodeId && o.output === (multiplicity.expr.refOutput ?? ""),
        )
      : undefined;
  const countTypeError =
    multiplicity.kind === "count" &&
    multiplicity.mode === "conditional" &&
    countMatched &&
    !typesRoughlyCompatible(countMatched.type, "bool")
      ? `${countMatched.label} is ${renderTypeCompact(countMatched.type)}, not bool`
      : undefined;

  // The stacked-card shadow's outer drop-shadow layer needs a much lighter
  // touch in light mode — the dark-mode rgba(0,0,0,0.4) reads as a harsh
  // smudge against a light background instead of a subtle stacked-card cue.
  // Dark mode can reuse background.default/divider directly because
  // background.default (#0d1117) is darker than the node's own paper
  // surface (#161b22). Light mode's background.default is pure white
  // (#ffffff) — *lighter* than paper (#f6f8fa) — so reusing it here made the
  // stacked card read as a bright white slab against the node body/header.
  // A fixed, solid grey fill fixes that on its own — the extra 1px outline
  // layer isn't needed once the fill itself reads as clearly darker.
  const stackedCardShadow =
    resolvedMode === "light"
      ? "4px 4px 0 0 #d0d7de, 0 2px 6px rgba(0,0,0,0.15)"
      : "4px 4px 0 0 var(--mui-palette-background-default), 4px 4px 0 1px var(--mui-palette-divider), 0 2px 6px rgba(0,0,0,0.4)";

  return (
    <Box
      sx={{
        width: NODE_WIDTH,
        borderRadius: 2,
        border: "1px solid",
        borderColor: error || instanceNameError || diagnostics.length > 0 ? "#f85149" : "transparent",
        bgcolor: "background.paper",
        // A repeated (count/for_each) module reads as "N instances" via a
        // layered box-shadow stacked-card look rather than extra DOM nodes —
        // percentage/inset positioning doesn't work reliably against a
        // parent whose height is itself auto (content-driven).
        boxShadow: isRepeated ? stackedCardShadow : 3,
        overflow: "hidden",
      }}
    >
      <Box sx={{ px: 1, py: 0.75, bgcolor: "action.hover" }}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
          <WidgetIcon sx={{ fontSize: 16, opacity: 0.7, color: "#03deb8" }} />
          <Typography variant="caption" color="text.secondary">
            module.
          </Typography>
          <TextField
            variant="standard"
            size="small"
            value={instanceName}
            onChange={(e) => onRenameInstance(e.target.value)}
            error={!!instanceNameError}
            sx={{ flex: 1, minWidth: 0, "& input": { fontWeight: 700, fontSize: "0.8125rem" } }}
          />
          <IconButton size="small" onClick={onRemove} aria-label={`Remove ${instanceName}`}>
            <CloseIcon sx={{ fontSize: 14 }} />
          </IconButton>
        </Box>
        <Box sx={{ display: "flex", alignItems: "center", gap: 0.5, mt: 0.5 }}>
          <Tooltip title={countTypeError ?? "Repeat this module with count or for_each"}>
            <Chip
              size="small"
              icon={isRepeated ? <RepeatIcon sx={{ fontSize: "12px !important" }} /> : undefined}
              label={multiplicity.kind === "none" ? "single" : multiplicity.kind}
              color={countTypeError ? "error" : isRepeated ? "info" : "default"}
              variant={isRepeated || countTypeError ? "filled" : "outlined"}
              onClick={(e) => setRepeatAnchor(e.currentTarget)}
              sx={{ height: 18, fontSize: "0.65rem", cursor: "pointer" }}
            />
          </Tooltip>
          {grade && (
            <Chip
              size="small"
              label={grade}
              color={GRADE_COLOR[grade] ?? "default"}
              variant="outlined"
              sx={{ height: 18, fontSize: "0.65rem" }}
            />
          )}
        </Box>
        <Tooltip
          title={identityTruncated ? moduleIdentity : ""}
          placement="top"
          enterDelay={300}
          disableInteractive
          slotProps={{ tooltip: { sx: { maxWidth: 360 } } }}
        >
          <Box
            component={Link}
            href={moduleDetailsHref}
            className="nodrag nopan"
            aria-label={`Open ${namespace}/${name} module details`}
            sx={{
              display: "flex",
              alignItems: "center",
              gap: 0.5,
              minWidth: 0,
              color: "text.secondary",
              textDecoration: "none",
              cursor: "pointer",
              "& .module-details-arrow": { display: "none" },
              "&:hover, &:focus-visible": { color: "text.primary" },
              "&:hover .module-details-info, &:focus-visible .module-details-info": { display: "none" },
              "&:hover .module-details-arrow, &:focus-visible .module-details-arrow": { display: "block" },
              "&:focus-visible": { outline: "2px solid", outlineColor: "primary.main", outlineOffset: 2, borderRadius: 0.5 },
            }}
          >
            <Typography ref={identityTextRef} variant="caption" noWrap sx={{ minWidth: 0, flex: 1, color: "inherit" }}>
              {moduleIdentity}
            </Typography>
            <InfoOutlinedIcon className="module-details-info" sx={{ fontSize: 14, flexShrink: 0 }} />
            <ArrowForwardIcon className="module-details-arrow" sx={{ fontSize: 14, flexShrink: 0 }} />
          </Box>
        </Tooltip>
        {instanceNameError && (
          <Typography variant="caption" sx={{ color: "#f85149", display: "block" }}>
            {instanceNameError}
          </Typography>
        )}
      </Box>

      <Popover
        open={!!repeatAnchor}
        anchorEl={repeatAnchor}
        onClose={() => setRepeatAnchor(null)}
        anchorOrigin={{ vertical: "bottom", horizontal: "left" }}
      >
        <Box sx={{ p: 1.5, width: 280, display: "flex", flexDirection: "column", gap: 1 }}>
          <FormControl size="small" fullWidth>
            <InputLabel id={`${reactId}-multiplicity-kind`}>Repeat</InputLabel>
            <Select
              labelId={`${reactId}-multiplicity-kind`}
              label="Repeat"
              value={multiplicity.kind}
              onChange={(e) => {
                const kind = e.target.value as Multiplicity["kind"];
                if (kind === "none") {
                  onMultiplicityChange({ kind: "none" });
                } else if (kind === "count") {
                  onMultiplicityChange({ kind: "count", mode: "fixed", expr: { mode: "literal", literal: "1" } });
                } else {
                  onMultiplicityChange({ kind: "for_each", variableNodeId: null });
                }
              }}
            >
              <MenuItem value="none">Single (default)</MenuItem>
              <MenuItem value="count">count</MenuItem>
              <MenuItem value="for_each">for_each</MenuItem>
            </Select>
          </FormControl>

          {multiplicity.kind === "count" &&
            (() => {
              const matched = countMatched;
              const typeError = countTypeError;

              return (
                <>
                  <FormControl size="small" fullWidth>
                    <InputLabel id={`${reactId}-count-mode`}>Mode</InputLabel>
                    <Select
                      labelId={`${reactId}-count-mode`}
                      label="Mode"
                      value={multiplicity.mode}
                      onChange={(e) => {
                        const mode = e.target.value as "fixed" | "conditional";
                        onMultiplicityChange({
                          kind: "count",
                          mode,
                          expr: { mode: "literal", literal: mode === "conditional" ? "" : "1" },
                        });
                      }}
                    >
                      <MenuItem value="fixed">Fixed number</MenuItem>
                      <MenuItem value="conditional">Conditional (create or not)</MenuItem>
                    </Select>
                  </FormControl>
                  <Typography variant="caption" color="text.secondary">
                    {multiplicity.mode === "conditional"
                      ? "Condition — creates 1 instance when true, 0 when false:"
                      : "Number of instances:"}
                  </Typography>
                  <FieldEditor
                    type={multiplicity.mode === "conditional" ? "bool" : "number"}
                    value={multiplicity.expr}
                    referenceOptions={referenceOptions}
                    placeholder={multiplicity.mode === "conditional" ? "true or false" : "e.g. 3"}
                    error={typeError}
                    onChange={(expr) => onMultiplicityChange({ ...multiplicity, expr })}
                  />
                  {matched?.sourceKind === "module" && (
                    <Typography variant="caption" sx={{ color: "warning.main" }}>
                      A count condition needs to know its length at plan time — referencing another
                      module&apos;s output here can fail with &quot;value depends on resource
                      attributes that cannot be determined until apply&quot; unless that
                      output is itself statically known.
                    </Typography>
                  )}
                </>
              );
            })()}

          {multiplicity.kind === "for_each" && (
            <>
              {variableOptions.length === 0 ? (
                <Typography variant="caption" color="text.secondary">
                  Add a Variable first — for_each is locked to a known variable, since its
                  key set must be provably known at plan time.
                </Typography>
              ) : (
                <>
                  <FormControl size="small" fullWidth>
                    <InputLabel id={`${reactId}-foreach-variable`}>Variable</InputLabel>
                    <Select
                      labelId={`${reactId}-foreach-variable`}
                      label="Variable"
                      value={multiplicity.variableNodeId ?? ""}
                      onChange={(e) =>
                        onMultiplicityChange({ kind: "for_each", variableNodeId: (e.target.value as string) || null })
                      }
                    >
                      {variableOptions.map((v) => (
                        <MenuItem key={v.nodeId} value={v.nodeId} disabled={!isForEachCompatible(v.type)}>
                          var.{v.name}
                          {!isForEachCompatible(v.type) ? " (not a list/set/map)" : ""}
                        </MenuItem>
                      ))}
                    </Select>
                  </FormControl>
                  {(() => {
                    const source = variableOptions.find((v) => v.nodeId === multiplicity.variableNodeId);
                    if (!source) {
                      return (
                        <Typography variant="caption" color="text.secondary">
                          Pick a variable to derive each.key / each.value.
                        </Typography>
                      );
                    }

                    const { shape, attributes } = forEachShapeFromTypeSpec(source.type);
                    return (
                      <Typography variant="caption" color="text.secondary">
                        {shape === "set" && "each.key and each.value will both be the element value."}
                        {shape === "map_scalar" && "each.key is the map key; each.value is the map value."}
                        {shape === "map_of_object" &&
                          `each.key is the map key; each.value is the object (${attributes.join(", ")}).`}
                        {shape === "unknown" && "This variable's type isn't list/set/map — each.key/each.value may not resolve."}
                      </Typography>
                    );
                  })()}
                </>
              )}
            </>
          )}
        </Box>
      </Popover>

      {loading && (
        <Box sx={{ display: "flex", alignItems: "center", gap: 1, p: 1.5 }}>
          <CircularProgress size={14} />
          <Typography variant="caption" color="text.secondary">
            Loading contract…
          </Typography>
        </Box>
      )}

      {error && (
        <Box sx={{ p: 1.5 }}>
          <Typography variant="caption" sx={{ color: "#f85149" }}>
            {error}
          </Typography>
        </Box>
      )}

      {!loading && !error && (
        <>
          {diagnostics.map((diagnostic, index) => (
            <Typography key={`${index}-${diagnostic}`} variant="caption" sx={{ display: "block", px: 1.5, pt: index === 0 ? 1.5 : 0, color: "error.main" }}>
              {diagnostic}
            </Typography>
          ))}
          <Box
            role="button"
            tabIndex={0}
            onClick={() => setInputsOpen(true)}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                setInputsOpen(true);
              }
            }}
            sx={{
              position: "relative",
              display: "flex",
              alignItems: "center",
              gap: 0.75,
              px: 1,
              py: 0.75,
              cursor: variables.length > 0 ? "pointer" : "default",
              "&:hover": variables.length > 0 ? { bgcolor: "action.hover" } : undefined,
            }}
          >
            <Handle
              type="target"
              position={Position.Left}
              id="in"
              isConnectable={false}
              style={{
                position: "absolute",
                left: -4,
                top: "50%",
                transform: "translateY(-50%)",
                width: 8,
                height: 8,
                opacity: 0,
              }}
            />
            <Typography variant="caption" fontWeight={600}>
              Inputs ({variables.length})
            </Typography>
            {errorCount > 0 ? (
              <Chip label={`${errorCount} to fix`} size="small" color="error" sx={{ height: 18, fontSize: "0.65rem" }} />
            ) : (
              variables.length > 0 && (
                <Chip label="configured" size="small" color="success" variant="outlined" sx={{ height: 18, fontSize: "0.65rem" }} />
              )
            )}
            {variables.length > 0 && <ChevronRightIcon sx={{ fontSize: 16, opacity: 0.6, ml: "auto" }} />}
          </Box>

          {requiredProviders.length > 0 && (
            <Box
              role="button"
              tabIndex={0}
              onClick={() => setProvidersOpen(true)}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") {
                  event.preventDefault();
                  setProvidersOpen(true);
                }
              }}
              sx={{
                borderTop: "1px solid",
                borderColor: "divider",
                display: "flex",
                alignItems: "center",
                gap: 0.75,
                px: 1,
                py: 0.75,
                cursor: "pointer",
                "&:hover": { bgcolor: "action.hover" },
              }}
            >
              <Typography variant="caption" fontWeight={600}>
                Providers ({requiredProviders.length})
              </Typography>
              <Chip
                label={
                  configuredProviderCount === requiredProviders.length
                    ? "configured"
                    : `${configuredProviderCount} configured`
                }
                size="small"
                color={configuredProviderCount === requiredProviders.length ? "success" : "default"}
                variant="outlined"
                sx={{ height: 18, fontSize: "0.65rem" }}
              />
              <ChevronRightIcon sx={{ fontSize: 16, opacity: 0.6, ml: "auto" }} />
            </Box>
          )}

          <Box sx={{ borderTop: "1px solid", borderColor: "divider", py: 0.5 }}>
            <Typography
              variant="caption"
              sx={{
                display: "block",
                px: 1,
                color: "text.secondary",
                textTransform: "uppercase",
                fontSize: "0.6rem",
                letterSpacing: "0.05em",
              }}
            >
              Outputs
            </Typography>
            {outputs.length === 0 && (
              <Typography variant="caption" sx={{ px: 1, color: "text.disabled" }}>
                none
              </Typography>
            )}
            {visibleOutputs.map((o) => (
              <Box
                key={o.name}
                sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 0.5, px: 1, py: 0.375 }}
              >
                <Tooltip title={renderTypeCompact(o.type)} placement="top">
                  <Typography variant="caption" noWrap>
                    {o.name}
                  </Typography>
                </Tooltip>
                <Handle
                  type="source"
                  position={Position.Right}
                  id={`out:${o.name}`}
                  isConnectable={false}
                  style={{
                    position: "relative",
                    transform: "none",
                    top: "auto",
                    right: "auto",
                    width: 8,
                    height: 8,
                    background: "#03deb8",
                  }}
                />
              </Box>
            ))}
            {outputs.length > OUTPUTS_COLLAPSED_COUNT && (
              <Box
                role="button"
                tabIndex={0}
                onClick={() => setOutputsExpanded((v) => !v)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    setOutputsExpanded((v) => !v);
                  }
                }}
                sx={{
                  position: "relative",
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "flex-end",
                  gap: 0.5,
                  px: 1,
                  py: 0.375,
                  cursor: "pointer",
                  "&:hover": { bgcolor: "action.hover" },
                }}
              >
                {/* Handles for the currently-collapsed outputs are kept in the DOM (stacked
                    invisibly at this row) so an edge referencing one never breaks — expanding
                    moves it to its real row and the edge redraws there. */}
                {hiddenOutputs.map((o) => (
                  <Handle
                    key={o.name}
                    type="source"
                    position={Position.Right}
                    id={`out:${o.name}`}
                    isConnectable={false}
                    style={{
                      position: "absolute",
                      right: -4,
                      top: "50%",
                      transform: "translateY(-50%)",
                      width: 8,
                      height: 8,
                      opacity: 0,
                    }}
                  />
                ))}
                <Typography variant="caption" color="primary.main">
                  {outputsExpanded ? "Show less" : `Show ${outputs.length - OUTPUTS_COLLAPSED_COUNT} more`}
                </Typography>
                {outputsExpanded ? (
                  <ExpandLessIcon sx={{ fontSize: 16, opacity: 0.8 }} />
                ) : (
                  <ExpandMoreIcon sx={{ fontSize: 16, opacity: 0.8 }} />
                )}
              </Box>
            )}
          </Box>

          {warnings && warnings.length > 0 && (
            <Tooltip title={warnings.join(" ")} placement="bottom">
              <Typography
                variant="caption"
                sx={{
                  display: "block",
                  px: 1,
                  py: 0.5,
                  color: "warning.main",
                  borderTop: "1px solid",
                  borderColor: "divider",
                  cursor: "help",
                }}
                noWrap
              >
                {warnings.length} warning{warnings.length === 1 ? "" : "s"} degrading this contract
              </Typography>
            </Tooltip>
          )}
        </>
      )}

      <InputsModal
        open={inputsOpen}
        onClose={() => setInputsOpen(false)}
        instanceName={instanceName}
        namespace={namespace}
        moduleName={name}
        system={data.system}
        version={version}
        multiplicity={multiplicity}
        variableOptions={variableOptions}
        providerBindings={providerBindings}
        providerOptions={providerOptions}
        variables={variables}
        values={values}
        optionalFieldVisibility={optionalFieldVisibility}
        fieldErrors={fieldErrors}
        referenceOptions={referenceOptions}
        metaOptions={metaOptionsFor(multiplicity, variableOptions)}
        onFieldChange={onFieldChange}
        onOptionalFieldVisibilityChange={onOptionalFieldVisibilityChange}
      />
      <ProvidersModal
        open={providersOpen}
        onClose={() => setProvidersOpen(false)}
        instanceName={instanceName}
        requiredProviders={requiredProviders}
        providerOptions={providerOptions}
        providerBindings={providerBindings}
        onProviderBindingChange={onProviderBindingChange}
      />
    </Box>
  );
}

export default React.memo(ModuleNode, areAssemblyNodePropsEqual);
