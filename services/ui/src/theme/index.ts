import { createTheme } from "@mui/material/styles";

// Cookie used to persist the user's resolved light/dark preference so the
// server can render the correct `data-mui-color-scheme` attribute on <html>
// with zero flash for returning visitors. Written client-side (see
// ThemeRegistry's ColorModeCookieSync) and read server-side in layout.tsx.
export const COLOR_MODE_COOKIE = "opendepot_color_mode";

// Brand tokens from docs/stylesheets/extra.css
// Dark-mode palette mirrors the docs slate scheme: teal (#04cfd0) is primary,
// blue (#047df1) is demoted to primary.dark / secondary. Light-mode palette
// reuses the docs site's light scheme tokens verbatim (blue primary, sky
// light variant, teal/mint demoted to secondary) so both modes draw from the
// same five-color brand palette, just swapping which hue leads.
const teal = "#04cfd0";
const mint = "#03deb8";
const blue = "#047df1";
const sky = "#05b6de";
const deepBlue = "#0350c7";

const theme = createTheme({
  cssVariables: {
    colorSchemeSelector: "data-mui-color-scheme",
  },
  colorSchemes: {
    dark: {
      palette: {
        primary: {
          main: teal,
          light: mint,
          dark: blue,
        },
        secondary: {
          main: blue,
          light: mint,
        },
        background: {
          default: "#0d1117",
          paper: "#161b22",
        },
        error: { main: "#f85149" },
        warning: { main: "#d29922" },
        info: { main: mint },
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
          main: blue,
          light: sky,
          dark: deepBlue,
        },
        secondary: {
          main: teal,
          light: mint,
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
          // Default (light-mode-friendly) elevation relies on MUI's built-in
          // Paper shadow, which reads clearly against a light page
          // background. That same dark shadow disappears against the
          // near-black dark background, so dark mode gets an explicit
          // "card stock" treatment instead: a subtle top highlight (light
          // catching the raised edge) plus a tuned drop shadow that's dark
          // enough to show up against the page.
          ...theme.applyStyles("dark", {
            backgroundImage: "linear-gradient(180deg, rgba(255,255,255,0.035), rgba(255,255,255,0) 140px)",
            boxShadow: "0 1px 0 0 rgba(255,255,255,0.06) inset, 0 10px 24px -8px rgba(0,0,0,0.55), 0 2px 6px rgba(0,0,0,0.4)",
          }),
          "&:hover": {
            borderColor: "rgba(4,207,208,0.5)",
            boxShadow: "0 0 0 1px rgba(4,207,208,0.15), 0 4px 16px rgba(0,0,0,0.4)",
          },
          ...theme.applyStyles("light", {
            "&:hover": {
              borderColor: "rgba(4,125,241,0.4)",
              boxShadow: "0 0 0 1px rgba(4,125,241,0.12), 0 4px 16px rgba(0,0,0,0.08)",
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

