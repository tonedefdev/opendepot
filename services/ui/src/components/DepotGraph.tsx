"use client";

import * as React from "react";
import { useCallback, useMemo, useState } from "react";
import ReactFlow, {
  Background,
  Controls,
  MiniMap,
  type Edge,
  type Node,
  type NodeMouseHandler,
  MarkerType,
} from "reactflow";
import "reactflow/dist/style.css";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Chip from "@mui/material/Chip";
import type {
  BrowseDepotGraph,
  BrowseGraphDepot,
  BrowseGraphModule,
  BrowseGraphProvider,
} from "@/lib/api";
import DepotNodePanel from "./DepotNodePanel";

// ── Brand palette ─────────────────────────────────────────────────────────
const DEPOT_BORDER = "#047df1";
const DEPOT_BG = "rgba(4,125,241,0.10)";
const MODULE_BORDER = "#03deb8";
const MODULE_BG = "rgba(3,222,184,0.08)";
const PROVIDER_BORDER = "#04cfd0";
const PROVIDER_BG = "rgba(4,207,208,0.08)";

// ── Layout constants ───────────────────────────────────────────────────────
const LANE_X = { depot: 40, resource: 440 };
const DEPOT_GAP = 120;
const RESOURCE_GAP = 64;
const NODE_WIDTH = 200;
const NODE_HEIGHT = 64;

// ── Node types ─────────────────────────────────────────────────────────────
type NodeKind = "depot" | "module" | "provider";

interface NodeData {
  kind: NodeKind;
  namespace: string;
  name: string;
  label: string;
  synced?: boolean;
  provider?: string;
  providerNamespace?: string;
  storageBackend?: string;
}

function nodeStyle(kind: NodeKind): React.CSSProperties {
  const map: Record<NodeKind, { border: string; bg: string }> = {
    depot: { border: DEPOT_BORDER, bg: DEPOT_BG },
    module: { border: MODULE_BORDER, bg: MODULE_BG },
    provider: { border: PROVIDER_BORDER, bg: PROVIDER_BG },
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
    color: "#e6edf3",
    fontFamily: "inherit",
    fontSize: 13,
    display: "flex",
    flexDirection: "column" as const,
    justifyContent: "center",
    gap: 4,
    boxSizing: "border-box" as const,
  };
}

function DepotFlowNode({ data }: { data: NodeData }) {
  return (
    <div style={nodeStyle("depot")}>
      <span style={{ fontWeight: 700, fontSize: 13, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.name}
      </span>
      <span style={{ fontSize: 11, color: "#8b949e", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.namespace}
        {data.storageBackend ? ` · ${data.storageBackend}` : ""}
      </span>
    </div>
  );
}

function ModuleFlowNode({ data }: { data: NodeData }) {
  return (
    <div style={nodeStyle("module")}>
      <span style={{ fontWeight: 600, fontSize: 13, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.name}
      </span>
      <span style={{ fontSize: 11, color: "#8b949e", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
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
      <span style={{ fontWeight: 600, fontSize: 13, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.name}
      </span>
      <span style={{ fontSize: 11, color: "#8b949e", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {data.namespace}
        {data.providerNamespace ? `/${data.providerNamespace}` : ""}
        {!data.synced ? " · ⚠ unsynced" : ""}
      </span>
    </div>
  );
}

const nodeTypes = {
  depotNode: DepotFlowNode,
  moduleNode: ModuleFlowNode,
  providerNode: ProviderFlowNode,
};

// ── Layout builder ─────────────────────────────────────────────────────────
function buildGraph(graph: BrowseDepotGraph): { nodes: Node<NodeData>[]; edges: Edge[] } {
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

  // Position module nodes
  const resourceNodes: Array<{ id: string; type: string; data: NodeData }> = [];
  graph.modules.forEach((m: BrowseGraphModule) => {
    resourceNodes.push({
      id: m.id,
      type: "moduleNode",
      data: {
        kind: "module",
        namespace: m.namespace,
        name: m.name,
        label: m.name,
        synced: m.synced,
        provider: m.provider,
      },
    });
  });
  graph.providers.forEach((p: BrowseGraphProvider) => {
    resourceNodes.push({
      id: p.id,
      type: "providerNode",
      data: {
        kind: "provider",
        namespace: p.namespace,
        name: p.name,
        label: p.name,
        synced: p.synced,
        providerNamespace: p.providerNamespace,
      },
    });
  });

  resourceNodes.forEach((n, i) => {
    nodes.push({
      id: n.id,
      type: n.type,
      position: { x: LANE_X.resource, y: i * RESOURCE_GAP },
      data: n.data,
    });
  });

  // Build edges from graph data
  graph.edges.forEach((e) => {
    edges.push({
      id: e.id,
      source: e.source,
      target: e.target,
      markerEnd: { type: MarkerType.ArrowClosed, color: "#8b949e", width: 12, height: 12 },
      style: { stroke: "#8b949e", strokeWidth: 1.5 },
    });
  });

  return { nodes, edges };
}

// ── Main component ─────────────────────────────────────────────────────────
interface DepotGraphProps {
  graph: BrowseDepotGraph;
}

type SelectedNode = {
  kind: NodeKind;
  namespace: string;
  name: string;
  synced?: boolean;
  provider?: string;
  providerNamespace?: string;
  storageBackend?: string;
} | null;

export default function DepotGraph({ graph }: DepotGraphProps) {
  const [selected, setSelected] = useState<SelectedNode>(null);

  const { nodes, edges } = useMemo(() => buildGraph(graph), [graph]);

  const onNodeClick: NodeMouseHandler = useCallback((_event, node) => {
    const data = node.data as NodeData;
    setSelected({
      kind: data.kind,
      namespace: data.namespace,
      name: data.name,
      synced: data.synced,
      provider: data.provider,
      providerNamespace: data.providerNamespace,
      storageBackend: data.storageBackend,
    });
  }, []);

  if (graph.depots.length === 0 && graph.modules.length === 0 && graph.providers.length === 0) {
    return (
      <Box sx={{ display: "flex", alignItems: "center", justifyContent: "center", height: 400 }}>
        <Typography color="text.secondary">No depot relationships to display.</Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ display: "flex", height: "calc(100vh - 200px)", minHeight: 500, gap: 0 }}>
      {/* Graph canvas */}
      <Box sx={{ flex: 1, position: "relative" }}>
        {/* Legend */}
        <Box
          sx={{
            position: "absolute",
            top: 12,
            right: 12,
            zIndex: 10,
            display: "flex",
            gap: 1,
            flexWrap: "wrap",
          }}
        >
          <Chip
            label="Depot"
            size="small"
            sx={{ bgcolor: DEPOT_BG, borderColor: DEPOT_BORDER, color: DEPOT_BORDER, border: "1px solid" }}
          />
          <Chip
            label="Module"
            size="small"
            sx={{ bgcolor: MODULE_BG, borderColor: MODULE_BORDER, color: MODULE_BORDER, border: "1px solid" }}
          />
          <Chip
            label="Provider"
            size="small"
            sx={{ bgcolor: PROVIDER_BG, borderColor: PROVIDER_BORDER, color: PROVIDER_BORDER, border: "1px solid" }}
          />
        </Box>

        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          onNodeClick={onNodeClick}
          fitView
          fitViewOptions={{ padding: 0.2 }}
          minZoom={0.2}
          maxZoom={2}
          proOptions={{ hideAttribution: true }}
          style={{ background: "#0d1117" }}
        >
          <Background color="#21262d" gap={20} />
          <Controls
            style={{
              background: "#161b22",
              border: "1px solid rgba(240,246,252,0.12)",
              borderRadius: 6,
            }}
          />
          <MiniMap
            style={{ background: "#161b22", border: "1px solid rgba(240,246,252,0.12)", borderRadius: 6 }}
            nodeColor={(n) => {
              const d = n.data as NodeData;
              if (d.kind === "depot") return DEPOT_BORDER;
              if (d.kind === "module") return MODULE_BORDER;
              return PROVIDER_BORDER;
            }}
          />
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
