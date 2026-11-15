"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import { useColorScheme } from "@mui/material/styles";
import type { Components } from "react-markdown";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeSanitize from "rehype-sanitize";
import { Highlight, type Language, themes } from "prism-react-renderer";
import { lightCodeTheme } from "@/lib/codeTheme";
import CopyButton from "@/components/CopyButton";
import { markdownTableComponents } from "@/components/MarkdownTable";
import { Prism, resolvePrismLanguage } from "@/lib/codeHighlight";

interface HighlightedCodeBlockProps {
  code: string;
  language: string;
}

export function HighlightedCodeBlock({ code, language }: HighlightedCodeBlockProps) {
  const { mode, systemMode } = useColorScheme();
  const resolvedMode = mode === "system" ? systemMode : mode;
  const prismTheme = resolvedMode === "light" ? lightCodeTheme : themes.nightOwl;

  return (
    <Box sx={{ position: "relative", mb: 1.5 }}>
      <Highlight prism={Prism as typeof Prism} theme={prismTheme} code={code} language={language as Language}>
        {({ style, tokens, getLineProps, getTokenProps }) => (
          <Box
            component="pre"
            sx={{
              m: 0,
              p: 2,
              pr: 6,
              borderRadius: 1.5,
              border: "1px solid",
              borderColor: "divider",
              fontFamily: "var(--od-font-mono)",
              fontSize: "0.8125rem",
              lineHeight: 1.65,
              overflowX: "auto",
              whiteSpace: "pre",
              ...style,
              // Code sits in a well sunk to the canvas color, like every other inset surface.
              backgroundColor: "background.default",
            }}
          >
            {tokens.map((line, i) => (
              <div key={i} {...getLineProps({ line })}>
                {line.map((token, key) => (
                  <span key={key} {...getTokenProps({ token })} />
                ))}
              </div>
            ))}
          </Box>
        )}
      </Highlight>
      <Box sx={{ position: "absolute", top: 6, right: 6 }}>
        <CopyButton value={code} />
      </Box>
    </Box>
  );
}

interface MarkdownCodeOptions {
  /** Render ```markdown fences as markdown instead of code. Used for skill and agent bodies. */
  renderEmbeddedMarkdown?: boolean;
}

function EmbeddedMarkdown({ code }: { code: string }) {
  return (
    <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeSanitize]} components={highlightedMarkdownComponents({ renderEmbeddedMarkdown: true })}>
      {code}
    </ReactMarkdown>
  );
}

// Fenced blocks with a supported language are highlighted; anything else stays plain monospace.
export function highlightedMarkdownComponents({ renderEmbeddedMarkdown = false }: MarkdownCodeOptions = {}): Components {
  return {
    ...markdownTableComponents,
    pre({ children }) {
      const codeElement = React.isValidElement(children) ? children : null;
      const codeProps = codeElement?.props as { className?: string; children?: React.ReactNode } | undefined;
      const lang = /language-(\w+)/.exec(codeProps?.className || "")?.[1];
      const code = String(codeProps?.children ?? "").replace(/\n$/, "");

      if (renderEmbeddedMarkdown && (lang === "markdown" || lang === "md")) {
        return <EmbeddedMarkdown code={code} />;
      }

      const language = lang ? resolvePrismLanguage(lang) : null;
      if (!language) {
        return <pre className="md-plain-pre">{children}</pre>;
      }

      return <HighlightedCodeBlock code={code} language={language} />;
    },
  };
}
