"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Link from "@mui/material/Link";
import CallSplitIcon from "@mui/icons-material/CallSplit";

interface SourceRefChipProps {
  sourceRef?: string;
  sourceCommit?: string;
  sourcePath?: string;
  repoUrl?: string;
  /** Set to false when the chip is rendered inside another link, since nested anchors are invalid. */
  linked?: boolean;
}

// Links are only built for GitHub repositories, so any other host shows plain text.
function githubBaseUrl(repoUrl?: string): string | null {
  if (!repoUrl) {
    return null;
  }

  const trimmed = repoUrl.replace(/\/+$/, "");
  return trimmed.startsWith("https://github.com/") ? trimmed : null;
}

export default function SourceRefChip({ sourceRef, sourceCommit, sourcePath, repoUrl, linked = true }: SourceRefChipProps) {
  if (!sourceRef) {
    return null;
  }

  const base = linked ? githubBaseUrl(repoUrl) : null;
  const shortCommit = sourceCommit ? sourceCommit.slice(0, 7) : "";
  const treePath = sourcePath ? `/${sourcePath.replace(/^\/+/, "")}` : "";
  const linkSx = { color: "inherit", textDecorationColor: "inherit" };

  return (
    <Box
      component="span"
      sx={{
        display: "inline-flex",
        alignItems: "center",
        gap: 0.5,
        px: 1,
        py: 0.25,
        borderRadius: "999px",
        border: 1,
        borderColor: "divider",
        fontFamily: "monospace",
        fontSize: "0.75rem",
      }}
    >
      <CallSplitIcon sx={{ fontSize: 14 }} />
      {base ? (
        <Link href={`${base}/tree/${sourceRef}${treePath}`} target="_blank" rel="noopener noreferrer" underline="hover" sx={linkSx}>
          {sourceRef}
        </Link>
      ) : (
        <span>{sourceRef}</span>
      )}
      {shortCommit && (
        <>
          <span>@</span>
          {base ? (
            <Link href={`${base}/commit/${sourceCommit}`} target="_blank" rel="noopener noreferrer" underline="hover" sx={linkSx}>
              {shortCommit}
            </Link>
          ) : (
            <span>{shortCommit}</span>
          )}
        </>
      )}
    </Box>
  );
}
