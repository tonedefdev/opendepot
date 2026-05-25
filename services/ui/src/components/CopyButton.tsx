"use client";

import * as React from "react";
import { useState } from "react";
import Tooltip from "@mui/material/Tooltip";
import IconButton from "@mui/material/IconButton";
import CheckIcon from "@mui/icons-material/Check";
import ContentCopyIcon from "@mui/icons-material/ContentCopy";

interface CopyButtonProps {
  value: string;
  size?: "small" | "medium";
}

export default function CopyButton({ value, size = "small" }: CopyButtonProps) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    } catch {
      // Clipboard API may be unavailable in non-HTTPS contexts — silently ignore.
    }
  };

  return (
    <Tooltip title={copied ? "Copied!" : "Copy to clipboard"}>
      <IconButton
        size={size}
        onClick={() => void handleCopy()}
        aria-label="Copy to clipboard"
        sx={{
          color: copied ? "secondary.main" : "text.secondary",
          transition: "color 0.2s",
          p: 0.4,
        }}
      >
        {copied ? (
          <CheckIcon sx={{ fontSize: 14 }} />
        ) : (
          <ContentCopyIcon sx={{ fontSize: 14 }} />
        )}
      </IconButton>
    </Tooltip>
  );
}
