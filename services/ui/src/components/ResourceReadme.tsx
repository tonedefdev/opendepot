"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import { useColorScheme } from "@mui/material/styles";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

interface ResourceReadmeProps {
  content: string;
}

export default function ResourceReadme({ content }: ResourceReadmeProps) {
  const { mode, systemMode } = useColorScheme();
  const resolvedMode = mode === "system" ? systemMode : mode;
  const codeBg = resolvedMode === "light" ? "#f0f7ff" : "#1e1e1e";

  return (
    <Box
      sx={{
        "& h1, & h2, & h3, & h4, & h5, & h6": { fontWeight: 600, mt: 2, mb: 1 },
        "& h1": { fontSize: "1.5rem" },
        "& h2": { fontSize: "1.25rem" },
        "& h3": { fontSize: "1.125rem" },
        "& p": { mb: 1.5, lineHeight: 1.6 },
        "& ul, & ol": { mb: 1.5, pl: 3 },
        "& li": { mb: 0.5 },
        "& a": { color: "primary.main", textDecoration: "none" },
        "& a:hover": { textDecoration: "underline" },
        "& code": {
          fontFamily: "monospace",
          fontSize: "0.8125rem",
          bgcolor: codeBg,
          px: 0.5,
          py: 0.25,
          borderRadius: 0.5,
        },
        "& pre": {
          bgcolor: codeBg,
          p: 2,
          borderRadius: 1.5,
          border: "1px solid",
          borderColor: "divider",
          overflowX: "auto",
        },
        "& pre code": { bgcolor: "transparent", p: 0 },
        "& blockquote": {
          borderLeft: "3px solid",
          borderColor: "divider",
          pl: 2,
          ml: 0,
          color: "text.secondary",
        },
        "& table": { borderCollapse: "collapse", mb: 1.5 },
        "& th, & td": {
          border: "1px solid",
          borderColor: "divider",
          px: 1.5,
          py: 0.75,
        },
        "& img": { maxWidth: "100%" },
      }}
    >
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
    </Box>
  );
}
