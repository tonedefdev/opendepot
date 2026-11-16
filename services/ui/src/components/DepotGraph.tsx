"use client";

import * as React from "react";
import { useCallback, useEffect, useMemo, useState } from "react";
import ReactFlow, {
  Background,
  Controls,
  Handle,
  MiniMap,
  getRectOfNodes,
  getTransformForBounds,
  useNodesInitialized,
  useReactFlow,
  useStore,
  type Edge,
  type Node,
  type NodeMouseHandler,
  MarkerType,
  Position,
} from "reactflow";
import "reactflow/dist/style.css";
import Box from "@mui/material/Box";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import { DepotKindIcon, type DepotKind } from "@/components/DepotKindIcon";
import { kindColor, kindTint } from "@/lib/kindColors";
import { BRAND_GRADIENT, BRAND_GRADIENT_WASH } from "@/lib/brand";
import type {
  BrowseDepotGraph,
  BrowseGraphDepot,
  BrowseGraphModule,
  BrowseGraphProvider,
  BrowseGraphAgent,
  BrowseStorageConfig,
} from "@/lib/api";
import DepotNodePanel from "./DepotNodePanel";
import { TOGGLE_NODE_MAP_EVENT } from "./MobileMapButton";

// ── Brand palette ─────────────────────────────────────────────────────────
// Every kind shares the accent. Icons and labels tell kinds apart, not color.
const DEPOT_COLOR = kindColor("depot");
const MODULE_COLOR = kindColor("module");
const PROVIDER_COLOR = kindColor("provider");
const SKILL_COLOR = kindColor("skill");
const AGENT_COLOR = kindColor("agent");
const VERSION_COLOR = kindColor("version");
const ACCENT_BG = kindTint("module");
const VERSION_BG = kindTint("version", 10);
const PAPER_LAYER = "linear-gradient(var(--mui-palette-background-paper), var(--mui-palette-background-paper)) padding-box";
// Depots are the graph roots, so they carry the brand gradient as a border instead of a flat kind color.
const DEPOT_BACKGROUND = `${BRAND_GRADIENT_WASH.replace(/\)$/, ") padding-box")}, ${PAPER_LAYER}, ${BRAND_GRADIENT.replace(/\)$/, ") border-box")}`;

// ── Layout constants ───────────────────────────────────────────────────────
const LANE_X = { depot: 40, resource: 420, version: 780 };
const DEPOT_GAP = 140;
const RESOURCE_GAP = 86;
const VERSION_GAP = 80;
const NODE_WIDTH = 200;
const NODE_HEIGHT = 64;
// Number of version nodes shown per resource before collapsing the rest.
const VERSION_PAGE_SIZE = 3;
const FIT_PADDING = 0.2;
const MIN_ZOOM = 0.2;
const MAX_ZOOM = 2;
const MIN_READABLE_ZOOM = 0.8;
const VIEWPORT_INSET = 40;

// ── Node types ─────────────────────────────────────────────────────────────
type NodeKind = "depot" | "module" | "provider" | "skill" | "agent" | "version" | "versionOverflow";

interface NodeData {
  kind: NodeKind;
  namespace: string;
  name: string;
  displayName?: string;
  label: string;
  synced?: boolean;
  provider?: string;
  providerNamespace?: string;
  upstreamRegistry?: string;
  storageBackend?: string;
  storageConfig?: BrowseStorageConfig;
  githubAuthenticated?: boolean;
  latestVersion?: string;
  fileName?: string;
  checksum?: string;
  lastScanned?: string;
  // versionOverflow nodes only:
  resourceId?: string;
  isExpanded?: boolean;
  hiddenCount?: number;
}

function isEffectivelySynced(synced?: boolean, syncStatus?: string): boolean {
  if (synced) {
    return true;
  }
  return /successfully\s+synced/i.test(syncStatus ?? "");
}

function compareVersionDesc(a: string, b: string): number {
  const normalize = (v: string) => v.replace(/^v/i, "");
  const tokenize = (v: string) =>
    normalize(v)
      .split(/[.+-]/)
      .map((part) => {
        const numeric = Number(part);
        return Number.isFinite(numeric) && part !== "" ? numeric : part.toLowerCase();
      });

  const aTokens = tokenize(a);
  const bTokens = tokenize(b);
  const length = Math.max(aTokens.length, bTokens.length);

  for (let i = 0; i < length; i += 1) {
    const left = aTokens[i];
    const right = bTokens[i];

    if (left === undefined) return 1;
    if (right === undefined) return -1;
    if (left === right) continue;

    if (typeof left === "number" && typeof right === "number") {
      return right - left;
    }

    if (typeof left === "number") return -1;
    if (typeof right === "number") return 1;

    return String(right).localeCompare(String(left));
  }

  return 0;
}

function nodeStyle(kind: NodeKind): React.CSSProperties {
  const map: Record<NodeKind, { border: string; bg: string }> = {
    depot: { border: "transparent", bg: DEPOT_BACKGROUND },
    module: { border: MODULE_COLOR, bg: ACCENT_BG },
    provider: { border: PROVIDER_COLOR, bg: ACCENT_BG },
    skill: { border: SKILL_COLOR, bg: ACCENT_BG },
    agent: { border: AGENT_COLOR, bg: ACCENT_BG },
    version: { border: VERSION_COLOR, bg: VERSION_BG },
    versionOverflow: { border: VERSION_COLOR, bg: VERSION_BG },
  };
  const { border, bg } = map[kind];
  return {
    background: bg,
    border: `1px solid ${border}`,
    borderRadius: 8,
    padding: "10px 14px",
    minWidth: NODE_WIDTH,
    maxWidth: NODE_WIDTH,
    minHeight: NODE_HEIGHT,
    color: "var(--mui-palette-text-primary)",
    fontFamily: "inherit",
    fontSize: 13,
    display: "flex",
    flexDirection: "column" as const,
    justifyContent: "center",
    gap: 4,
    boxSizing: "border-box" as const,
  };
}

// A Skill or Agent Version as drawn on the graph.
export interface AgentVersionMeta {
  version: string;
  commit?: string;
  fileName?: string;
  checksum?: string;
  lastScanned?: string;
}

type AgentNodeKind = "skill" | "agent";

type AgentVersionItem = { label: string; fileName?: string; checksum?: string; lastScanned?: string };

export function agentVersionKey(kind: AgentNodeKind, namespace: string, name: string): string {
  return `${kind}/${namespace}/${name}`;
}

// Versions published without a version string are labeled by their short commit.
// Without a version list, the Skill or Agent's latest version is the only entry.
export function agentVersionItems(
  kind: AgentNodeKind,
  item: BrowseGraphAgent,
  metaByKey: Record<string, AgentVersionMeta[]>,
): AgentVersionItem[] {
  const meta = metaByKey[agentVersionKey(kind, item.namespace, item.name)];
  if (meta === undefined) {
    return item.latestVersion ? [{ label: item.latestVersion }] : [];
  }

  const byLabel = new Map<string, AgentVersionItem>();
  meta.forEach((entry) => {
    const label = entry.version || entry.commit?.slice(0, 7) || "";
    if (label && !byLabel.has(label)) {
      byLabel.set(label, { label, fileName: entry.fileName, checksum: entry.checksum, lastScanned: entry.lastScanned });
    }
  });
  return Array.from(byLabel.values()).sort((a, b) => compareVersionDesc(a.label, b.label));
}

function NodeTitle({
  kind,
  color,
  weight = 600,
  children,
}: {
  kind: DepotKind;
  color: string;
  weight?: number;
  children: React.ReactNode;
}) {
  return (
    <span style={{ display: "flex", alignItems: "center", gap: 6, minWidth: 0 }}>
      <DepotKindIcon kind={kind} sx={{ fontSize: 16, color, flexShrink: 0 }} />
      <span style={{ fontWeight: weight, fontSize: 13, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", minWidth: 0 }}>
        {children}
      </span>
    </span>
  );
}

function VersionFlowNode({ data }: { data: NodeData }) {
  const primaryText = data.displayName ?? data.latestVersion ?? "No version";
  const secondaryText = data.displayName ? (data.latestVersion ?? data.name) : data.name;

  return (
    <div style={nodeStyle("version")}>
      <Handle type="target" position={Position.Left} style={{ opacity: 0, width: 8, height: 8 }} />
      <NodeTitle kind="version" color={VERSION_COLOR}>
        {primaryText}
      </NodeTitle>
      <span style={{ fontSize: 11, color: "var(--mui-palette-text-secondary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {secondaryText}
      </span>
    </div>
  );
}

function DepotFlowNode({ data }: { data: NodeData }) {
  return (
    <div style={nodeStyle("depot")}>
      <Handle type="target" position={Position.Left} style={{ opacity: 0, width: 8, height: 8 }} />
      <Handle type="source" position={Position.Right} style={{ opacity: 0, width: 8, height: 8 }} />
      <NodeTitle kind="depot" color={DEPOT_COLOR} weight={700}>
        {data.name}
      </NodeTitle>
      <span style={{ fontSize: 11, color: "var(--mui-palette-text-secondary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.namespace}
        {data.storageBackend ? ` · ${data.storageBackend}` : ""}
      </span>
    </div>
  );
}

function ModuleFlowNode({ data }: { data: NodeData }) {
  return (
    <div style={nodeStyle("module")}>
      <Handle type="target" position={Position.Left} style={{ opacity: 0, width: 8, height: 8 }} />
      <Handle type="source" position={Position.Right} style={{ opacity: 0, width: 8, height: 8 }} />
      <NodeTitle kind="module" color={MODULE_COLOR}>
        {data.name}
      </NodeTitle>
      <span style={{ fontSize: 11, color: "var(--mui-palette-text-secondary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.namespace}
        {data.provider ? ` · ${data.provider}` : ""}
        {!data.synced ? " · ⚠ unsynced" : ""}
      </span>
    </div>
  );
}

function ProviderFlowNode({ data }: { data: NodeData }) {
  return (
    <div style={nodeStyle("provider")}>
      <Handle type="target" position={Position.Left} style={{ opacity: 0, width: 8, height: 8 }} />
      <Handle type="source" position={Position.Right} style={{ opacity: 0, width: 8, height: 8 }} />
      <NodeTitle kind="provider" color={PROVIDER_COLOR}>
        {data.name}
      </NodeTitle>
      <span style={{ fontSize: 11, color: "var(--mui-palette-text-secondary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.namespace}
        {data.providerNamespace ? `/${data.providerNamespace}` : ""}
        {!data.synced ? " · ⚠ unsynced" : ""}
      </span>
    </div>
  );
}

function CatalogFlowNode({ data }: { data: NodeData }) {
  return (
    <div style={nodeStyle(data.kind)}>
      <Handle type="target" position={Position.Left} style={{ opacity: 0, width: 8, height: 8 }} />
      <Handle type="source" position={Position.Right} style={{ opacity: 0, width: 8, height: 8 }} />
      <NodeTitle
        kind={data.kind === "agent" ? "agent" : "skill"}
        color={data.kind === "agent" ? AGENT_COLOR : SKILL_COLOR}
      >
        {data.name}
      </NodeTitle>
      <span style={{ fontSize: 11, color: "var(--mui-palette-text-secondary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.namespace}
        {data.latestVersion ? ` · ${data.latestVersion}` : ""}
        {!data.synced ? " · ⚠ unsynced" : ""}
      </span>
    </div>
  );
}

function VersionOverflowFlowNode({ data }: { data: NodeData }) {
  const overflowStyle: React.CSSProperties = {
    ...nodeStyle("version"),
    background: "rgba(139,148,158,0.06)",
    border: "1px dashed #8b949e",
    cursor: "pointer",
    alignItems: "center",
    flexDirection: "row",
    gap: 8,
    justifyContent: "center",
  };
  return (
    <div style={overflowStyle}>
      <Handle type="target" position={Position.Left} style={{ opacity: 0, width: 8, height: 8 }} />
      {data.isExpanded ? (
        <Tooltip title="collapse" placement="top">
          <span style={{ fontSize: 18, lineHeight: 1, fontWeight: 600 }}>−</span>
        </Tooltip>
      ) : (
        <>
          <span style={{ fontSize: 18, lineHeight: 1, fontWeight: 600 }}>+</span>
          <span style={{ fontWeight: 500, fontSize: 12, color: "var(--mui-palette-text-secondary)" }}>
            {`${data.hiddenCount} more versions`}
          </span>
        </>
      )}
    </div>
  );
}

const nodeTypes = {
  depotNode: DepotFlowNode,
  moduleNode: ModuleFlowNode,
  providerNode: ProviderFlowNode,
  skillNode: CatalogFlowNode,
  agentNode: CatalogFlowNode,
  versionNode: VersionFlowNode,
  versionOverflowNode: VersionOverflowFlowNode,
};

// ── Layout builder ─────────────────────────────────────────────────────────
function buildGraph(
  graph: BrowseDepotGraph,
  moduleVersionsByKey: Record<string, string[]>,
  providerVersionsByKey: Record<string, string[]>,
  providerVersionMetaByKey: Record<string, Array<{ version: string; name?: string; fileName?: string; checksum?: string; lastScanned?: string }>>,
  moduleVersionMetaByKey: Record<string, Array<{ version: string; fileName?: string; checksum?: string; lastScanned?: string }>>,
  moduleDetailByKey: Record<string, { storageConfig?: BrowseStorageConfig; githubAuthenticated?: boolean }>,
  agentVersionMetaByKey: Record<string, AgentVersionMeta[]>,
  expandedResources: Set<string>,
): { nodes: Node<NodeData>[]; edges: Edge[] } {
  const nodes: Node<NodeData>[] = [];
  const edges: Edge[] = [];

  // Position depot nodes
  graph.depots.forEach((d: BrowseGraphDepot, i: number) => {
    nodes.push({
      id: d.id,
      type: "depotNode",
      position: { x: LANE_X.depot, y: i * DEPOT_GAP },
      data: {
        kind: "depot",
        namespace: d.namespace,
        name: d.name,
        label: d.name,
        storageBackend: d.storageBackend,
      },
    });
  });

  // Position module/provider nodes with enough vertical space for module version fanout.
  const resourceNodes: Array<{ id: string; type: string; data: NodeData; stride: number }> = [];
  graph.modules.forEach((m: BrowseGraphModule) => {
    const effectiveSynced = isEffectivelySynced(m.synced, m.syncStatus);
    const key = `${m.namespace}/${m.name}`;
    const moduleDetail = moduleDetailByKey[key];
    const moduleVersions = moduleVersionsByKey[key] ?? (m.latestVersion ? [m.latestVersion] : []);
    const uniqueVersions = Array.from(new Set(moduleVersions)).filter(Boolean);
    const isExpanded = expandedResources.has(m.id);
    const visibleCount =
      uniqueVersions.length > VERSION_PAGE_SIZE && !isExpanded
        ? VERSION_PAGE_SIZE
        : uniqueVersions.length;
    // +1 for overflow node when we need to truncate
    const slotCount = uniqueVersions.length > VERSION_PAGE_SIZE ? visibleCount + 1 : visibleCount;
    const versionStackHeight = NODE_HEIGHT + Math.max(0, slotCount - 1) * VERSION_GAP;
    const stride = Math.max(RESOURCE_GAP, versionStackHeight + 20);
    resourceNodes.push({
      id: m.id,
      type: "moduleNode",
      stride,
      data: {
        kind: "module",
        namespace: m.namespace,
        name: m.name,
        label: m.name,
        synced: effectiveSynced,
        provider: m.provider,
        storageConfig: moduleDetail?.storageConfig,
        githubAuthenticated: moduleDetail?.githubAuthenticated,
        latestVersion: m.latestVersion,
      },
    });
  });
  graph.providers.forEach((p: BrowseGraphProvider) => {
    const key = `${p.namespace}/${p.name}`;
    const providerMeta = providerVersionMetaByKey[key] ?? [];
    const totalVersionCount = providerMeta.length > 0
      ? providerMeta.length
      : Array.from(new Set(providerVersionsByKey[key] ?? [])).filter(Boolean).length;
    const isExpanded = expandedResources.has(p.id);
    const visibleCount =
      totalVersionCount > VERSION_PAGE_SIZE && !isExpanded
        ? VERSION_PAGE_SIZE
        : totalVersionCount;
    const slotCount = totalVersionCount > VERSION_PAGE_SIZE ? visibleCount + 1 : visibleCount;
    const versionStackHeight = NODE_HEIGHT + Math.max(0, slotCount - 1) * VERSION_GAP;
    const stride = Math.max(RESOURCE_GAP, versionStackHeight + 20);
    resourceNodes.push({
      id: p.id,
      type: "providerNode",
      stride,
      data: {
        kind: "provider",
        namespace: p.namespace,
        name: p.name,
        label: p.name,
        synced: p.synced,
        providerNamespace: p.providerNamespace,
        upstreamRegistry: p.upstreamRegistry,
      },
    });
  });

  const agentItems = [
    ...graph.skills.map((s: BrowseGraphAgent) => ({ item: s, type: "skillNode", kind: "skill" as const })),
    ...graph.agents.map((a: BrowseGraphAgent) => ({ item: a, type: "agentNode", kind: "agent" as const })),
  ];
  agentItems.forEach(({ item, type, kind }) => {
    const versionCount = agentVersionItems(kind, item, agentVersionMetaByKey).length;
    const visibleCount =
      versionCount > VERSION_PAGE_SIZE && !expandedResources.has(item.id)
        ? VERSION_PAGE_SIZE
        : versionCount;
    const slotCount = versionCount > VERSION_PAGE_SIZE ? visibleCount + 1 : visibleCount;
    const versionStackHeight = NODE_HEIGHT + Math.max(0, slotCount - 1) * VERSION_GAP;
    resourceNodes.push({
      id: item.id,
      type,
      stride: Math.max(RESOURCE_GAP, versionStackHeight + 20),
      data: {
        kind,
        namespace: item.namespace,
        name: item.name,
        label: item.name,
        synced: isEffectivelySynced(item.synced, item.syncStatus),
        latestVersion: item.latestVersion,
      },
    });
  });

  let currentResourceY = 0;
  const resourceYById = new Map<string, number>();
  resourceNodes.forEach((n) => {
    resourceYById.set(n.id, currentResourceY);
    nodes.push({
      id: n.id,
      type: n.type,
      position: { x: LANE_X.resource, y: currentResourceY },
      data: n.data,
    });
    currentResourceY += n.stride;
  });

  // Add one version node per module version.
  graph.modules.forEach((m) => {
    const key = `${m.namespace}/${m.name}`;
    const versionMeta = moduleVersionMetaByKey[key] ?? [];
    const moduleVersions =
      versionMeta.length > 0
        ? versionMeta.map((entry) => entry.version)
        : moduleVersionsByKey[key] ?? (m.latestVersion ? [m.latestVersion] : []);
    const versions = Array.from(new Set(moduleVersions)).filter(Boolean).sort(compareVersionDesc);
    if (versions.length === 0) {
      return;
    }
    const baseY = resourceYById.get(m.id);
    if (baseY === undefined) {
      return;
    }

    const isExpanded = expandedResources.has(m.id);
    const visibleVersions =
      versions.length > VERSION_PAGE_SIZE && !isExpanded
        ? versions.slice(0, VERSION_PAGE_SIZE)
        : versions;

    visibleVersions.forEach((version, index) => {
      const versionNodeId = `version/${m.namespace}/${m.name}/${version}`;
      const meta = versionMeta.find((entry) => entry.version === version);
      nodes.push({
        id: versionNodeId,
        type: "versionNode",
        position: { x: LANE_X.version, y: baseY + index * VERSION_GAP },
        data: {
          kind: "version",
          namespace: m.namespace,
          name: m.name,
          label: version,
          latestVersion: version,
          fileName: meta?.fileName,
          checksum: meta?.checksum,
          lastScanned: meta?.lastScanned,
        },
      });

      edges.push({
        id: `edge-${m.id}-${versionNodeId}`,
        source: m.id,
        target: versionNodeId,
        markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 14, height: 14 },
        style: { stroke: "#8b949e", strokeWidth: 1.6, strokeDasharray: "4 3" },
        type: "smoothstep",
      });
    });

    if (versions.length > VERSION_PAGE_SIZE) {
      const overflowIndex = visibleVersions.length;
      const overflowNodeId = `versionOverflow/${m.id}`;
      nodes.push({
        id: overflowNodeId,
        type: "versionOverflowNode",
        position: { x: LANE_X.version, y: baseY + overflowIndex * VERSION_GAP },
        data: {
          kind: "versionOverflow",
          namespace: m.namespace,
          name: m.name,
          label: isExpanded ? "collapse" : `+${versions.length - VERSION_PAGE_SIZE} more versions`,
          resourceId: m.id,
          isExpanded,
          hiddenCount: versions.length - VERSION_PAGE_SIZE,
        },
      });
      edges.push({
        id: `edge-${m.id}-${overflowNodeId}`,
        source: m.id,
        target: overflowNodeId,
        style: { stroke: "#8b949e", strokeWidth: 1, strokeDasharray: "3 4", opacity: 0.5 },
        type: "smoothstep",
      });
    }
  });

  // Add one version node per provider version.
  graph.providers.forEach((p) => {
    const key = `${p.namespace}/${p.name}`;
    const providerMeta = providerVersionMetaByKey[key] ?? [];

    const items: Array<{ version: string; name?: string; fileName?: string; checksum?: string; lastScanned?: string }> =
      providerMeta.length > 0
        ? [...providerMeta]
        : Array.from(new Set(providerVersionsByKey[key] ?? []))
            .filter(Boolean)
            .map((version) => ({ version }));

    items.sort((a, b) => {
      const versionCompare = compareVersionDesc(a.version, b.version);
      if (versionCompare !== 0) {
        return versionCompare;
      }
      return (a.name ?? "").localeCompare(b.name ?? "");
    });

    if (items.length === 0) {
      return;
    }

    const baseY = resourceYById.get(p.id);
    if (baseY === undefined) {
      return;
    }

    const isExpanded = expandedResources.has(p.id);
    const visibleItems =
      items.length > VERSION_PAGE_SIZE && !isExpanded
        ? items.slice(0, VERSION_PAGE_SIZE)
        : items;

    visibleItems.forEach((item, index) => {
      const version = item.version;
      const versionName = item.name?.trim() ? item.name : p.name;
      const rawIdSuffix = item.name?.trim()
        ? item.name
        : `${version}-${index}`;
      const idSuffix = encodeURIComponent(rawIdSuffix);
      const versionNodeId = `version/${p.namespace}/${p.name}/${idSuffix}`;
      nodes.push({
        id: versionNodeId,
        type: "versionNode",
        position: { x: LANE_X.version, y: baseY + index * VERSION_GAP },
        data: {
          kind: "version",
          namespace: p.namespace,
          name: versionName,
          displayName: item.name,
          label: version,
          latestVersion: version,
          fileName: item.fileName,
          checksum: item.checksum,
          lastScanned: item.lastScanned,
        },
      });

      edges.push({
        id: `edge-${p.id}-${versionNodeId}`,
        source: p.id,
        target: versionNodeId,
        markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 14, height: 14 },
        style: { stroke: "#8b949e", strokeWidth: 1.6, strokeDasharray: "4 3" },
        type: "smoothstep",
      });
    });

    if (items.length > VERSION_PAGE_SIZE) {
      const overflowIndex = visibleItems.length;
      const overflowNodeId = `versionOverflow/${p.id}`;
      nodes.push({
        id: overflowNodeId,
        type: "versionOverflowNode",
        position: { x: LANE_X.version, y: baseY + overflowIndex * VERSION_GAP },
        data: {
          kind: "versionOverflow",
          namespace: p.namespace,
          name: p.name,
          label: isExpanded ? "collapse" : `+${items.length - VERSION_PAGE_SIZE} more versions`,
          resourceId: p.id,
          isExpanded,
          hiddenCount: items.length - VERSION_PAGE_SIZE,
        },
      });
      edges.push({
        id: `edge-${p.id}-${overflowNodeId}`,
        source: p.id,
        target: overflowNodeId,
        style: { stroke: "#8b949e", strokeWidth: 1, strokeDasharray: "3 4", opacity: 0.5 },
        type: "smoothstep",
      });
    }
  });

  // Add one version node per Skill and Agent version.
  agentItems.forEach(({ item, kind }) => {
    const items = agentVersionItems(kind, item, agentVersionMetaByKey);
    if (items.length === 0) {
      return;
    }
    const baseY = resourceYById.get(item.id);
    if (baseY === undefined) {
      return;
    }

    const isExpanded = expandedResources.has(item.id);
    const visibleItems =
      items.length > VERSION_PAGE_SIZE && !isExpanded
        ? items.slice(0, VERSION_PAGE_SIZE)
        : items;

    visibleItems.forEach((version, index) => {
      const versionNodeId = `version/${kind}/${item.namespace}/${item.name}/${version.label}`;
      nodes.push({
        id: versionNodeId,
        type: "versionNode",
        position: { x: LANE_X.version, y: baseY + index * VERSION_GAP },
        data: {
          kind: "version",
          namespace: item.namespace,
          name: item.name,
          label: version.label,
          latestVersion: version.label,
          fileName: version.fileName,
          checksum: version.checksum,
          lastScanned: version.lastScanned,
        },
      });

      edges.push({
        id: `edge-${item.id}-${versionNodeId}`,
        source: item.id,
        target: versionNodeId,
        markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 14, height: 14 },
        style: { stroke: "#8b949e", strokeWidth: 1.6, strokeDasharray: "4 3" },
        type: "smoothstep",
      });
    });

    if (items.length > VERSION_PAGE_SIZE) {
      const overflowNodeId = `versionOverflow/${item.id}`;
      nodes.push({
        id: overflowNodeId,
        type: "versionOverflowNode",
        position: { x: LANE_X.version, y: baseY + visibleItems.length * VERSION_GAP },
        data: {
          kind: "versionOverflow",
          namespace: item.namespace,
          name: item.name,
          label: isExpanded ? "collapse" : `+${items.length - VERSION_PAGE_SIZE} more versions`,
          resourceId: item.id,
          isExpanded,
          hiddenCount: items.length - VERSION_PAGE_SIZE,
        },
      });
      edges.push({
        id: `edge-${item.id}-${overflowNodeId}`,
        source: item.id,
        target: overflowNodeId,
        style: { stroke: "#8b949e", strokeWidth: 1, strokeDasharray: "3 4", opacity: 0.5 },
        type: "smoothstep",
      });
    }
  });

  // Build edges from graph data
  const relationshipEdges: Edge[] = [];
  graph.edges.forEach((e) => {
    relationshipEdges.push({
      id: e.id,
      source: e.source,
      target: e.target,
      markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 12, height: 12 },
      style: { stroke: "#8b949e", strokeWidth: 1.5 },
      type: "smoothstep",
    });
  });

  // Fallback: when backend edges are empty, infer depot relationships from managed names.
  if (relationshipEdges.length === 0) {
    for (const depot of graph.depots) {
      for (const moduleName of depot.managedModuleNames ?? []) {
        const mod = graph.modules.find((m) => m.namespace === depot.namespace && m.name === moduleName);
        if (!mod) {
          continue;
        }
        relationshipEdges.push({
          id: `edge-${depot.id}-${mod.id}`,
          source: depot.id,
          target: mod.id,
          markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 14, height: 14 },
          style: { stroke: "#8b949e", strokeWidth: 1.6 },
          type: "smoothstep",
        });
      }

      for (const providerName of depot.managedProviderNames ?? []) {
        const prov = graph.providers.find((p) => p.namespace === depot.namespace && p.name === providerName);
        if (!prov) {
          continue;
        }
        relationshipEdges.push({
          id: `edge-${depot.id}-${prov.id}`,
          source: depot.id,
          target: prov.id,
          markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 14, height: 14 },
          style: { stroke: "#8b949e", strokeWidth: 1.6 },
          type: "smoothstep",
        });
      }

      for (const skillName of depot.managedSkillNames ?? []) {
        const skill = graph.skills.find((s) => s.namespace === depot.namespace && s.name === skillName);
        if (!skill) {
          continue;
        }
        relationshipEdges.push({
          id: `edge-${depot.id}-${skill.id}`,
          source: depot.id,
          target: skill.id,
          markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 14, height: 14 },
          style: { stroke: "#8b949e", strokeWidth: 1.6 },
          type: "smoothstep",
        });
      }

      for (const agentName of depot.managedAgentNames ?? []) {
        const agent = graph.agents.find((a) => a.namespace === depot.namespace && a.name === agentName);
        if (!agent) {
          continue;
        }
        relationshipEdges.push({
          id: `edge-${depot.id}-${agent.id}`,
          source: depot.id,
          target: agent.id,
          markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 14, height: 14 },
          style: { stroke: "#8b949e", strokeWidth: 1.6 },
          type: "smoothstep",
        });
      }
    }
  }

  relationshipEdges.forEach((e) => {
    edges.push({
      ...e,
      animated: true,
    });
  });

  return { nodes, edges };
}

// ── Main component ─────────────────────────────────────────────────────────
interface DepotGraphProps {
  graph: BrowseDepotGraph;
  moduleVersionsByKey?: Record<string, string[]>;
  providerVersionsByKey?: Record<string, string[]>;
  providerVersionMetaByKey?: Record<string, Array<{ version: string; name?: string; fileName?: string; checksum?: string; lastScanned?: string }>>;
  moduleVersionMetaByKey?: Record<string, Array<{ version: string; fileName?: string; checksum?: string; lastScanned?: string }>>;
  moduleDetailByKey?: Record<string, { storageConfig?: BrowseStorageConfig; githubAuthenticated?: boolean }>;
  agentVersionMetaByKey?: Record<string, AgentVersionMeta[]>;
}

type SelectedNode = {
  kind: NodeKind;
  namespace: string;
  name: string;
  synced?: boolean;
  provider?: string;
  providerNamespace?: string;
  storageBackend?: string;
  storageConfig?: BrowseStorageConfig;
  githubAuthenticated?: boolean;
  latestVersion?: string;
  fileName?: string;
  checksum?: string;
  lastScanned?: string;
} | null;

// Fits the whole graph on first render unless that would shrink nodes below a
// readable zoom; large graphs instead open at that zoom anchored to the top.
function InitialViewport() {
  const nodesInitialized = useNodesInitialized();
  const { getNodes, setViewport } = useReactFlow();
  const width = useStore((state) => state.width);
  const height = useStore((state) => state.height);
  const applied = React.useRef(false);

  useEffect(() => {
    if (applied.current || !nodesInitialized || !width || !height) return;
    applied.current = true;
    const bounds = getRectOfNodes(getNodes());
    const [x, y, zoom] = getTransformForBounds(bounds, width, height, MIN_ZOOM, MAX_ZOOM, FIT_PADDING);
    if (zoom >= MIN_READABLE_ZOOM) {
      setViewport({ x, y, zoom });
      return;
    }
    const graphWidth = bounds.width * MIN_READABLE_ZOOM;
    const left = graphWidth + VIEWPORT_INSET * 2 <= width ? (width - graphWidth) / 2 : VIEWPORT_INSET;
    setViewport({
      x: left - bounds.x * MIN_READABLE_ZOOM,
      y: VIEWPORT_INSET - bounds.y * MIN_READABLE_ZOOM,
      zoom: MIN_READABLE_ZOOM,
    });
  }, [nodesInitialized, width, height, getNodes, setViewport]);

  return null;
}

export default function DepotGraph({ graph, moduleVersionsByKey = {}, providerVersionsByKey = {}, providerVersionMetaByKey = {}, moduleVersionMetaByKey = {}, moduleDetailByKey = {}, agentVersionMetaByKey = {} }: DepotGraphProps) {
  const [selected, setSelected] = useState<SelectedNode>(null);
  const [expandedResources, setExpandedResources] = useState<Set<string>>(new Set());
  const [mobileMapOpen, setMobileMapOpen] = useState(false);

  useEffect(() => {
    const toggleMap = () => setMobileMapOpen((open) => !open);
    window.addEventListener(TOGGLE_NODE_MAP_EVENT, toggleMap);
    return () => window.removeEventListener(TOGGLE_NODE_MAP_EVENT, toggleMap);
  }, []);

  const { nodes, edges } = useMemo(
    () => buildGraph(graph, moduleVersionsByKey, providerVersionsByKey, providerVersionMetaByKey, moduleVersionMetaByKey, moduleDetailByKey, agentVersionMetaByKey, expandedResources),
    [graph, moduleVersionsByKey, providerVersionsByKey, providerVersionMetaByKey, moduleVersionMetaByKey, moduleDetailByKey, agentVersionMetaByKey, expandedResources],
  );

  const onNodeClick: NodeMouseHandler = useCallback((_event, node) => {
    const data = node.data as NodeData;
    if (data.kind === "versionOverflow" && data.resourceId) {
      setExpandedResources((prev) => {
        const next = new Set(prev);
        if (data.isExpanded) {
          next.delete(data.resourceId!);
        } else {
          next.add(data.resourceId!);
        }
        return next;
      });
      return;
    }
    setSelected({
      kind: data.kind,
      namespace: data.namespace,
      name: data.name,
      synced: data.synced,
      provider: data.provider,
      providerNamespace: data.providerNamespace,
      storageBackend: data.storageBackend,
      storageConfig: data.storageConfig,
      githubAuthenticated: data.githubAuthenticated,
      latestVersion: data.latestVersion,
      fileName: data.fileName,
      checksum: data.checksum,
      lastScanned: data.lastScanned,
    });
  }, []);

  if (graph.depots.length === 0 && graph.modules.length === 0 && graph.providers.length === 0 && graph.skills.length === 0 && graph.agents.length === 0) {
    return (
      <Box sx={{ display: "flex", alignItems: "center", justifyContent: "center", height: 400 }}>
        <Typography color="text.secondary">No depot relationships to display.</Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ display: "flex", height: "calc(100vh - 290px)", minHeight: 420, gap: 0 }}>
      {/* Graph canvas */}
      <Box sx={{ flex: 1, position: "relative" }}>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          onNodeClick={onNodeClick}
          minZoom={MIN_ZOOM}
          maxZoom={MAX_ZOOM}
          proOptions={{ hideAttribution: true }}
          style={{ background: "var(--mui-palette-background-default)" }}
        >
          <InitialViewport />
          <Background color="var(--mui-palette-divider)" gap={20} />
          <Controls
            fitViewOptions={{ padding: FIT_PADDING }}
            style={{
              background: "var(--mui-palette-background-paper)",
              border: "1px solid var(--mui-palette-divider)",
              borderRadius: 6,
            }}
          />
          <Box
            sx={{
              display: { xs: mobileMapOpen ? "block" : "none", sm: "block" },
              // MiniMap paints nodes with SVG fill attributes, which cannot resolve the theme's CSS variables.
              // CSS fill overrides the attribute, so kind colors are applied through these per-kind classes.
              "& .react-flow__minimap-node": { fill: VERSION_COLOR },
              "& .od-minimap-depot": { fill: DEPOT_COLOR },
              "& .od-minimap-module, & .od-minimap-provider": { fill: MODULE_COLOR },
              "& .od-minimap-skill, & .od-minimap-agent": { fill: AGENT_COLOR },
              "& .react-flow__minimap-mask": { fill: "rgba(var(--mui-palette-background-defaultChannel) / 0.7)" },
            }}
          >
            <MiniMap
              style={{
                background: "var(--mui-palette-background-paper)",
                border: "1px solid var(--mui-palette-divider)",
                borderRadius: 6,
              }}
              nodeClassName={(n) => `od-minimap-${(n.data as NodeData).kind}`}
            />
          </Box>
        </ReactFlow>
      </Box>

      {/* Side panel */}
      {selected && (
        <DepotNodePanel
          node={selected}
          onClose={() => setSelected(null)}
        />
      )}
    </Box>
  );
}
