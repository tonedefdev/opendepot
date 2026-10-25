import { describe, expect, it } from "vitest";
import { agentVersionItems, agentVersionKey, type AgentVersionMeta } from "./DepotGraph";
import type { BrowseGraphAgent } from "@/lib/api";

const skill: BrowseGraphAgent = {
  id: "skill/opendepot-system/context-loader",
  kind: "skill",
  namespace: "opendepot-system",
  name: "context-loader",
  synced: true,
  latestVersion: "98396d2",
};

describe("agentVersionItems", () => {
  it("falls back to the latest version when no version detail was loaded", () => {
    expect(agentVersionItems("skill", skill, {})).toEqual([{ label: "98396d2" }]);
  });

  it("returns no versions when the loaded detail has none", () => {
    const metaByKey = { [agentVersionKey("skill", "opendepot-system", "context-loader")]: [] };
    expect(agentVersionItems("skill", skill, metaByKey)).toEqual([]);
  });

  it("labels commit-only versions with the short commit and sorts versions descending", () => {
    const meta: AgentVersionMeta[] = [
      { version: "0.9.0", fileName: "a.zip" },
      { version: "", commit: "98396d2abcdef0123" },
      { version: "1.2.0", checksum: "sha" },
      { version: "0.9.0", fileName: "duplicate" },
    ];
    const metaByKey = { [agentVersionKey("skill", "opendepot-system", "context-loader")]: meta };

    const labels = agentVersionItems("skill", skill, metaByKey).map((item) => item.label);
    expect(labels).toEqual(["1.2.0", "0.9.0", "98396d2"]);
  });

  it("keeps the first file metadata for a duplicate label", () => {
    const meta: AgentVersionMeta[] = [
      { version: "0.9.0", fileName: "first.zip" },
      { version: "0.9.0", fileName: "second.zip" },
    ];
    const metaByKey = { [agentVersionKey("skill", "opendepot-system", "context-loader")]: meta };

    expect(agentVersionItems("skill", skill, metaByKey)).toEqual([
      { label: "0.9.0", fileName: "first.zip", checksum: undefined, lastScanned: undefined },
    ]);
  });
});
