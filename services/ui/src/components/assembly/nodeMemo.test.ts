// @vitest-environment jsdom

import type { NodeProps } from "reactflow";
import { describe, expect, it } from "vitest";
import { areAssemblyNodePropsEqual } from "./nodeMemo";

function nodeProps(data: Record<string, unknown>, changes: Partial<NodeProps<Record<string, unknown>>> = {}) {
  return {
    id: "module-1",
    type: "module",
    xPos: 0,
    yPos: 0,
    selected: false,
    dragging: false,
    data,
    ...changes,
  } as NodeProps<Record<string, unknown>>;
}

describe("areAssemblyNodePropsEqual", () => {
  it("ignores callback identity when node data is otherwise unchanged", () => {
    expect(areAssemblyNodePropsEqual(
      nodeProps({ label: "module", onChange: () => undefined }),
      nodeProps({ label: "module", onChange: () => undefined }),
    )).toBe(true);
  });

  it("detects meaningful data changes", () => {
    expect(areAssemblyNodePropsEqual(
      nodeProps({ label: "module", values: { count: 1 } }),
      nodeProps({ label: "module", values: { count: 2 } }),
    )).toBe(false);
  });

  it("detects position changes", () => {
    expect(areAssemblyNodePropsEqual(
      nodeProps({ label: "module" }),
      nodeProps({ label: "module" }, { xPos: 20 }),
    )).toBe(false);
  });
});
