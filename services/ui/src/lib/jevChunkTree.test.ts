import { describe, expect, it } from "vitest";

import type { JevChunkResult, JevSkippedFile } from "./api";
import { buildJevChunkTree, unlistedChunkFiles, type JevTreeDirectory, type JevTreeFile } from "./jevChunkTree";

function chunk(overrides: Partial<JevChunkResult> & Pick<JevChunkResult, "index" | "label">): JevChunkResult {
  return {
    fileCount: 0,
    status: "Scored",
    ...overrides,
  };
}

function dirAt(nodes: { kind: string; name: string }[], name: string): JevTreeDirectory {
  const node = nodes.find((n) => n.kind === "directory" && n.name === name);
  if (node === undefined) {
    throw new Error(`no directory ${name}`);
  }

  return node as JevTreeDirectory;
}

function fileAt(nodes: { kind: string; name: string }[], name: string): JevTreeFile {
  const node = nodes.find((n) => n.kind === "file" && n.name === name);
  if (node === undefined) {
    throw new Error(`no file ${name}`);
  }

  return node as JevTreeFile;
}

describe("buildJevChunkTree", () => {
  it("returns an empty tree for a legacy assessment with no chunks or skipped files", () => {
    expect(buildJevChunkTree(undefined, undefined)).toEqual({ nodes: [], unlistedFiles: 0 });
  });

  it("nests directories and sorts directories before files", () => {
    const tree = buildJevChunkTree(
      [
        chunk({
          index: 1,
          label: "1",
          fileCount: 4,
          files: ["top.md", "a/d.md", "a/b/c.md", "z.txt"],
        }),
      ],
      [],
    );

    expect(tree.nodes.map((n) => n.name)).toEqual(["a", "top.md", "z.txt"]);

    const a = dirAt(tree.nodes, "a");
    expect(a.path).toBe("a");
    expect(a.children.map((n) => n.name)).toEqual(["b", "d.md"]);

    const b = dirAt(a.children, "b");
    expect(b.path).toBe("a/b");
    expect(fileAt(b.children, "c.md").path).toBe("a/b/c.md");
  });

  it("gives a file in several parts the labels of those parts", () => {
    const tree = buildJevChunkTree(
      [
        chunk({ index: 3, label: "3b", fileCount: 1, files: ["shared.md"] }),
        chunk({ index: 3, label: "3", fileCount: 1, files: ["shared.md"] }),
        chunk({ index: 3, label: "3a", fileCount: 1, files: ["shared.md"] }),
      ],
      [],
    );

    expect(fileAt(tree.nodes, "shared.md").labels).toEqual(["3", "3a", "3b"]);
  });

  it("takes the worst values for a file in several scored parts", () => {
    const tree = buildJevChunkTree(
      [
        chunk({
          index: 1,
          label: "1",
          fileCount: 1,
          files: ["run.sh"],
          riskLevel: "Low",
          riskScore: 0.2,
          safeProbability: 0.9,
          riskConfidence: 0.95,
        }),
        chunk({
          index: 2,
          label: "2",
          fileCount: 1,
          files: ["run.sh"],
          riskLevel: "High",
          riskScore: 0.7,
          safeProbability: 0.4,
          riskConfidence: 0.6,
        }),
        chunk({
          index: 3,
          label: "3",
          fileCount: 1,
          files: ["run.sh"],
          riskLevel: "Moderate",
          riskScore: 0.5,
          safeProbability: 0.8,
          riskConfidence: 0.7,
        }),
      ],
      [],
    );

    const leaf = fileAt(tree.nodes, "run.sh");
    expect(leaf.riskLevel).toBe("High");
    expect(leaf.riskScore).toBe(0.7);
    expect(leaf.safeProbability).toBe(0.4);
    expect(leaf.riskConfidence).toBe(0.6);
    expect(leaf.status).toBe("Scored");
  });

  it("ranks the worst level by severity, not by label order", () => {
    const tree = buildJevChunkTree(
      [
        chunk({ index: 1, label: "1", fileCount: 1, files: ["x"], riskLevel: "Critical" }),
        chunk({ index: 2, label: "2", fileCount: 1, files: ["x"], riskLevel: "Minimal" }),
      ],
      [],
    );

    expect(fileAt(tree.nodes, "x").riskLevel).toBe("Critical");
  });

  it("does not take risk values from a part that did not score", () => {
    const tree = buildJevChunkTree(
      [
        chunk({ index: 1, label: "1", fileCount: 1, files: ["x"], riskLevel: "Low", riskScore: 0.1 }),
        chunk({
          index: 2,
          label: "2",
          fileCount: 1,
          files: ["x"],
          status: "Failed",
          riskLevel: "Critical",
          riskScore: 0.99,
          error: "jev returned 500",
        }),
      ],
      [],
    );

    const leaf = fileAt(tree.nodes, "x");
    expect(leaf.status).toBe("Failed");
    expect(leaf.riskLevel).toBe("Low");
    expect(leaf.riskScore).toBe(0.1);
  });

  it("ranks status Failed > TokenLimitExceeded > NotSent > Scored", () => {
    const statusOf = (statuses: JevChunkResult["status"][]): string | undefined => {
      const tree = buildJevChunkTree(
        statuses.map((status, i) => chunk({ index: i + 1, label: `${i + 1}`, fileCount: 1, files: ["f"], status })),
        [],
      );

      return fileAt(tree.nodes, "f").status;
    };

    expect(statusOf(["Scored", "NotSent"])).toBe("NotSent");
    expect(statusOf(["NotSent", "TokenLimitExceeded"])).toBe("TokenLimitExceeded");
    expect(statusOf(["TokenLimitExceeded", "Failed", "Scored"])).toBe("Failed");
    expect(statusOf(["Scored", "Scored"])).toBe("Scored");
  });

  it("rolls directory values up from their children", () => {
    const tree = buildJevChunkTree(
      [
        chunk({
          index: 1,
          label: "1",
          fileCount: 2,
          files: ["lib/a.ts", "lib/deep/b.ts"],
          riskLevel: "Low",
          riskScore: 0.2,
          safeProbability: 0.9,
          riskConfidence: 0.9,
        }),
        chunk({
          index: 2,
          label: "2",
          fileCount: 1,
          files: ["lib/deep/b.ts"],
          riskLevel: "High",
          riskScore: 0.6,
          safeProbability: 0.3,
          riskConfidence: 0.8,
        }),
        chunk({ index: 3, label: "3", fileCount: 1, files: ["lib/a.ts"], status: "NotSent" }),
      ],
      [{ path: "lib/deep/bad.png", reason: "Binary" }],
    );

    const lib = dirAt(tree.nodes, "lib");
    expect(lib.fileCount).toBe(3);
    expect(lib.skippedCount).toBe(1);
    expect(lib.riskLevel).toBe("High");
    expect(lib.riskScore).toBe(0.6);
    expect(lib.safeProbability).toBe(0.3);
    expect(lib.riskConfidence).toBe(0.8);
    expect(lib.status).toBe("NotSent");

    const deep = dirAt(lib.children, "deep");
    expect(deep.fileCount).toBe(2);
    expect(deep.skippedCount).toBe(1);
    expect(deep.riskScore).toBe(0.6);
  });

  it("shows the reason for skipped files", () => {
    const skippedFiles: JevSkippedFile[] = [
      { path: "assets/logo.png", reason: "Binary" },
      { path: "scripts/hook.sh", reason: "Withheld" },
    ];

    const tree = buildJevChunkTree([], skippedFiles);

    expect(fileAt(dirAt(tree.nodes, "assets").children, "logo.png").skipReason).toBe("Binary");
    expect(fileAt(dirAt(tree.nodes, "scripts").children, "hook.sh").skipReason).toBe("Withheld");
    expect(fileAt(dirAt(tree.nodes, "assets").children, "logo.png").labels).toEqual([]);
  });

  it("merges a skipped file that is also listed in a part into one leaf", () => {
    const tree = buildJevChunkTree(
      [chunk({ index: 1, label: "1", fileCount: 1, files: ["main.tf"], status: "NotSent" })],
      [{ path: "main.tf", reason: "Withheld" }],
    );

    expect(tree.nodes).toHaveLength(1);
    const leaf = fileAt(tree.nodes, "main.tf");
    expect(leaf.skipReason).toBe("Withheld");
    expect(leaf.labels).toEqual(["1"]);
    expect(leaf.status).toBe("NotSent");
  });

  it("counts files cut from a part's list as unlisted", () => {
    const files = Array.from({ length: 32 }, (_, i) => `f${String(i).padStart(2, "0")}.md`);
    const cut = chunk({ index: 1, label: "1", fileCount: 40, files });
    const whole = chunk({ index: 2, label: "2", fileCount: 2, files: ["x.md", "y.md"] });

    expect(unlistedChunkFiles(cut)).toBe(8);
    expect(unlistedChunkFiles(whole)).toBe(0);
    expect(buildJevChunkTree([cut, whole], []).unlistedFiles).toBe(8);
  });

  it("drops empty path segments and ignores empty paths", () => {
    const tree = buildJevChunkTree([chunk({ index: 1, label: "1", fileCount: 2, files: ["//a//b.md", ""] })], []);

    expect(tree.nodes).toHaveLength(1);
    expect(dirAt(tree.nodes, "a").path).toBe("a");
    expect(fileAt(dirAt(tree.nodes, "a").children, "b.md").path).toBe("a/b.md");
  });
});
