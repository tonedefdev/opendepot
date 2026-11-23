"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Link from "@mui/material/Link";
import CallSplitIcon from "@mui/icons-material/CallSplit";
import { metaChipSurfaceSx } from "./chipStyles";

interface SourceRefChipProps {
  sourceRef?: string;
  sourceCommit?: string;
  sourcePath?: string;
  repoUrl?: string;
  /** Set to false when the chip is rendered inside another link, since nested anchors are invalid. */
  linked?: boolean;
  /** Drops the chip surface so the ref reads as plain text inside a table cell or a line of metadata. */
  plain?: boolean;
}

const FULL_SHA_PATTERN = /^[0-9a-f]{40}$/i;

// A ref pinned to a full commit SHA is shortened for display. The link still uses the full value.
function displayRef(ref: string): string {
  return FULL_SHA_PATTERN.test(ref) ? ref.slice(0, 7) : ref;
}

// Links are only built for GitHub repositories, so any other host shows plain text.
function githubBaseUrl(repoUrl?: string): string | null {
  if (!repoUrl) {
    return null;
  }

  let trimmed = repoUrl;
  while (trimmed.endsWith("/")) {
    trimmed = trimmed.slice(0, -1);
  }

  return trimmed.startsWith("https://github.com/") ? trimmed : null;
}

export default function SourceRefChip({ sourceRef, sourceCommit, sourcePath, repoUrl, linked = true, plain = false }: SourceRefChipProps) {
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
      title={shortCommit ? `${sourceRef}@${shortCommit}` : sourceRef}
      sx={{
        ...(plain ? { color: "text.secondary", fontSize: "0.8125rem", verticalAlign: "middle" } : { ...metaChipSurfaceSx, px: 0.75 }),
        display: "inline-flex",
        alignItems: "center",
        gap: 0.5,
        maxWidth: "100%",
        minWidth: 0,
        boxSizing: "border-box",
        fontFamily: "var(--od-font-mono)",
        fontWeight: 500,
      }}
    >
      <CallSplitIcon sx={{ fontSize: 14, flexShrink: 0 }} />
      <Box component="span" sx={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {base ? (
          <Link href={`${base}/tree/${sourceRef}${treePath}`} target="_blank" rel="noopener noreferrer" underline="hover" sx={linkSx}>
            {displayRef(sourceRef)}
          </Link>
        ) : (
          <span>{displayRef(sourceRef)}</span>
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
    </Box>
  );
}
