"use client";

import * as React from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import ReactFlow, {
  ReactFlowProvider,
  Background,
  Controls,
  MiniMap,
  useNodesState,
  useReactFlow,
  type Edge,
  type Node,
  type NodeTypes,
} from "reactflow";
import "reactflow/dist/style.css";
import Box from "@mui/material/Box";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import Divider from "@mui/material/Divider";
import Dialog from "@mui/material/Dialog";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import LinearProgress from "@mui/material/LinearProgress";
import IconButton from "@mui/material/IconButton";
import DeleteSweepIcon from "@mui/icons-material/DeleteSweep";
import AddIcon from "@mui/icons-material/Add";
import DownloadIcon from "@mui/icons-material/Download";
import ChevronLeftIcon from "@mui/icons-material/ChevronLeft";
import ChevronRightIcon from "@mui/icons-material/ChevronRight";
import TerminalIcon from "@mui/icons-material/Terminal";
import type { AssemblyDiagnostic, BrowseContract, BrowseProviderSchema, BrowseResource, CtyType } from "@/lib/api";
import { exportAssembly, getProviderSchema } from "@/lib/api";
import { TOGGLE_NODE_MAP_EVENT } from "@/components/MobileMapButton";
import { isValidInstanceName, literalValidationError, renderTypeCompact, typesRoughlyCompatible } from "@/lib/ctyType";
import ModuleNode, { type ModuleNodeData } from "./ModuleNode";
import VariableNode, { type VariableNodeData } from "./VariableNode";
import ProviderNode, { type ProviderNodeData } from "./ProviderNode";
import { emptyProviderConfiguration, providerConfigurationForSchema } from "./ProviderConfigurationEditor";
import { emptyValueSpec, enumerateForEachKeys, metaExprCtyType, objectAttributePaths, typeSpecToCtyType } from "./typeSpec";
import {
  EMPTY_TYPE_SPEC,
  EMPTY_VALUE_SPEC,
  type FieldValue,
  type ModuleInputValue,
  type Multiplicity,
  type ProviderBinding,
  type ProviderConfiguration,
  type ProviderOption,
  type ReferenceOption,
  type TypeSpec,
  type ValueSpec,
  type VariableValidation,
  type VariableOption,
} from "./types";

/** Normalizes a version string to have exactly one leading "v" for display. */
function displayVersion(v: string): string {
  if (!v) return "";
  return v.startsWith("v") ? v : `v${v}`;
}

const nodeTypes: NodeTypes = { module: ModuleNode, variable: VariableNode, provider: ProviderNode };

interface Props {
  modules: BrowseResource[];
  providers: BrowseResource[];
}

export function assemblyResourceMatchesQuery(
  resource: Partial<Record<"name" | "namespace" | "provider" | "providerNamespace", string | null | undefined>>,
  query: string,
): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;

  return [resource.name, resource.namespace, resource.provider, resource.providerNamespace].some(
    (value) => typeof value === "string" && value.toLowerCase().includes(q),
  );
}

/**
 * Node state actually held in React: everything except the callbacks and the
 * cross-node derived data (reference options, validation errors), which are
 * recomputed fresh on every render in `renderNodes` below. Keeping those out
 * of state means a rename or a newly added module is reflected everywhere
 * instantly with nothing to manually keep in sync. `kind` is a plain
 * discriminant on data (rather than relying on ReactFlow's own untyped
 * `node.type` string) so narrowing `nds.map((n) => n.data.kind === ...)`
 * works with real TypeScript type inference.
 */
type RawModuleData = Omit<
  ModuleNodeData,
  | "onRemove"
  | "onRenameInstance"
  | "onFieldChange"
  | "onMultiplicityChange"
  | "referenceOptions"
  | "fieldErrors"
  | "instanceNameError"
  | "variableOptions"
  | "providerOptions"
  | "onProviderBindingChange"
> & { kind: "module" };

type RawVariableData = Omit<
  VariableNodeData,
  | "onRename"
  | "onTypeChange"
  | "onDescriptionChange"
  | "onValidationsChange"
  | "onHasDefaultChange"
  | "onDefaultChange"
  | "onRemove"
  | "nameError"
  | "validationError"
> & { kind: "variable" };

type RawProviderData = Omit<
  ProviderNodeData,
  | "onRemove"
  | "onAliasChange"
  | "onConfigurationChange"
  | "referenceOptions"
  | "localNameError"
  | "aliasError"
> & { kind: "provider" };

type RawNodeData = RawModuleData | RawVariableData | RawProviderData;

let nodeSeq = 0;

function scalarInputValue(value: ModuleInputValue | undefined): FieldValue | undefined {
  if (!value) return undefined;
  if (value.kind === "scalar") return value.value;
  if ("mode" in value && (value.mode === "literal" || value.mode === "reference")) return value as unknown as FieldValue;
  return undefined;
}

function moduleInputValueProvided(value: ModuleInputValue | undefined): boolean {
  const scalar = scalarInputValue(value);
  if (scalar) return scalar.mode !== "literal" || scalar.literal.trim() !== "";
  if (!value || typeof value !== "object") return false;
  if (value.kind === "list") return Array.isArray(value.items) && value.items.length > 0;
  if (value.kind === "map" || value.kind === "object") return Array.isArray(value.entries) && value.entries.length > 0;
  return false;
}

function referencedValueType(type: CtyType, field: FieldValue, sourceMultiplicity: string): CtyType {
  let selectedType = type;

  if (sourceMultiplicity !== "none" && field.refSelector?.kind === "all") {
    selectedType = ["list", selectedType];
  }

  if (field.refOutputSelector?.kind !== "all" && Array.isArray(selectedType) && selectedType.length > 1) {
    const kind = selectedType[0];
    if (kind === "list" || kind === "set" || kind === "map") {
      selectedType = selectedType[1] as CtyType;
    }
  }

  return selectedType;
}

/** Assembly canvas state is persisted here so a design survives a reload —
 * there's no backend save yet, just a single local snapshot per browser. */
const STORAGE_KEY = "opendepot:assembly:v3";
const PREVIOUS_STORAGE_KEY = "opendepot:assembly:v2";
const LEGACY_STORAGE_KEY = "opendepot:assembly:v1";

export function migrateStoredAssemblyNodes(saved: Node<RawNodeData>[]): Node<RawNodeData>[] {
  return saved.map((node) => {
    if (node.data.kind === "module") {
      return {
        ...node,
        data: {
          ...node.data,
          values: Object.fromEntries(
            Object.entries(node.data.values ?? {}).map(([name, value]) => [name, normalizeModuleInputValue(value)]),
          ),
          requiredProviders: node.data.requiredProviders ?? [],
          providerBindings: node.data.providerBindings ?? {},
        },
      };
    }

    if (node.data.kind === "variable") {
      return {
        ...node,
        data: {
          ...node.data,
          description: node.data.description ?? "",
          validations: node.data.validations ?? [],
        },
      };
    }

    return node;
  });
}

function normalizeModuleInputValue(value: unknown): ModuleInputValue {
  if (!value || typeof value !== "object") return { kind: "scalar", value: { mode: "literal", literal: "" } };

  const candidate = value as Record<string, unknown>;
  if (candidate.kind === "scalar" && candidate.value && typeof candidate.value === "object") {
    return { kind: "scalar", value: candidate.value as FieldValue };
  }
  if (candidate.kind === "list" && Array.isArray(candidate.items)) {
    return { kind: "list", items: candidate.items.map(normalizeModuleInputValue) };
  }
  if (candidate.kind === "map" && Array.isArray(candidate.entries)) {
    return {
      kind: "map",
      entries: candidate.entries.filter(isRecord).map((entry) => ({
        key: typeof entry.key === "string" ? entry.key : "",
        value: normalizeModuleInputValue(entry.value),
      })),
    };
  }
  if (candidate.kind === "object" && Array.isArray(candidate.entries)) {
    return {
      kind: "object",
      entries: candidate.entries.filter(isRecord).map((entry) => ({
        name: typeof entry.name === "string" ? entry.name : "",
        value: normalizeModuleInputValue(entry.value),
      })),
    };
  }
  if (candidate.mode === "literal" || candidate.mode === "reference") {
    return { kind: "scalar", value: candidate as unknown as FieldValue };
  }

  return { kind: "scalar", value: { mode: "literal", literal: "" } };
}

export function duplicateMapKeys(value: ModuleInputValue | undefined): string[] {
  if (!value) return [];

  const duplicates = new Set<string>();

  const visit = (current: ModuleInputValue) => {
    if (current.kind === "map") {
      const counts = new Map<string, number>();
      for (const entry of current.entries) {
        const key = entry.key.trim();
        counts.set(key, (counts.get(key) ?? 0) + 1);
        visit(entry.value);
      }
      for (const [key, count] of counts) {
        if (key && count > 1) duplicates.add(key);
      }
      return;
    }

    if (current.kind === "object") {
      current.entries.forEach((entry) => visit(entry.value));
      return;
    }

    if (current.kind === "list") {
      current.items.forEach(visit);
    }
  };

  visit(value);
  return Array.from(duplicates).sort();
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

export function serializeAssemblyDocument(nodes: Node<RawNodeData>[]) {
  return {
    schemaVersion: "assembly.export.v1",
    variables: nodes.filter((node): node is Node<RawVariableData> => node.data.kind === "variable").map((node) => ({
      nodeId: node.id,
      name: node.data.name,
      type: node.data.type,
      description: node.data.description,
      validations: node.data.validations,
      hasDefault: node.data.hasDefault,
      default: node.data.default,
    })),
    modules: nodes.filter((node): node is Node<RawModuleData> => node.data.kind === "module").map((node) => ({
      nodeId: node.id,
      namespace: node.data.namespace,
      name: node.data.name,
      version: node.data.version,
      localName: node.data.instanceName,
      values: Object.fromEntries(
        Object.entries(node.data.values).filter(([, value]) => {
          const field = scalarInputValue(value);
          return moduleInputValueProvided(value);
        }),
      ),
      multiplicity: node.data.multiplicity,
      providerBindings: node.data.providerBindings,
    })),
    providers: nodes.filter((node): node is Node<RawProviderData> => node.data.kind === "provider").map((node) => ({
      nodeId: node.id,
      namespace: node.data.namespace,
      name: node.data.name,
      providerNamespace: node.data.providerNamespace,
      providerName: node.data.providerName,
      version: node.data.version,
      localName: node.data.localName,
      alias: node.data.alias,
      configuration: node.data.configuration,
    })),
  };
}

async function fetchContract(namespace: string, name: string, version: string): Promise<BrowseContract | null> {
  const res = await fetch(
    `/api/contract/${encodeURIComponent(namespace)}/module/${encodeURIComponent(name)}?version=${encodeURIComponent(version)}`,
  );

  if (res.status === 404) {
    return null;
  }

  if (!res.ok) {
    throw new Error(`Failed to load contract: ${res.status}`);
  }

  return (await res.json()) as BrowseContract;
}

function slugifyInstanceName(raw: string): string {
  let base = raw.replace(/^terraform-[a-z0-9]+-/i, "");
  base = base.toLowerCase().replace(/[^a-z0-9_]+/g, "_").replace(/^_+|_+$/g, "");
  if (!base) base = "module";
  if (/^[0-9]/.test(base)) base = `m_${base}`;
  return base;
}

function uniqueInstanceName(base: string, existing: Set<string>): string {
  if (!existing.has(base)) return base;

  let i = 2;
  while (existing.has(`${base}_${i}`)) i += 1;

  return `${base}_${i}`;
}

function providerConfigurationInvalid(schema: RawProviderData["schema"], configuration: ProviderConfiguration): boolean {
  for (const [name, attribute] of Object.entries(schema.attributes ?? {})) {
    if (attribute.required && !configuration.arguments[name]) return true;
  }

  for (const [name, nested] of Object.entries(schema.blocks ?? {})) {
    const instances = configuration.blocks[name] ?? [];
    if (instances.length < (nested.minItems ?? 0)) return true;
    if (nested.maxItems && instances.length > nested.maxItems) return true;
    if (instances.some((instance) => providerConfigurationInvalid(nested.block, instance))) return true;
  }

  return false;
}

function AssemblyCanvasInner({ modules, providers }: Props) {
  const [resourceMenuOpen, setResourceMenuOpen] = useState(false);
  const [mobileMapOpen, setMobileMapOpen] = useState(false);
  const [nodes, setNodes, onNodesChange] = useNodesState<RawNodeData>([]);
  const [query, setQuery] = useState("");
  const [hydrated, setHydrated] = useState(false);
  const [clearConfirmationOpen, setClearConfirmationOpen] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [exportComplete, setExportComplete] = useState(false);
  const [exportError, setExportError] = useState<{ message: string; output?: string } | null>(null);
  const [exportOutput, setExportOutput] = useState("");
  const [serverDiagnostics, setServerDiagnostics] = useState<AssemblyDiagnostic[]>([]);
  const wrapperRef = useRef<HTMLDivElement>(null);
  const saveTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const { screenToFlowPosition } = useReactFlow();

  useEffect(() => {
    const toggleNodeMap = () => setMobileMapOpen((open) => !open);
    window.addEventListener(TOGGLE_NODE_MAP_EVENT, toggleNodeMap);
    return () => window.removeEventListener(TOGGLE_NODE_MAP_EVENT, toggleNodeMap);
  }, []);

  // Load any previously saved assembly once, on mount. Deferred to an effect
  // (rather than the useNodesState initializer) since localStorage isn't
  // available during server rendering.
  useEffect(() => {
    try {
      const raw =
        localStorage.getItem(STORAGE_KEY) ??
        localStorage.getItem(PREVIOUS_STORAGE_KEY) ??
        localStorage.getItem(LEGACY_STORAGE_KEY);
      if (raw) {
        const saved = migrateStoredAssemblyNodes(JSON.parse(raw) as Node<RawNodeData>[]);
        setNodes(saved);

        const maxSeq = saved.reduce((max, n) => {
          const match = /-(\d+)$/.exec(n.id);
          return match ? Math.max(max, Number(match[1])) : max;
        }, 0);
        nodeSeq = Math.max(nodeSeq, maxSeq);
      }
    } catch {
      // Corrupt or unavailable storage — start from an empty canvas.
    }

    setHydrated(true);
  }, [setNodes]);

  // Save on every node change once hydrated, debounced so continuous drags
  // don't serialize the whole canvas on every mouse-move frame.
  useEffect(() => {
    if (!hydrated) return;

    if (saveTimeoutRef.current) {
      clearTimeout(saveTimeoutRef.current);
    }

    saveTimeoutRef.current = setTimeout(() => {
      try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(nodes));
      } catch {
        // Storage unavailable or full — saving is best-effort only.
      }
    }, 400);

    return () => {
      if (saveTimeoutRef.current) {
        clearTimeout(saveTimeoutRef.current);
      }
    };
  }, [nodes, hydrated]);

  const filteredModules = useMemo(() => {
    return modules.filter((module) => assemblyResourceMatchesQuery(module, query));
  }, [modules, query]);

  const filteredProviders = useMemo(() => {
    return providers.filter((provider) => assemblyResourceMatchesQuery(provider, query));
  }, [providers, query]);

  const removeNode = useCallback(
    (id: string) => {
      setNodes((current) =>
        current
          .filter((node) => node.id !== id)
          .map((node) => {
            if (node.data.kind !== "module") return node;

            const providerBindings = Object.fromEntries(
              Object.entries(node.data.providerBindings).filter(([, binding]) => binding.providerNodeId !== id),
            );
            return { ...node, data: { ...node.data, providerBindings } };
          }),
      );
    },
    [setNodes],
  );

  const renameInstance = useCallback(
    (id: string, newName: string) => {
      setNodes((nds) =>
        nds.map((n) => (n.id === id && n.data.kind === "module" ? { ...n, data: { ...n.data, instanceName: newName } } : n)),
      );
    },
    [setNodes],
  );

  const changeField = useCallback(
    (id: string, variableName: string, value: ModuleInputValue) => {
      setNodes((nds) =>
        nds.map((n) =>
          n.id === id && n.data.kind === "module"
            ? { ...n, data: { ...n.data, values: { ...n.data.values, [variableName]: value } } }
            : n,
        ),
      );
    },
    [setNodes],
  );

  const changeMultiplicity = useCallback(
    (id: string, multiplicity: Multiplicity) => {
      setNodes((nds) => nds.map((n) => (n.id === id && n.data.kind === "module" ? { ...n, data: { ...n.data, multiplicity } } : n)));
    },
    [setNodes],
  );

  const changeProviderBinding = useCallback(
    (id: string, localName: string, providerNodeId: string) => {
      setNodes((current) =>
        current.map((node) => {
          if (node.id !== id || node.data.kind !== "module") return node;
          const bindings = { ...node.data.providerBindings };
          if (providerNodeId) bindings[localName] = { providerNodeId };
          else delete bindings[localName];
          return { ...node, data: { ...node.data, providerBindings: bindings } };
        }),
      );
    },
    [setNodes],
  );

  const changeProvider = useCallback(
    (id: string, patch: Partial<Pick<RawProviderData, "alias" | "configuration">>) => {
      setNodes((current) =>
        current.map((node) =>
          node.id === id && node.data.kind === "provider" ? { ...node, data: { ...node.data, ...patch } } : node,
        ),
      );
    },
    [setNodes],
  );

  const renameVariable = useCallback(
    (id: string, name: string) => {
      setNodes((nds) => nds.map((n) => (n.id === id && n.data.kind === "variable" ? { ...n, data: { ...n.data, name } } : n)));
    },
    [setNodes],
  );

  const changeVariableType = useCallback(
    (id: string, type: TypeSpec) => {
      setNodes((nds) =>
        nds.map((n) =>
          n.id === id && n.data.kind === "variable"
            ? { ...n, data: { ...n.data, type, default: emptyValueSpec(type) } }
            : n,
        ),
      );
    },
    [setNodes],
  );

  const changeVariableHasDefault = useCallback(
    (id: string, hasDefault: boolean) => {
      setNodes((nds) =>
        nds.map((n) => (n.id === id && n.data.kind === "variable" ? { ...n, data: { ...n.data, hasDefault } } : n)),
      );
    },
    [setNodes],
  );

  const changeVariableDefault = useCallback(
    (id: string, value: ValueSpec) => {
      setNodes((nds) => nds.map((n) => (n.id === id && n.data.kind === "variable" ? { ...n, data: { ...n.data, default: value } } : n)));
    },
    [setNodes],
  );

  const changeVariableDescription = useCallback(
    (id: string, description: string) => {
      setNodes((nds) =>
        nds.map((n) => (n.id === id && n.data.kind === "variable" ? { ...n, data: { ...n.data, description } } : n)),
      );
    },
    [setNodes],
  );

  const changeVariableValidations = useCallback(
    (id: string, validations: VariableValidation[]) => {
      setNodes((nds) =>
        nds.map((n) => (n.id === id && n.data.kind === "variable" ? { ...n, data: { ...n.data, validations } } : n)),
      );
    },
    [setNodes],
  );

  const addModuleNode = useCallback(
    async (resource: BrowseResource, position: { x: number; y: number }) => {
      nodeSeq += 1;
      const id = `module-${nodeSeq}`;
      const version = resource.latestVersion;

      setNodes((nds) => {
        const existing = new Set(
          nds.filter((n): n is Node<RawModuleData> => n.data.kind === "module").map((n) => n.data.instanceName),
        );
        const instanceName = uniqueInstanceName(slugifyInstanceName(resource.name), existing);

        const placeholder: Node<RawNodeData> = {
          id,
          type: "module",
          position,
          data: {
            kind: "module",
            namespace: resource.namespace,
            name: resource.name,
            version,
            instanceName,
            grade: null,
            variables: [],
            outputs: [],
            requiredProviders: [],
            providerBindings: {},
            values: {},
            multiplicity: { kind: "none" },
            loading: true,
            error: null,
          },
        };

        return nds.concat(placeholder);
      });

      try {
        const contract = await fetchContract(resource.namespace, resource.name, version);
        setNodes((nds) =>
          nds.map((n) =>
            n.id === id && n.data.kind === "module"
              ? {
                  ...n,
                  data: {
                    ...n.data,
                    loading: false,
                    grade: contract?.compatibility.grade ?? "unsupported",
                    variables: contract?.variables ?? [],
                    outputs: contract?.outputs ?? [],
                    requiredProviders: contract?.requiredProviders ?? [],
                    warnings: contract?.compatibility.warnings,
                  },
                }
              : n,
          ),
        );
      } catch (err) {
        const message = err instanceof Error ? err.message : "Failed to load contract.";
        setNodes((nds) =>
          nds.map((n) => (n.id === id && n.data.kind === "module" ? { ...n, data: { ...n.data, loading: false, error: message } } : n)),
        );
      }
    },
    [setNodes],
  );

  const addProviderNode = useCallback(
    async (resource: BrowseResource, position: { x: number; y: number }) => {
      nodeSeq += 1;
      const id = `provider-${nodeSeq}`;
      const version = resource.latestVersion;
      const providerName = resource.name;
      const providerNamespace = resource.providerNamespace || "hashicorp";

      setNodes((current) => {
        const localName = slugifyInstanceName(providerName);
        return current.concat({
          id,
          type: "provider",
          position,
          data: {
            kind: "provider",
            namespace: resource.namespace,
            name: resource.name,
            version,
            providerNamespace,
            providerName,
            localName,
            alias: "",
            schema: {},
            configuration: emptyProviderConfiguration(),
            loading: true,
            error: null,
          },
        });
      });

      try {
        const schema: BrowseProviderSchema | null = await getProviderSchema(resource.namespace, resource.name, version);
        setNodes((current) =>
          current.map((node) =>
            node.id === id && node.data.kind === "provider"
              ? {
                  ...node,
                  data: {
                    ...node.data,
                    loading: false,
                    error: schema ? null : "Provider configuration schema is unavailable.",
                    schema: schema?.configuration ?? {},
                    configuration: schema ? providerConfigurationForSchema(schema.configuration) : node.data.configuration,
                    providerNamespace: schema?.providerNamespace ?? node.data.providerNamespace,
                    providerName: schema?.providerName ?? node.data.providerName,
                    version: schema?.version ?? node.data.version,
                  },
                }
              : node,
          ),
        );
      } catch (error) {
        const message = error instanceof Error ? error.message : "Failed to load provider schema.";
        setNodes((current) =>
          current.map((node) =>
            node.id === id && node.data.kind === "provider" ? { ...node, data: { ...node.data, loading: false, error: message } } : node,
          ),
        );
      }
    },
    [setNodes],
  );

  const addVariableNode = useCallback(
    (position: { x: number; y: number }) => {
      nodeSeq += 1;
      const id = `variable-${nodeSeq}`;

      setNodes((nds) => {
        const existing = new Set(
          nds.filter((n): n is Node<RawVariableData> => n.data.kind === "variable").map((n) => n.data.name),
        );
        const name = uniqueInstanceName("var", existing);

        const placeholder: Node<RawNodeData> = {
          id,
          type: "variable",
          position,
          data: {
            kind: "variable",
            name,
            type: EMPTY_TYPE_SPEC,
            description: "",
            validations: [],
            hasDefault: false,
            default: EMPTY_VALUE_SPEC,
          },
        };

        return nds.concat(placeholder);
      });
    },
    [setNodes],
  );

  // Every module's available reference targets: module.<instanceName>.<output>
  // for every other module's outputs, plus var.<name> (and var.<name>.<attr>
  // for object-typed variables) for every variable on the canvas. Recomputed
  // from scratch whenever any node's instanceName/type/outputs change, so a
  // rename never leaves a stale label behind. Variables never consume this
  // (they have no fields to wire), only modules do.
  const referenceOptionsByNode = useMemo(() => {
    const byNode = new Map<string, ReferenceOption[]>();

    for (const node of nodes) {
      if (node.data.kind !== "module" && node.data.kind !== "provider") continue;

      const options: ReferenceOption[] = [];

      for (const other of nodes) {
        if (other.id === node.id) continue;

        if (other.data.kind === "module") {
          if (other.data.loading || other.data.error) continue;

          // Work backwards from a for_each'd module to its variable's own
          // default value, if any, so "one instance" can offer a dropdown
          // of real keys instead of a blind free-text field.
          let forEachKeys: string[] | undefined;
          if (other.data.multiplicity.kind === "for_each" && other.data.multiplicity.variableNodeId) {
            const variableNodeId = other.data.multiplicity.variableNodeId;
            const varNode = nodes.find((n) => n.id === variableNodeId);
            if (varNode && varNode.data.kind === "variable") {
              forEachKeys = enumerateForEachKeys(varNode.data.type, varNode.data.hasDefault, varNode.data.default);
            }
          }

          for (const output of other.data.outputs) {
            options.push({
              nodeId: other.id,
              instanceName: other.data.instanceName,
              output: output.name,
              type: output.type,
              label: `module.${other.data.instanceName}.${output.name}`,
              sourceMultiplicity: other.data.multiplicity.kind,
              sourceKind: "module",
              forEachKeys,
            });
          }
        } else if (other.data.kind === "variable") {
          options.push({
            nodeId: other.id,
            instanceName: other.data.name,
            output: "",
            type: typeSpecToCtyType(other.data.type),
            label: `var.${other.data.name}`,
            sourceMultiplicity: "none",
            sourceKind: "variable",
          });

          for (const attribute of objectAttributePaths(other.data.type)) {
            options.push({
              nodeId: other.id,
              instanceName: other.data.name,
              output: attribute.path,
              type: typeSpecToCtyType(attribute.type),
              label: `var.${other.data.name}.${attribute.path}`,
              sourceMultiplicity: "none",
              sourceKind: "variable",
            });
          }
        }
      }

      byNode.set(node.id, options);
    }

    return byNode;
  }, [nodes]);

  // Every variable currently on the canvas, offered identically to every
  // module for the for_each picker — for_each is locked to referencing a
  // known variable, so this (not a general reference field) is the only way
  // a for_each source is chosen.
  const variableOptions = useMemo<VariableOption[]>(
    () =>
      nodes
        .filter((n): n is Node<RawVariableData> => n.data.kind === "variable")
        .map((n) => ({ nodeId: n.id, name: n.data.name, type: n.data.type })),
    [nodes],
  );

  const providerOptions = useMemo<ProviderOption[]>(
    () =>
      nodes
        .filter((node): node is Node<RawProviderData> => node.data.kind === "provider" && !node.data.loading && !node.data.error)
        .map((node) => ({
          nodeId: node.id,
          localName: node.data.localName,
          alias: node.data.alias,
          providerNamespace: node.data.providerNamespace,
          providerName: node.data.providerName,
          label: `${node.data.localName}${node.data.alias ? `.${node.data.alias}` : ""}`,
        })),
    [nodes],
  );

  const instanceNameErrorsByNode = useMemo(() => {
    const moduleCounts = new Map<string, number>();
    const variableCounts = new Map<string, number>();
    const providerAliasCounts = new Map<string, number>();

    for (const node of nodes) {
      if (node.data.kind === "module") {
        moduleCounts.set(node.data.instanceName, (moduleCounts.get(node.data.instanceName) ?? 0) + 1);
      } else if (node.data.kind === "variable") {
        variableCounts.set(node.data.name, (variableCounts.get(node.data.name) ?? 0) + 1);
      } else {
        const key = `${node.data.localName}:${node.data.alias}`;
        providerAliasCounts.set(key, (providerAliasCounts.get(key) ?? 0) + 1);
      }
    }

    const errors = new Map<string, string>();

    for (const node of nodes) {
      const name = node.data.kind === "module" ? node.data.instanceName : node.data.kind === "variable" ? node.data.name : node.data.localName;
      const counts = node.data.kind === "module" ? moduleCounts : variableCounts;
      const noun = node.data.kind;

      if (!name.trim()) {
        errors.set(node.id, "Name is required");
      } else if (!isValidInstanceName(name)) {
        errors.set(node.id, "Must start with a letter or underscore; letters, numbers, _ and - only");
      } else if (node.data.kind !== "provider" && (counts.get(name) ?? 0) > 1) {
        errors.set(node.id, `Name is already used by another ${noun} on this canvas`);
      } else if (node.data.kind === "provider" && (providerAliasCounts.get(`${node.data.localName}:${node.data.alias}`) ?? 0) > 1) {
        errors.set(node.id, "Provider local name and alias must be unique");
      }
    }

    return errors;
  }, [nodes]);

  const fieldErrorsByNode = useMemo(() => {
    const byNode = new Map<string, Record<string, string>>();

    for (const node of nodes) {
      if (node.data.kind !== "module") continue;

      const errors: Record<string, string> = {};

      for (const variable of node.data.variables) {
        const conflictingKeys = duplicateMapKeys(node.data.values[variable.name]);
        if (conflictingKeys.length > 0) {
          errors[variable.name] = `Conflicting map keys: ${conflictingKeys.map((key) => `"${key}"`).join(", ")}`;
          continue;
        }

        const field = scalarInputValue(node.data.values[variable.name]);

        if (field?.mode === "reference") {
          const stillExists = nodes.some((n) => n.id === field.refNodeId);
          if (!stillExists) {
            errors[variable.name] = "References a module that no longer exists";
          }
          continue;
        }

        const literal = field?.literal ?? "";
        if (variable.required && !moduleInputValueProvided(node.data.values[variable.name])) {
          errors[variable.name] = "Required";
          continue;
        }

        const typeError = literalValidationError(variable.type, literal);
        if (typeError) {
          errors[variable.name] = typeError;
          continue;
        }

        const trimmed = literal.trim();
        const metaType = metaExprCtyType(trimmed, node.data.multiplicity, variableOptions);
        if (metaType && !typesRoughlyCompatible(metaType, variable.type)) {
          errors[variable.name] = `${trimmed} is ${renderTypeCompact(metaType)}, not ${renderTypeCompact(variable.type)}`;
        }
      }

      byNode.set(node.id, errors);
    }

    return byNode;
  }, [nodes, variableOptions]);

  const variableValidationErrorsByNode = useMemo(() => {
    const errors = new Map<string, string>();

    for (const node of nodes) {
      if (node.data.kind !== "variable") continue;

      if (node.data.validations.some((validation) => !validation.condition.trim() || !validation.errorMessage.trim())) {
        errors.set(node.id, "Complete every variable validation");
      }
    }

    return errors;
  }, [nodes]);

  // Edges are entirely derived from field values in "reference" mode — there
  // is no manual wire-dragging in this UI. Setting a field to
  // `module.<name>.<output>` or `var.<name>` IS what draws the connection.
  const edges = useMemo<Edge[]>(() => {
    const result: Edge[] = [];

    for (const node of nodes) {
      if (node.data.kind !== "module") continue;

      const options = referenceOptionsByNode.get(node.id) ?? [];

      for (const variable of node.data.variables) {
        const field = scalarInputValue(node.data.values[variable.name]);
        if (field?.mode !== "reference" || !field.refNodeId) continue;

        const matched = options.find((o) => o.nodeId === field.refNodeId && o.output === (field.refOutput ?? ""));
        const compatible = matched
          ? typesRoughlyCompatible(referencedValueType(matched.type, field, matched.sourceMultiplicity), variable.type)
          : true;
        const sourceHandle =
          matched?.sourceKind === "variable"
            ? matched.output
              ? `attr:${matched.output.split(".")[0]}`
              : "value"
            : `out:${field.refOutput}`;

        // A reference into a repeated source needs a selector to say whether
        // it means the whole collection or one instance — reflected in the
        // edge label so the wire's intent is visible without opening a modal.
        const selectorSuffix =
          field.refSelector && field.refSelector.kind !== "all"
            ? ` [${field.refSelector.expr.literal || "?"}]`
            : field.refSelector?.kind === "all"
              ? " (all)"
              : "";
        const outputSelectorSuffix =
          field.refOutputSelector && field.refOutputSelector.kind !== "all"
            ? ` [${field.refOutputSelector.expr.literal || "?"}]`
            : field.refOutputSelector?.kind === "all"
              ? " (all output items)"
              : "";

        // Inputs are edited through a modal rather than one handle per field,
        // so every reference into this node converges on its single "in"
        // handle — the edge label is what tells the wires apart.
        result.push({
          id: `${field.refNodeId}:${field.refOutput}->${node.id}:${variable.name}`,
          source: field.refNodeId,
          sourceHandle,
          target: node.id,
          targetHandle: "in",
          animated: true,
          style: compatible ? { stroke: "#03deb8" } : { stroke: "#f85149", strokeDasharray: "4 3" },
          label: compatible
            ? `${variable.name}${selectorSuffix}${outputSelectorSuffix}`
            : `${variable.name}${selectorSuffix}${outputSelectorSuffix} (type mismatch?)`,
          labelStyle: { fill: compatible ? "#8b949e" : "#f85149", fontSize: 10 },
        });
      }

      if (node.data.multiplicity.kind === "count") {
        const expr = node.data.multiplicity.expr;

        if (expr.mode === "reference" && expr.refNodeId) {
          const refNodeId = expr.refNodeId;
          const matched = options.find((o) => o.nodeId === refNodeId && o.output === (expr.refOutput ?? ""));
          const sourceHandle =
            matched?.sourceKind === "variable"
              ? matched.output
                ? `attr:${matched.output.split(".")[0]}`
                : "value"
              : `out:${expr.refOutput}`;

          result.push({
            id: `${refNodeId}:${expr.refOutput}->${node.id}:count`,
            source: refNodeId,
            sourceHandle,
            target: node.id,
            targetHandle: "in",
            animated: true,
            style: { stroke: "#04cfd0" },
            label: "count",
            labelStyle: { fill: "#04cfd0", fontSize: 10 },
          });
        }
      }

      if (node.data.multiplicity.kind === "for_each" && node.data.multiplicity.variableNodeId) {
        const variableNodeId = node.data.multiplicity.variableNodeId;
        result.push({
          id: `${variableNodeId}:value->${node.id}:for_each`,
          source: variableNodeId,
          sourceHandle: "value",
          target: node.id,
          targetHandle: "in",
          animated: true,
          style: { stroke: "#04cfd0" },
          label: "for_each",
          labelStyle: { fill: "#04cfd0", fontSize: 10 },
        });
      }

      for (const [localName, binding] of Object.entries(node.data.providerBindings)) {
        if (!nodes.some((candidate) => candidate.id === binding.providerNodeId && candidate.data.kind === "provider")) continue;
        result.push({
          id: `${binding.providerNodeId}->${node.id}:provider:${localName}`,
          source: binding.providerNodeId,
          sourceHandle: "provider",
          target: node.id,
          targetHandle: "in",
          animated: false,
          style: { stroke: "#d29922", strokeDasharray: "4 3" },
          label: localName,
          labelStyle: { fill: "#d29922", fontSize: 10 },
        });
      }
    }

    return result;
  }, [nodes, referenceOptionsByNode]);

  // Merge the callbacks and cross-node derived data into each node's data
  // just before handing it to ReactFlow.
  const renderNodes = useMemo(
    () =>
      nodes.map((n) => {
        if (n.data.kind === "module") {
          return {
            ...n,
            data: {
              ...n.data,
              referenceOptions: referenceOptionsByNode.get(n.id) ?? [],
              fieldErrors: fieldErrorsByNode.get(n.id) ?? {},
              instanceNameError: instanceNameErrorsByNode.get(n.id) ?? null,
              variableOptions,
              providerOptions,
              error: n.data.error ?? serverDiagnostics.find((diagnostic) => diagnostic.nodeId === n.id)?.message ?? null,
              onRemove: () => removeNode(n.id),
              onRenameInstance: (name: string) => renameInstance(n.id, name),
              onFieldChange: (variableName: string, value: ModuleInputValue) => changeField(n.id, variableName, value),
              onMultiplicityChange: (multiplicity: Multiplicity) => changeMultiplicity(n.id, multiplicity),
              onProviderBindingChange: (localName: string, providerNodeId: string) => changeProviderBinding(n.id, localName, providerNodeId),
            },
          };
        }

        if (n.data.kind === "provider") {
          return {
            ...n,
            data: {
              ...n.data,
              referenceOptions: referenceOptionsByNode.get(n.id) ?? [],
              localNameError: instanceNameErrorsByNode.get(n.id) ?? null,
              aliasError: n.data.alias && !isValidInstanceName(n.data.alias) ? "Alias is invalid" : null,
              error: n.data.error ?? serverDiagnostics.find((diagnostic) => diagnostic.nodeId === n.id)?.message ?? null,
              onRemove: () => removeNode(n.id),
              onAliasChange: (alias: string) => changeProvider(n.id, { alias }),
              onConfigurationChange: (configuration: ProviderConfiguration) => changeProvider(n.id, { configuration }),
            },
          };
        }

        return {
          ...n,
          data: {
            ...n.data,
            nameError: instanceNameErrorsByNode.get(n.id) ?? null,
            validationError: variableValidationErrorsByNode.get(n.id) ?? null,
            onRename: (name: string) => renameVariable(n.id, name),
            onTypeChange: (type: TypeSpec) => changeVariableType(n.id, type),
            onDescriptionChange: (description: string) => changeVariableDescription(n.id, description),
            onValidationsChange: (validations: VariableValidation[]) => changeVariableValidations(n.id, validations),
            onHasDefaultChange: (has: boolean) => changeVariableHasDefault(n.id, has),
            onDefaultChange: (value: ValueSpec) => changeVariableDefault(n.id, value),
            onRemove: () => removeNode(n.id),
          },
        };
      }),
    [
      nodes,
      referenceOptionsByNode,
      fieldErrorsByNode,
      instanceNameErrorsByNode,
      variableValidationErrorsByNode,
      variableOptions,
      providerOptions,
      serverDiagnostics,
      removeNode,
      renameInstance,
      changeField,
      changeMultiplicity,
      changeProviderBinding,
      changeProvider,
      renameVariable,
      changeVariableType,
      changeVariableDescription,
      changeVariableValidations,
      changeVariableHasDefault,
      changeVariableDefault,
    ],
  );

  const onDragOver = useCallback((event: React.DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
  }, []);

  const onDrop = useCallback(
    (event: React.DragEvent<HTMLDivElement>) => {
      event.preventDefault();
      const raw = event.dataTransfer.getData("application/opendepot-module");
      const providerRaw = event.dataTransfer.getData("application/opendepot-provider");
      if (!raw && !providerRaw) return;

      const position = screenToFlowPosition({ x: event.clientX, y: event.clientY });
      if (providerRaw) void addProviderNode(JSON.parse(providerRaw) as BrowseResource, position);
      else void addModuleNode(JSON.parse(raw) as BrowseResource, position);
    },
    [addModuleNode, addProviderNode, screenToFlowPosition],
  );

  const clearCanvas = useCallback(() => {
    setNodes([]);
    setServerDiagnostics([]);
    setClearConfirmationOpen(false);
    try {
      localStorage.removeItem(STORAGE_KEY);
      localStorage.removeItem(PREVIOUS_STORAGE_KEY);
      localStorage.removeItem(LEGACY_STORAGE_KEY);
    } catch {
      // Storage unavailable — nothing to clean up.
    }
  }, [setNodes]);

  const hasClientErrors =
    nodes.length === 0 ||
    nodes.some((node) => (node.data.kind !== "variable" && (node.data.loading || !!node.data.error))) ||
    nodes.some((node) => node.data.kind === "provider" && (!!node.data.alias && !isValidInstanceName(node.data.alias) || providerConfigurationInvalid(node.data.schema, node.data.configuration))) ||
    instanceNameErrorsByNode.size > 0 ||
    variableValidationErrorsByNode.size > 0 ||
    Array.from(fieldErrorsByNode.values()).some((errors) => Object.keys(errors).length > 0);

  const exportCanvas = useCallback(async () => {
    setExporting(true);
    setExportComplete(false);
    setExportError(null);
    setExportOutput("");
    setServerDiagnostics([]);

    const document = serializeAssemblyDocument(nodes);

    try {
      const result = await exportAssembly(document, ({ phase, output }) => {
        const line = phase ? `[${phase}] ${output}` : output;
        setExportOutput((current) => current ? `${current}\n${line}` : line);
      });
      if (result.error) {
        setServerDiagnostics(result.error.diagnostics ?? []);
        setExportError({ message: result.error.message, output: result.error.output });
        return;
      }
      if (result.blob) {
        const url = URL.createObjectURL(result.blob);
        const anchor = window.document.createElement("a");
        anchor.href = url;
        anchor.download = "assembly-line.zip";
        anchor.style.display = "none";
        window.document.body.appendChild(anchor);
        anchor.click();
        window.setTimeout(() => {
          anchor.remove();
          URL.revokeObjectURL(url);
        }, 1000);
        setExportComplete(true);
      }
    } catch (error) {
      setExportError({ message: error instanceof Error ? error.message : "Assembly export failed." });
    } finally {
      setExporting(false);
    }
  }, [nodes]);

  const closeExportDialog = () => {
    if (exporting) return;

    setExportComplete(false);
    setExportError(null);
    setExportOutput("");
  };

  return (
    <Box sx={{ display: "flex", height: "100%", position: "relative" }}>
      <Box
        sx={{
          width: { xs: "min(280px, calc(100vw - 16px))", sm: 280 },
          flexShrink: 0,
          borderRight: "1px solid",
          borderColor: "divider",
          display: { xs: resourceMenuOpen ? "flex" : "none", sm: "flex" },
          flexDirection: "column",
          position: { xs: "absolute", sm: "static" },
          inset: { xs: "0 auto 0 0", sm: "auto" },
          zIndex: { xs: 10, sm: "auto" },
          height: { xs: "100%", sm: "auto" },
          bgcolor: "background.paper",
          boxShadow: { xs: 8, sm: "none" },
        }}
      >
        <Box sx={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column", p: 1.5, gap: 1 }}>
          <TextField
            id="assembly-resource-search"
            size="small"
            placeholder="Search resources…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <Box sx={{ overflowY: "auto", flex: 1, minHeight: 0 }}>
            <Typography variant="caption" fontWeight={600} color="text.secondary" sx={{ display: "block", mb: 0.75 }}>
              Modules
            </Typography>
            {filteredModules.map((m) => (
              <Box
                key={`${m.namespace}/${m.name}`}
                draggable
                onDragStart={(e) => {
                  e.dataTransfer.setData("application/opendepot-module", JSON.stringify(m));
                  e.dataTransfer.effectAllowed = "move";
                }}
                onClick={() => void addModuleNode(m, { x: 80 + nodes.length * 30, y: 80 + nodes.length * 24 })}
                sx={{
                  p: 1,
                  mb: 0.75,
                  borderRadius: 1,
                  border: "1px solid",
                  borderColor: "divider",
                  cursor: "grab",
                  "&:hover": { borderColor: "primary.main", bgcolor: "action.hover" },
                }}
              >
                <Typography variant="body2" fontWeight={600} noWrap>
                  {m.name}
                </Typography>
                <Typography variant="caption" color="text.secondary" noWrap sx={{ display: "block" }}>
                  {m.namespace} · {m.provider} · {displayVersion(m.latestVersion)}
                </Typography>
              </Box>
            ))}
            {filteredModules.length === 0 && (
              <Typography variant="caption" color="text.secondary" sx={{ display: "block", mb: 1 }}>
                No modules found.
              </Typography>
            )}
            <Divider sx={{ my: 1 }} />
            <Typography variant="caption" fontWeight={600} color="text.secondary" sx={{ display: "block", mb: 0.75 }}>
              Providers
            </Typography>
            {filteredProviders.map((provider) => (
              <Box
                key={`${provider.namespace}/${provider.name}`}
                draggable
                onDragStart={(event) => {
                  event.dataTransfer.setData("application/opendepot-provider", JSON.stringify(provider));
                  event.dataTransfer.effectAllowed = "move";
                }}
                onClick={() => void addProviderNode(provider, { x: 80 + nodes.length * 30, y: 80 + nodes.length * 24 })}
                sx={{ p: 1, mb: 0.75, borderRadius: 1, border: "1px solid", borderColor: "divider", cursor: "grab", "&:hover": { borderColor: "warning.main", bgcolor: "action.hover" } }}
              >
                <Typography variant="body2" fontWeight={600} noWrap>{provider.name}</Typography>
                <Typography variant="caption" color="text.secondary" noWrap sx={{ display: "block" }}>
                  {provider.namespace} · {displayVersion(provider.latestVersion)}
                </Typography>
              </Box>
            ))}
            {filteredProviders.length === 0 && (
              <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
                No providers found.
              </Typography>
            )}
          </Box>
        </Box>

        <Divider />

        <Box sx={{ display: "flex", flexDirection: "column", gap: 1, p: 1.5, maxHeight: "40%" }}>
          <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
            <Typography variant="caption" fontWeight={600} color="text.secondary" sx={{ textTransform: "uppercase", letterSpacing: "0.05em" }}>
              Variables
            </Typography>
            <Button
              size="small"
              startIcon={<AddIcon />}
              onClick={() => addVariableNode({ x: 80 + nodes.length * 30, y: 80 + nodes.length * 24 })}
            >
              Add
            </Button>
          </Box>
          <Box sx={{ overflowY: "auto" }}>
            {variableOptions.length === 0 && (
              <Typography variant="caption" color="text.secondary">
                No variables yet.
              </Typography>
            )}
            {variableOptions.map((v) => (
              <Box
                key={v.nodeId}
                sx={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "space-between",
                  gap: 0.5,
                  p: 0.75,
                  mb: 0.5,
                  borderRadius: 1,
                  border: "1px solid",
                  borderColor: "divider",
                }}
              >
                <Typography variant="body2" noWrap>
                  var.{v.name}
                </Typography>
                <Chip label={v.type.kind} size="small" variant="outlined" sx={{ height: 18, fontSize: "0.65rem" }} />
              </Box>
            ))}
          </Box>
        </Box>

        <Divider />

        <Box sx={{ display: "grid", gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr)", gap: 1, p: 1.5 }}>
          <Button
            color="error"
            size="small"
            startIcon={<DeleteSweepIcon />}
            onClick={() => setClearConfirmationOpen(true)}
            disabled={nodes.length === 0}
          >
            Clear
          </Button>
          <Button
            color="primary"
            size="small"
            startIcon={<DownloadIcon />}
            onClick={() => void exportCanvas()}
            disabled={hasClientErrors || exporting}
          >
            {exporting ? "Validating…" : "Export"}
          </Button>
        </Box>
      </Box>

      <Box ref={wrapperRef} sx={{ flex: 1, minWidth: 0, position: "relative" }} onDrop={onDrop} onDragOver={onDragOver}>
        <IconButton
          aria-label={resourceMenuOpen ? "Hide Assembly Line menu" : "Show Assembly Line menu"}
          onClick={() => setResourceMenuOpen((open) => !open)}
          sx={{
            display: { xs: "flex", sm: "none" },
            position: "absolute",
            top: "50%",
            left: resourceMenuOpen ? "calc(min(280px, calc(100vw - 16px)) - 21px)" : 0,
            transform: "translateY(-50%)",
            zIndex: 11,
            bgcolor: "transparent",
            borderRadius: "50%",
            "&:hover": { bgcolor: "action.hover" },
          }}
        >
          {resourceMenuOpen ? <ChevronLeftIcon /> : <ChevronRightIcon />}
        </IconButton>
        <ReactFlow
          nodes={renderNodes}
          edges={edges}
          nodeTypes={nodeTypes}
          onNodesChange={onNodesChange}
          onPaneClick={() => setResourceMenuOpen(false)}
          onNodeClick={() => setResourceMenuOpen(false)}
          fitView
          minZoom={0.2}
          maxZoom={2}
          proOptions={{ hideAttribution: true }}
          style={{ background: "var(--mui-palette-background-default)" }}
        >
          <Background color="var(--mui-palette-divider)" gap={20} />
          <Controls
            style={{
              background: "var(--mui-palette-background-paper)",
              border: "1px solid var(--mui-palette-divider)",
              borderRadius: 6,
            }}
          />
          <Box sx={{ display: { xs: mobileMapOpen ? "block" : "none", sm: "block" } }}>
            <MiniMap
              style={{
                background: "var(--mui-palette-background-paper)",
                border: "1px solid var(--mui-palette-divider)",
                borderRadius: 6,
              }}
              nodeColor={(n) => (n.type === "variable" ? "#04cfd0" : n.type === "provider" ? "#d29922" : "#03deb8")}
            />
          </Box>
        </ReactFlow>
        {nodes.length === 0 && (
          <Box
            sx={{
              position: "absolute",
              inset: 0,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              px: { xs: 6, sm: 3 },
              pointerEvents: "none",
            }}
          >
            <Typography color="text.secondary" sx={{ maxWidth: 480, textAlign: "center" }}>
              Drag a module from the left, or click one to add it here. Wire modules together by setting a
              field to another module&apos;s output or a variable.
            </Typography>
          </Box>
        )}
      </Box>
      <Dialog open={clearConfirmationOpen} onClose={() => setClearConfirmationOpen(false)} fullWidth maxWidth="xs">
        <DialogTitle>Clear the entire canvas?</DialogTitle>
        <DialogContent dividers>
          <Typography variant="body2">
            This permanently removes every module, provider, variable, and connection from this browser-local Assembly.
            This action cannot be undone.
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setClearConfirmationOpen(false)} autoFocus>
            Cancel
          </Button>
          <Button color="error" variant="contained" startIcon={<DeleteSweepIcon />} onClick={clearCanvas}>
            Clear canvas
          </Button>
        </DialogActions>
      </Dialog>
      <Dialog open={exporting || exportComplete || !!exportError} onClose={closeExportDialog} fullWidth maxWidth="sm">
        <DialogTitle>{exportError ? "Export failed" : exportComplete ? "Export complete" : "Validating export"}</DialogTitle>
        <DialogContent dividers>
          {exporting && <LinearProgress sx={{ mb: 2 }} />}
          {exportError && <Typography variant="body2">{exportError.message}</Typography>}
          {exportComplete && <Typography variant="body2">The validated Assembly archive has been downloaded.</Typography>}
          {(exportOutput || exportError?.output) && (
            <Box sx={{ mt: 2, overflow: "hidden", border: "1px solid #30363d", borderRadius: 1, bgcolor: "#0d1117", boxShadow: "0 8px 24px rgba(0, 0, 0, 0.18)" }}>
              <Box sx={{ height: 34, px: 1.5, display: "flex", alignItems: "center", gap: 1, bgcolor: "#161b22", borderBottom: "1px solid #30363d", color: "#8b949e" }}>
                <TerminalIcon sx={{ fontSize: 16, color: "#03deb8" }} />
                <Typography sx={{ fontFamily: "monospace", fontSize: "0.7rem", color: "#c9d1d9" }}>
                  OpenTofu
                </Typography>
                <Typography sx={{ ml: "auto", fontFamily: "monospace", fontSize: "0.65rem", color: exporting ? "#03deb8" : exportError ? "#f85149" : "#8b949e", textTransform: "uppercase" }}>
                  {exporting ? "running" : exportError ? "failed" : "complete"}
                </Typography>
              </Box>
              <Box
                component="pre"
                aria-label="OpenTofu output"
                sx={{ m: 0, p: 2, maxHeight: 360, overflow: "auto", color: "#c9d1d9", fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace', fontSize: "0.75rem", lineHeight: 1.55, whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}
              >
                {exportOutput || exportError?.output}
              </Box>
            </Box>
          )}
        </DialogContent>
        <DialogActions>{!exporting && <Button onClick={closeExportDialog}>Close</Button>}</DialogActions>
      </Dialog>
    </Box>
  );
}

export default function AssemblyCanvas(props: Props) {
  return (
    <ReactFlowProvider>
      <AssemblyCanvasInner {...props} />
    </ReactFlowProvider>
  );
}
