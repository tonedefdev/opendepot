import { createTheme } from "@mui/material/styles";
import { BRAND_BLUE, BRAND_BRIDGE, BRAND_MINT } from "@/lib/brand";

// Cookie used to persist the user's resolved light/dark preference so the
// server can render the correct `data-mui-color-scheme` attribute on <html>
// with zero flash for returning visitors. Written client-side (see
// ThemeRegistry's ColorModeCookieSync) and read server-side in layout.tsx.
export const COLOR_MODE_COOKIE = "opendepot_color_mode";

// Brand hues from the OpenDepot Brand Guidelines 2026 (see src/lib/brand.ts). Mint leads in dark mode and
// blue leads in light mode, because mint fails contrast on white and blue fails on the dark surface. The
// bridge color sits between them as the secondary hue in both modes.

const theme = createTheme({
  cssVariables: {
    colorSchemeSelector: "data-mui-color-scheme",
  },
  colorSchemes: {
    dark: {
      palette: {
        primary: {
          main: BRAND_MINT,
          dark: BRAND_BRIDGE,
        },
        secondary: {
          main: BRAND_BRIDGE,
          light: BRAND_MINT,
        },
        background: {
          default: "#0d1117",
          paper: "#161b22",
        },
        error: { main: "#f85149" },
        warning: { main: "#d29922" },
        info: { main: BRAND_MINT },
        success: { main: "#3fb950" },
        divider: "rgba(240,246,252,0.1)",
        text: {
          primary: "#e6edf3",
          secondary: "#8b949e",
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
          default: "#ffffff",
          paper: "#f6f8fa",
        },
        // Semantic feedback colors are re-tuned (not taken from the docs
        // palette) so they keep sufficient contrast against a white surface;
        // the brand hues themselves (primary/secondary above) are unchanged.
        error: { main: "#d1242f" },
        warning: { main: "#9a6700" },
        info: { main: "#037a70" },
        success: { main: "#1a7f37" },
        divider: "rgba(31,35,40,0.12)",
        text: {
          primary: "#1f2328",
          secondary: "#656d76",
        },
      },
    },
  },
  typography: {
    fontFamily: '"Inter", "Roboto", "Helvetica Neue", Arial, sans-serif',
    h1: { fontWeight: 700, letterSpacing: "-0.025em" },
    h2: { fontWeight: 700, letterSpacing: "-0.02em" },
    h3: { fontWeight: 600, letterSpacing: "-0.015em" },
    h4: { fontWeight: 600, letterSpacing: "-0.01em" },
    h5: { fontWeight: 600 },
    h6: { fontWeight: 600 },
    body1: { fontSize: "0.9375rem" },
    body2: { fontSize: "0.875rem" },
    caption: { fontSize: "0.75rem" },
  },
  shape: {
    borderRadius: 8,
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: (theme) => ({
        // Mode-aware brand and kind accents. Inline styles (ReactFlow nodes) cannot read the theme, so they
        // reference these variables. Mint fails contrast on white, so AI kinds and the accent use blue in light mode.
        ":root": {
          "--od-accent": BRAND_MINT,
          "--od-kind-iac": BRAND_BRIDGE,
          "--od-kind-ai": BRAND_MINT,
          "--od-kind-version": "#8b949e",
          // Assembly Line node icon colors. Light mode uses the for_each chip's teal so the icon and minimap read on the card.
          "--od-assembly-variable": BRAND_BRIDGE,
          "--od-assembly-module": BRAND_MINT,
        },
        '[data-mui-color-scheme="light"]': {
          "--od-accent": BRAND_BLUE,
          "--od-kind-ai": BRAND_BLUE,
          "--od-kind-version": "#6e7781",
          "--od-assembly-variable": BRAND_BLUE,
          "--od-assembly-module": "#037a70",
        },
        body: {
          scrollbarColor: `${theme.vars.palette.divider} transparent`,
          "&::-webkit-scrollbar": { width: "8px" },
          "&::-webkit-scrollbar-track": { background: "transparent" },
          "&::-webkit-scrollbar-thumb": {
            background: "#30363d",
            borderRadius: "4px",
          },
          ...theme.applyStyles("light", {
            "&::-webkit-scrollbar-thumb": {
              background: "#d0d7de",
            },
          }),
        },
      }),
    },
    MuiTooltip: {
      styleOverrides: {
        tooltip: ({ theme }) => ({
          backgroundColor: "#24292f",
          color: "#f6f8fa",
          border: `1px solid ${theme.vars.palette.divider}`,
          boxShadow: "0 8px 24px rgba(0,0,0,0.35)",
          ...theme.applyStyles("dark", {
            backgroundColor: "#2d333b",
          }),
        }),
        arrow: ({ theme }) => ({
          color: "#24292f",
          ...theme.applyStyles("dark", {
            color: "#2d333b",
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
    MuiCard: {
      styleOverrides: {
        root: ({ theme }) => ({
          backgroundImage: "none",
          backgroundColor: theme.vars.palette.background.paper,
          border: `1px solid ${theme.vars.palette.divider}`,
          transition: "border-color 0.15s ease, box-shadow 0.15s ease",
          ...theme.applyStyles("dark", {
            backgroundImage: "linear-gradient(180deg, rgba(255,255,255,0.035), rgba(255,255,255,0) 140px)",
            boxShadow: "0 1px 0 0 rgba(255,255,255,0.06) inset, 0 10px 24px -8px rgba(0,0,0,0.55), 0 2px 6px rgba(0,0,0,0.4)",
          }),
          "&:hover": {
            borderColor: `rgba(${theme.vars.palette.primary.mainChannel} / 0.5)`,
            boxShadow: `0 0 0 1px rgba(${theme.vars.palette.primary.mainChannel} / 0.15), 0 4px 16px rgba(0,0,0,0.4)`,
          },
          ...theme.applyStyles("light", {
            "&:hover": {
              borderColor: `rgba(${theme.vars.palette.primary.mainChannel} / 0.4)`,
              boxShadow: `0 0 0 1px rgba(${theme.vars.palette.primary.mainChannel} / 0.12), 0 4px 16px rgba(0,0,0,0.08)`,
            },
          }),
        }),
      },
    },
    MuiChip: {
      styleOverrides: {
        root: {
          fontWeight: 600,
          fontSize: "0.75rem",
          borderRadius: "6px",
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
    MuiTableCell: {
      styleOverrides: {
        head: ({ theme }) => ({
          fontWeight: 600,
          fontSize: "0.75rem",
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          color: theme.vars.palette.text.secondary,
          borderBottomColor: theme.vars.palette.divider,
        }),
        body: ({ theme }) => ({
          borderBottomColor: theme.vars.palette.divider,
          fontSize: "0.875rem",
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

