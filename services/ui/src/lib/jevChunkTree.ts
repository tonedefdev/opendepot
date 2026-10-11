import type {
  JevChunkResult,
  JevChunkStatus,
  JevRiskLevel,
  JevSkipReason,
  JevSkippedFile,
} from "./api";

// JevTreeRisk holds the worst values for a file, or the roll-up of them for a directory.
export interface JevTreeRisk {
  status?: JevChunkStatus;
  riskLevel?: JevRiskLevel;
  riskScore?: number;
  safeProbability?: number;
  riskConfidence?: number;
}

export interface JevTreeFile extends JevTreeRisk {
  kind: "file";
  name: string;
  path: string;
  // labels are the parts that contain the file, in part order (3, 3a, 3b).
  labels: string[];
  skipReason?: JevSkipReason;
}

export interface JevTreeDirectory extends JevTreeRisk {
  kind: "directory";
  name: string;
  path: string;
  children: JevTreeNode[];
  // fileCount and skippedCount count the file leaves below this directory.
  fileCount: number;
  skippedCount: number;
}

export type JevTreeNode = JevTreeFile | JevTreeDirectory;

export interface JevChunkTree {
  nodes: JevTreeNode[];
  // unlistedFiles is the number of files in parts whose files list was cut.
  unlistedFiles: number;
}

// Failed is the worst status, so a file that any part failed to score shows as failed.
const STATUS_RANK: Record<JevChunkStatus, number> = {
  Scored: 0,
  NotSent: 1,
  TokenLimitExceeded: 2,
  Failed: 3,
};

const RISK_RANK: Record<JevRiskLevel, number> = {
  Minimal: 0,
  Low: 1,
  Moderate: 2,
  High: 3,
  Critical: 4,
};

// unlistedChunkFiles returns how many of a part's files are not in its files list.
export function unlistedChunkFiles(chunk: JevChunkResult): number {
  return Math.max(0, chunk.fileCount - (chunk.files?.length ?? 0));
}

// buildJevChunkTree turns the parts and skipped files of a Jev assessment into a directory tree.
// Directories sort before files, and both sort by name.
export function buildJevChunkTree(
  chunks: JevChunkResult[] = [],
  skippedFiles: JevSkippedFile[] = [],
): JevChunkTree {
  const files = new Map<string, JevTreeFile>();
  let unlistedFiles = 0;

  const orderedChunks = [...chunks].sort(compareChunks);
  for (const chunk of orderedChunks) {
    unlistedFiles += unlistedChunkFiles(chunk);

    for (const filePath of chunk.files ?? []) {
      const leaf = fileLeaf(files, filePath);
      if (leaf === undefined) {
        continue;
      }

      if (!leaf.labels.includes(chunk.label)) {
        leaf.labels.push(chunk.label);
      }

      mergeRisk(leaf, chunkRisk(chunk));
    }
  }

  for (const skipped of skippedFiles) {
    const leaf = fileLeaf(files, skipped.path);
    if (leaf === undefined) {
      continue;
    }

    if (leaf.skipReason === undefined) {
      leaf.skipReason = skipped.reason;
    }
  }

  const root: JevTreeDirectory = {
    kind: "directory",
    name: "",
    path: "",
    children: [],
    fileCount: 0,
    skippedCount: 0,
  };
  const directories = new Map<string, JevTreeDirectory>([["", root]]);

  for (const leaf of files.values()) {
    const segments = leaf.path.split("/");
    let parent = root;

    for (let i = 0; i < segments.length - 1; i++) {
      const dirPath = segments.slice(0, i + 1).join("/");
      let dir = directories.get(dirPath);
      if (dir === undefined) {
        dir = {
          kind: "directory",
          name: segments[i],
          path: dirPath,
          children: [],
          fileCount: 0,
          skippedCount: 0,
        };
        directories.set(dirPath, dir);
        parent.children.push(dir);
      }

      parent = dir;
    }

    parent.children.push(leaf);
  }

  rollUp(root);

  return { nodes: root.children, unlistedFiles };
}

// fileLeaf returns the leaf for a path, creating it on first use. Empty path segments are dropped.
function fileLeaf(files: Map<string, JevTreeFile>, filePath: string): JevTreeFile | undefined {
  const segments = filePath.split("/").filter((segment) => segment !== "");
  if (segments.length === 0) {
    return undefined;
  }

  const path = segments.join("/");
  let leaf = files.get(path);
  if (leaf === undefined) {
    leaf = {
      kind: "file",
      name: segments[segments.length - 1],
      path,
      labels: [],
    };
    files.set(path, leaf);
  }

  return leaf;
}

// chunkRisk returns the values a part contributes to its files. Only a Scored part has risk values.
function chunkRisk(chunk: JevChunkResult): JevTreeRisk {
  if (chunk.status !== "Scored") {
    return { status: chunk.status };
  }

  return {
    status: chunk.status,
    riskLevel: chunk.riskLevel,
    riskScore: chunk.riskScore,
    safeProbability: chunk.safeProbability,
    riskConfidence: chunk.riskConfidence,
  };
}

// rollUp fills in the counts and worst values of each directory from its children, and sorts them.
function rollUp(dir: JevTreeDirectory): void {
  for (const child of dir.children) {
    if (child.kind === "directory") {
      rollUp(child);
      dir.fileCount += child.fileCount;
      dir.skippedCount += child.skippedCount;
    } else {
      dir.fileCount += 1;
      if (child.skipReason !== undefined) {
        dir.skippedCount += 1;
      }
    }

    mergeRisk(dir, child);
  }

  dir.children.sort(compareNodes);
}

// mergeRisk folds source into target, keeping the worst value of each field.
function mergeRisk(target: JevTreeRisk, source: JevTreeRisk): void {
  target.status = worstStatus(target.status, source.status);
  target.riskLevel = worstRiskLevel(target.riskLevel, source.riskLevel);
  target.riskScore = maxValue(target.riskScore, source.riskScore);
  target.safeProbability = minValue(target.safeProbability, source.safeProbability);
  target.riskConfidence = minValue(target.riskConfidence, source.riskConfidence);
}

function worstStatus(a?: JevChunkStatus, b?: JevChunkStatus): JevChunkStatus | undefined {
  if (a === undefined) {
    return b;
  }

  if (b === undefined) {
    return a;
  }

  return STATUS_RANK[b] > STATUS_RANK[a] ? b : a;
}

function worstRiskLevel(a?: JevRiskLevel, b?: JevRiskLevel): JevRiskLevel | undefined {
  if (a === undefined) {
    return b;
  }

  if (b === undefined) {
    return a;
  }

  return RISK_RANK[b] > RISK_RANK[a] ? b : a;
}

function maxValue(a?: number, b?: number): number | undefined {
  if (a === undefined) {
    return b;
  }

  if (b === undefined) {
    return a;
  }

  return Math.max(a, b);
}

function minValue(a?: number, b?: number): number | undefined {
  if (a === undefined) {
    return b;
  }

  if (b === undefined) {
    return a;
  }

  return Math.min(a, b);
}

function compareChunks(a: JevChunkResult, b: JevChunkResult): number {
  if (a.index !== b.index) {
    return a.index - b.index;
  }

  return compareText(a.label, b.label);
}

function compareNodes(a: JevTreeNode, b: JevTreeNode): number {
  if (a.kind !== b.kind) {
    return a.kind === "directory" ? -1 : 1;
  }

  return compareText(a.name, b.name);
}

// compareText compares by code unit so the order does not depend on the browser locale.
function compareText(a: string, b: string): number {
  if (a < b) {
    return -1;
  }

  if (a > b) {
    return 1;
  }

  return 0;
}
