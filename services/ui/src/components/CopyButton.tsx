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
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(value);
      } else {
        // Fallback for non-HTTPS contexts (e.g. local dev with a custom hostname).
        const textarea = document.createElement("textarea");
        textarea.value = value;
        textarea.style.cssText = "position:fixed;top:0;left:0;opacity:0;pointer-events:none";
        document.body.appendChild(textarea);
        textarea.focus();
        textarea.select();
        document.execCommand("copy");
        document.body.removeChild(textarea);
      }
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    } catch {
      // If all clipboard methods fail, silently ignore.
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
