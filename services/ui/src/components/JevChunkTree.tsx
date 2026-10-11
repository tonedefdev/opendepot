"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Collapse from "@mui/material/Collapse";
import List from "@mui/material/List";
import ListItem from "@mui/material/ListItem";
import ListItemButton from "@mui/material/ListItemButton";
import ToggleButton from "@mui/material/ToggleButton";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";
import Typography from "@mui/material/Typography";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import FolderOutlinedIcon from "@mui/icons-material/FolderOutlined";
import ViewStreamOutlinedIcon from "@mui/icons-material/ViewStreamOutlined";
import type { JevAssessment, JevChunkResult, JevChunkStatus, JevSkipReason, JevSkippedFile } from "@/lib/api";
import {
  buildJevChunkTree,
  unlistedChunkFiles,
  type JevChunkTree as JevChunkTreeData,
  type JevTreeDirectory,
  type JevTreeFile,
  type JevTreeNode,
  type JevTreeRisk,
} from "@/lib/jevChunkTree";
import { RISK_COLORS } from "@/lib/jevSeverity";

interface JevChunkTreeProps {
  assessment: JevAssessment;
}

type TreeView = "files" | "chunks";

const SKIP_REASON_TEXT: Record<JevSkipReason, string> = {
  Withheld: "Withheld by scan",
  Binary: "Binary, not read",
};

const STATUS_COLORS: Record<JevChunkStatus, "default" | "success" | "error"> = {
  Scored: "success",
  NotSent: "default",
  Failed: "error",
  TokenLimitExceeded: "error",
};

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
}

// JevChunkTree shows which files Jev read, either as a directory tree or as one row per part.
// Legacy assessments have none of chunks, skippedFiles or chunkCount, so they render nothing.
export default function JevChunkTree({ assessment }: JevChunkTreeProps) {
  const [view, setView] = React.useState<TreeView>("files");
  const tree = React.useMemo(
    () => buildJevChunkTree(assessment.chunks, assessment.skippedFiles),
    [assessment.chunks, assessment.skippedFiles],
  );

  if (assessment.chunks === undefined && assessment.skippedFiles === undefined && assessment.chunkCount === undefined) {
    return null;
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", justifyContent: "space-between", gap: 1 }}>
        <Typography variant="body2">Assessed content</Typography>
        <ToggleButtonGroup
          size="small"
          exclusive
          value={view}
          aria-label="Assessed content view"
          onChange={(_, next: TreeView | null) => {
            if (next !== null) setView(next);
          }}
        >
          <ToggleButton value="files">
            <FolderOutlinedIcon fontSize="inherit" />
            Files
          </ToggleButton>
          <ToggleButton value="chunks">
            <ViewStreamOutlinedIcon fontSize="inherit" />
            Chunks
          </ToggleButton>
        </ToggleButtonGroup>
      </Box>

      {view === "files" ? (
        <FilesView tree={tree} />
      ) : (
        <ChunksView chunks={assessment.chunks ?? []} skippedFiles={assessment.skippedFiles ?? []} />
      )}
    </Box>
  );
}

function FilesView({ tree }: { tree: JevChunkTreeData }) {
  if (tree.nodes.length === 0) {
    return (
      <Typography variant="caption" color="text.secondary">
        No files were sent to Jev.
      </Typography>
    );
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
      <List dense disablePadding sx={{ border: 1, borderColor: "divider", borderRadius: 1 }}>
        <TreeNodes nodes={tree.nodes} depth={0} />
      </List>
      {tree.unlistedFiles > 0 && (
        <Typography variant="caption" color="text.secondary">
          {`+${tree.unlistedFiles} more files not listed`}
        </Typography>
      )}
    </Box>
  );
}

function TreeNodes({ nodes, depth }: { nodes: JevTreeNode[]; depth: number }) {
  return (
    <>
      {nodes.map((node) =>
        node.kind === "directory" ? (
          <DirectoryRow key={node.path} dir={node} depth={depth} />
        ) : (
          <FileRow key={node.path} file={node} depth={depth} />
        ),
      )}
    </>
  );
}

function DirectoryRow({ dir, depth }: { dir: JevTreeDirectory; depth: number }) {
  const [open, setOpen] = React.useState(false);
  const childrenId = React.useId();

  return (
    <>
      <ListItemButton
        dense
        aria-expanded={open}
        aria-controls={childrenId}
        onClick={() => setOpen((value) => !value)}
        sx={{ pl: 1 + depth * 2, gap: 1, flexWrap: "wrap" }}
      >
        <ExpandMoreIcon
          fontSize="small"
          sx={{ color: "text.secondary", transform: open ? "rotate(0deg)" : "rotate(-90deg)", transition: "transform 150ms" }}
        />
        <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
          {`${dir.name}/`}
        </Typography>
        <RiskChips risk={dir} />
        {dir.skippedCount > 0 && (
          <Chip size="small" variant="outlined" color="error" label={`${dir.skippedCount} skipped`} />
        )}
        <Typography variant="caption" color="text.secondary">
          {`${dir.fileCount} files`}
        </Typography>
      </ListItemButton>
      <Collapse in={open} unmountOnExit id={childrenId}>
        <List dense disablePadding>
          <TreeNodes nodes={dir.children} depth={depth + 1} />
        </List>
      </Collapse>
    </>
  );
}

function FileRow({ file, depth }: { file: JevTreeFile; depth: number }) {
  return (
    <ListItem dense sx={{ pl: 1 + depth * 2 + 3, gap: 1, flexWrap: "wrap" }}>
      <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
        {file.name}
      </Typography>
      <RiskChips risk={file} />
      {file.skipReason !== undefined && (
        <Chip size="small" variant="outlined" color="error" label={SKIP_REASON_TEXT[file.skipReason]} />
      )}
      {file.labels.length > 0 && (
        <Typography variant="caption" color="text.secondary">
          Parts
        </Typography>
      )}
      {file.labels.map((label) => (
        <Chip key={label} size="small" variant="outlined" label={label} />
      ))}
    </ListItem>
  );
}

// RiskChips shows the risk level, and the status when a part for this entry did not score.
function RiskChips({ risk }: { risk: JevTreeRisk }) {
  return (
    <>
      {risk.riskLevel !== undefined && (
        <Chip size="small" variant="outlined" color={RISK_COLORS[risk.riskLevel]} label={risk.riskLevel} />
      )}
      {risk.status !== undefined && risk.status !== "Scored" && (
        <Chip size="small" variant="outlined" color={STATUS_COLORS[risk.status]} label={risk.status} />
      )}
    </>
  );
}

function ChunksView({ chunks, skippedFiles }: { chunks: JevChunkResult[]; skippedFiles: JevSkippedFile[] }) {
  if (chunks.length === 0 && skippedFiles.length === 0) {
    return (
      <Typography variant="caption" color="text.secondary">
        No parts were sent to Jev.
      </Typography>
    );
  }

  return (
    <List dense disablePadding sx={{ border: 1, borderColor: "divider", borderRadius: 1 }}>
      {chunks.map((chunk) => (
        <ChunkRow key={chunk.label} chunk={chunk} />
      ))}
      {skippedFiles.map((skipped) => (
        <ListItem key={skipped.path} dense sx={{ gap: 1, flexWrap: "wrap" }}>
          <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
            {skipped.path}
          </Typography>
          <Chip size="small" variant="outlined" color="error" label={SKIP_REASON_TEXT[skipped.reason]} />
        </ListItem>
      ))}
    </List>
  );
}

function ChunkRow({ chunk }: { chunk: JevChunkResult }) {
  const [open, setOpen] = React.useState(false);
  const filesId = React.useId();
  const files = chunk.files ?? [];
  const unlisted = unlistedChunkFiles(chunk);
  const showError = (chunk.status === "Failed" || chunk.status === "TokenLimitExceeded") && chunk.error;

  return (
    <>
      <ListItemButton
        dense
        aria-expanded={open}
        aria-controls={filesId}
        onClick={() => setOpen((value) => !value)}
        sx={{ gap: 1, flexWrap: "wrap" }}
      >
        <ExpandMoreIcon
          fontSize="small"
          sx={{ color: "text.secondary", transform: open ? "rotate(0deg)" : "rotate(-90deg)", transition: "transform 150ms" }}
        />
        <Typography variant="caption" color="text.secondary">
          Part
        </Typography>
        <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
          {chunk.label}
        </Typography>
        <Chip size="small" variant="outlined" color={STATUS_COLORS[chunk.status]} label={chunk.status} />
        {chunk.riskLevel !== undefined && (
          <Chip size="small" variant="outlined" color={RISK_COLORS[chunk.riskLevel]} label={chunk.riskLevel} />
        )}
        {chunk.riskScore !== undefined && (
          <Typography variant="caption">{`Score ${chunk.riskScore.toFixed(2)}`}</Typography>
        )}
        {chunk.safeProbability !== undefined && (
          <Typography variant="caption">{`Safe ${formatPercent(chunk.safeProbability)}`}</Typography>
        )}
        {chunk.riskConfidence !== undefined && (
          <Typography variant="caption">{`Confidence ${formatPercent(chunk.riskConfidence)}`}</Typography>
        )}
        <Typography variant="caption" color="text.secondary">
          {`${chunk.fileCount} files`}
        </Typography>
      </ListItemButton>
      {showError && (
        <Typography variant="caption" color="error.main" sx={{ display: "block", pl: 6, pb: 0.5 }}>
          {chunk.error}
        </Typography>
      )}
      <Collapse in={open} unmountOnExit id={filesId}>
        <List dense disablePadding sx={{ pl: 5 }}>
          {files.map((path) => (
            <ListItem key={path} dense>
              <Typography variant="body2" sx={{ fontFamily: "monospace" }}>
                {path}
              </Typography>
            </ListItem>
          ))}
          {unlisted > 0 && (
            <ListItem dense>
              <Typography variant="caption" color="text.secondary">
                {`+${unlisted} more`}
              </Typography>
            </ListItem>
          )}
        </List>
      </Collapse>
    </>
  );
}
