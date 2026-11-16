import { createTheme, darken } from "@mui/material/styles";
import { BRAND_BLUE, BRAND_BRIDGE, BRAND_MINT } from "@/lib/brand";

// Cookie used to persist the user's resolved light/dark preference so the
// server can render the correct `data-mui-color-scheme` attribute on <html>
// with zero flash for returning visitors. Written client-side (see
// ThemeRegistry's ColorModeCookieSync) and read server-side in layout.tsx.
export const COLOR_MODE_COOKIE = "opendepot_color_mode";

// Aspekta is the brand's primary typeface. Inter stays in the stack as the brand's supporting face and as
// the fallback while Aspekta loads. Both web fonts are self-hosted from public/fonts under the OFL.
export const FONT_SANS = '"Aspekta", "Inter Variable", "Inter", "Helvetica Neue", Arial, sans-serif';
// Monospace is reserved for values a person would paste into a terminal or HCL: versions, refs, digests, paths.
export const FONT_MONO = '"JetBrains Mono", ui-monospace, "SF Mono", Menlo, Consolas, monospace';

// Brand hues from the OpenDepot Brand Guidelines 2026 (see src/lib/brand.ts). Mint leads in dark mode and
// blue leads in light mode, because mint fails contrast on white and blue fails on black. The bridge color
// sits between them as the secondary hue in both modes.
//
// Surfaces follow one rule in both modes: background.default is the lowest layer (the canvas, and wells
// sunk into cards) and background.paper is raised above it. Dark mode sits on the brand black and light
// mode puts white paper on a cool grey canvas.

const theme = createTheme({
  cssVariables: {
    colorSchemeSelector: "data-mui-color-scheme",
  },
  colorSchemes: {
    dark: {
      palette: {
        primary: {
          main: BRAND_MINT,
          // Contained buttons hover to primary.dark. A deeper shade of mint keeps the hover in the same hue.
          dark: darken(BRAND_MINT, 0.2),
        },
        secondary: {
          main: BRAND_BRIDGE,
          light: BRAND_MINT,
        },
        background: {
          default: "#000000",
          paper: "#0c0e11",
        },
        error: { main: "#ff6257" },
        warning: { main: "#e0a526" },
        info: { main: BRAND_MINT },
        success: { main: "#3fb950" },
        divider: "rgba(255,255,255,0.11)",
        text: {
          primary: "#edf0f2",
          secondary: "#8d959e",
        },
      },
    },
    light: {
      palette: {
        primary: {
          main: BRAND_BLUE,
          light: BRAND_BRIDGE,
        },
        secondary: {
          main: BRAND_BRIDGE,
          light: BRAND_MINT,
        },
        background: {
          default: "#f3f5f8",
          paper: "#ffffff",
        },
        // Semantic feedback colors are tuned to keep 4.5:1 text contrast on white paper and on their tinted chips.
        error: { main: "#b4232c" },
        warning: { main: "#7d5300" },
        info: { main: "#02675f" },
        success: { main: "#17703a" },
        divider: "rgba(11,13,16,0.12)",
        text: {
          primary: "#0b0d10",
          secondary: "#5b6470",
          disabled: "#6b7480",
        },
      },
    },
  },
  typography: {
    fontFamily: FONT_SANS,
    // h1 to h3 follow the brand type scale (36/38, 24/28, 18/21). h4 to h6 stay as compact UI headings.
    h1: { fontSize: "2.25rem", lineHeight: 38 / 36, fontWeight: 650, letterSpacing: "-0.025em" },
    h2: { fontSize: "1.5rem", lineHeight: 28 / 24, fontWeight: 650, letterSpacing: "-0.02em" },
    h3: { fontSize: "1.125rem", lineHeight: 21 / 18, fontWeight: 600, letterSpacing: "-0.01em" },
    h4: { fontWeight: 650, letterSpacing: "-0.02em" },
    h5: { fontWeight: 600, letterSpacing: "-0.015em" },
    h6: { fontWeight: 600, letterSpacing: "-0.01em" },
    body1: { fontSize: "0.9375rem" },
    body2: { fontSize: "0.875rem" },
    caption: { fontSize: "0.75rem" },
    button: { fontWeight: 600 },
  },
  shape: {
    borderRadius: 8,
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: (theme) => ({
        // Mode-aware brand and kind accents. Inline styles (ReactFlow nodes) cannot read the theme, so they
        // reference these variables. Mint fails contrast on white, so the accent uses blue in light mode.
        ":root": {
          "--od-font-mono": FONT_MONO,
          "--od-accent": BRAND_MINT,
          "--od-kind-version": "#a0a8b0",
          // Assembly Line node colors. Modules use the primary accent so they match the for_each shadow; variables take the teal in light mode.
          "--od-assembly-variable": BRAND_BRIDGE,
          "--od-assembly-module": BRAND_MINT,
          // The rule under a table head and above a page footer: one step stronger than a row divider.
          "--od-rule-strong": "rgba(255,255,255,0.22)",
        },
        '[data-mui-color-scheme="light"]': {
          "--od-accent": BRAND_BLUE,
          "--od-kind-version": "#6b7480",
          "--od-assembly-variable": "#037a70",
          "--od-assembly-module": BRAND_BLUE,
          "--od-rule-strong": "rgba(11,13,16,0.28)",
        },
        body: {
          // JetBrains Mono joins sequences such as >= and != into single glyphs. Versions, constraints, and HCL must read literally.
          fontVariantLigatures: "no-contextual",
          scrollbarColor: `${theme.vars.palette.primary.main} transparent`,
          "&::-webkit-scrollbar": { width: "8px" },
          "&::-webkit-scrollbar-track": { background: "transparent" },
          "&::-webkit-scrollbar-thumb": {
            background: theme.vars.palette.primary.main,
            borderRadius: "4px",
          },
        },
        "code, pre, kbd, samp": {
          fontFamily: FONT_MONO,
        },
        "a:focus-visible": {
          outline: `2px solid ${theme.vars.palette.primary.main}`,
          outlineOffset: 2,
          borderRadius: 2,
        },
        // ReactFlow ships light-only chrome. These keep its controls and edge labels on the app's surfaces in both modes.
        ".react-flow__controls": {
          boxShadow: "none",
          overflow: "hidden",
        },
        ".react-flow__controls .react-flow__controls-button": {
          background: theme.vars.palette.background.paper,
          borderBottom: `1px solid ${theme.vars.palette.divider}`,
          color: theme.vars.palette.text.primary,
          "&:hover": { background: theme.vars.palette.action.hover },
          "&:last-of-type": { borderBottom: 0 },
          "& svg": { fill: "currentColor" },
        },
        // ReactFlow's default handle ring is white, which shows as a halo on dark surfaces.
        ".react-flow__node .react-flow__handle": {
          border: "1px solid transparent",
          backgroundClip: "padding-box",
        },
        ".react-flow__edge-textbg": {
          fill: theme.vars.palette.background.paper,
        },
        ".react-flow__minimap-mask": {
          fill: `rgba(${theme.vars.palette.background.defaultChannel} / 0.7)`,
        },
      }),
    },
    MuiTooltip: {
      styleOverrides: {
        tooltip: ({ theme }) => ({
          backgroundColor: "#0b0d10",
          color: "#f3f5f8",
          border: `1px solid ${theme.vars.palette.divider}`,
          boxShadow: "0 8px 24px rgba(0,0,0,0.28)",
          fontSize: "0.75rem",
          fontWeight: 500,
          ...theme.applyStyles("dark", {
            backgroundColor: "#1c2026",
          }),
        }),
        arrow: ({ theme }) => ({
          color: "#0b0d10",
          ...theme.applyStyles("dark", {
            color: "#1c2026",
          }),
        }),
      },
    },
    MuiTypography: {
      styleOverrides: {
        caption: ({ theme }) => ({
          color: theme.vars.palette.text.secondary,
        }),
      },
    },
    // Raised surfaces are told apart by a hairline, not by a shadow or a hover effect. Only surfaces that
    // float over the page (menus, popovers, dialogs) keep a shadow.
    MuiPaper: {
      styleOverrides: {
        root: { backgroundImage: "none" },
        outlined: ({ theme }) => ({
          borderColor: theme.vars.palette.divider,
          borderRadius: 10,
        }),
      },
    },
    MuiCard: {
      styleOverrides: {
        root: ({ theme }) => ({
          backgroundColor: theme.vars.palette.background.paper,
          border: `1px solid ${theme.vars.palette.divider}`,
          borderRadius: 10,
          boxShadow: "none",
        }),
      },
    },
    MuiMenu: {
      styleOverrides: {
        paper: ({ theme }) => ({
          border: `1px solid ${theme.vars.palette.divider}`,
        }),
      },
    },
    MuiPopover: {
      styleOverrides: {
        paper: ({ theme }) => ({
          border: `1px solid ${theme.vars.palette.divider}`,
        }),
      },
    },
    MuiChip: {
      styleOverrides: {
        root: {
          fontWeight: 600,
          fontSize: "0.75rem",
          borderRadius: "4px",
        },
        outlined: ({ theme }) => ({
          borderColor: theme.vars.palette.divider,
        }),
      },
    },
    MuiButton: {
      styleOverrides: {
        root: {
          textTransform: "none",
          fontWeight: 600,
          borderRadius: "6px",
        },
        contained: {
          boxShadow: "none",
          "&:hover": { boxShadow: "none" },
        },
      },
    },
    MuiToggleButton: {
      styleOverrides: {
        root: ({ theme }) => ({
          textTransform: "none",
          fontWeight: 600,
          borderRadius: "6px",
          borderColor: theme.vars.palette.divider,
        }),
      },
    },
    MuiOutlinedInput: {
      styleOverrides: {
        root: ({ theme }) => ({
          borderRadius: "6px",
          "& fieldset": {
            borderColor: theme.vars.palette.divider,
          },
          "&:hover fieldset": {
            borderColor: theme.vars.palette.text.secondary,
          },
        }),
      },
    },
    // Tables are the product's main reading surface: compact rows and aligned figures. Light mode fills the
    // header row with the brand blue so the column labels anchor each table; dark mode keeps a plain head rule.
    MuiTableCell: {
      styleOverrides: {
        root: {
          padding: "10px 16px",
          fontVariantNumeric: "tabular-nums",
        },
        head: ({ theme }) => ({
          paddingTop: 8,
          paddingBottom: 8,
          fontWeight: 600,
          fontSize: "0.75rem",
          lineHeight: 1.5,
          color: theme.vars.palette.text.secondary,
          borderBottomColor: "var(--od-rule-strong)",
          [theme.getColorSchemeSelector("light")]: {
            backgroundColor: theme.vars.palette.primary.main,
            color: theme.vars.palette.common.white,
            borderBottomColor: theme.vars.palette.primary.main,
            "& .MuiTableSortLabel-root, & .MuiTableSortLabel-root:hover, & .MuiTableSortLabel-root.Mui-active": {
              color: "inherit",
            },
            "& .MuiTableSortLabel-root .MuiTableSortLabel-icon, & .MuiTableSortLabel-root.Mui-active .MuiTableSortLabel-icon": {
              color: theme.vars.palette.common.white,
            },
          },
        }),
        body: ({ theme }) => ({
          borderBottomColor: theme.vars.palette.divider,
          fontSize: "0.875rem",
        }),
        // Sticky heads need an opaque fill so scrolled rows don't show through.
        stickyHeader: ({ theme }) => ({
          backgroundColor: theme.vars.palette.background.paper,
          [theme.getColorSchemeSelector("light")]: {
            backgroundColor: theme.vars.palette.primary.main,
          },
        }),
      },
    },
    MuiDrawer: {
      styleOverrides: {
        paper: ({ theme }) => ({
          backgroundColor: theme.vars.palette.background.default,
          borderRight: `1px solid ${theme.vars.palette.divider}`,
        }),
      },
    },
    MuiDivider: {
      styleOverrides: {
        root: ({ theme }) => ({
          borderColor: theme.vars.palette.divider,
        }),
      },
    },
    MuiAlert: {
      styleOverrides: {
        root: {
          borderRadius: "8px",
        },
      },
    },
  },
});

export default theme;
