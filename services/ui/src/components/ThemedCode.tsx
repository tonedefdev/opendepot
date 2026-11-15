"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import type { Theme } from "@mui/material/styles";
import type { SystemStyleObject } from "@mui/system";
import { Highlight, type Language, themes } from "prism-react-renderer";
import { lightCodeTheme } from "@/lib/codeTheme";
import { Prism } from "@/lib/codeHighlight";

export type CodeScheme = "dark" | "light";

// Both palettes render and CSS shows the one that matches <html data-mui-color-scheme>, which is set before
// first paint. Picking the palette from useColorScheme() would paint the wrong one until hydration completes.
const SCHEME_WRAPPER_SX: Record<CodeScheme, SystemStyleObject<Theme>> = {
  dark: { display: "block", minWidth: 0, "[data-mui-color-scheme='light'] &": { display: "none" } },
  light: { display: "none", minWidth: 0, "[data-mui-color-scheme='light'] &": { display: "block" } },
};

interface ThemedCodeProps {
  code: string;
  language: Language;
  /** Extra styles for the <pre> in each scheme, e.g. a light-only background. */
  preSx?: (scheme: CodeScheme) => SystemStyleObject<Theme>;
  /** Extra styles for each scheme's wrapper, e.g. to fill a fixed-height panel. Must not set display. */
  wrapperSx?: SystemStyleObject<Theme>;
  /** Labels the dark copy only, so the label is unique in the DOM. */
  ariaLabel?: string;
}

export function ThemedCode({ code, language, preSx, wrapperSx, ariaLabel }: ThemedCodeProps) {
  return (
    <>
      {(["dark", "light"] as const).map((scheme) => (
        <Box key={scheme} sx={[SCHEME_WRAPPER_SX[scheme], wrapperSx ?? {}]}>
          <Highlight
            prism={Prism as typeof Prism}
            theme={scheme === "dark" ? themes.nightOwl : lightCodeTheme}
            code={code}
            language={language}
          >
            {({ style, tokens, getLineProps, getTokenProps }) => (
              <Box
                component="pre"
                aria-label={scheme === "dark" ? ariaLabel : undefined}
                sx={[
                  {
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
                  },
                  preSx ? preSx(scheme) : {},
                ]}
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
        </Box>
      ))}
    </>
  );
}
