import { createTheme } from "@mui/material/styles";

// Brand tokens from docs/stylesheets/extra.css
// Dark-mode palette mirrors the docs slate scheme: teal (#04cfd0) is primary,
// blue (#047df1) is demoted to primary.dark / secondary.
const primary = "#04cfd0";
const accent = "#03deb8";
const secondary = "#047df1";

const theme = createTheme({
  palette: {
    mode: "dark",
    primary: {
      main: primary,
      light: accent,
      dark: secondary,
    },
    secondary: {
      main: secondary,
      light: accent,
    },
    background: {
      default: "#0d1117",
      paper: "#161b22",
    },
    error: { main: "#f85149" },
    warning: { main: "#d29922" },
    info: { main: accent },
    success: { main: "#3fb950" },
    divider: "rgba(240,246,252,0.1)",
    text: {
      primary: "#e6edf3",
      secondary: "#8b949e",
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
    caption: { fontSize: "0.75rem", color: "#8b949e" },
  },
  shape: {
    borderRadius: 8,
  },
  components: {
    MuiCssBaseline: {
      styleOverrides: {
        body: {
          scrollbarColor: "#30363d transparent",
          "&::-webkit-scrollbar": { width: "8px" },
          "&::-webkit-scrollbar-track": { background: "transparent" },
          "&::-webkit-scrollbar-thumb": {
            background: "#30363d",
            borderRadius: "4px",
          },
        },
      },
    },
    MuiCard: {
      styleOverrides: {
        root: {
          backgroundImage: "none",
          backgroundColor: "#161b22",
          border: "1px solid rgba(240,246,252,0.1)",
          transition: "border-color 0.15s ease, box-shadow 0.15s ease",
          "&:hover": {
            borderColor: "rgba(4,207,208,0.5)",
            boxShadow: "0 0 0 1px rgba(4,207,208,0.15), 0 4px 16px rgba(0,0,0,0.4)",
          },
        },
      },
    },
    MuiChip: {
      styleOverrides: {
        root: {
          fontWeight: 600,
          fontSize: "0.75rem",
          borderRadius: "6px",
        },
        outlined: {
          borderColor: "rgba(240,246,252,0.15)",
        },
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
        root: {
          borderRadius: "6px",
          "& fieldset": {
            borderColor: "rgba(240,246,252,0.15)",
          },
          "&:hover fieldset": {
            borderColor: "rgba(240,246,252,0.3)",
          },
        },
      },
    },
    MuiTableCell: {
      styleOverrides: {
        head: {
          fontWeight: 600,
          fontSize: "0.75rem",
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          color: "#8b949e",
          borderBottomColor: "rgba(240,246,252,0.1)",
        },
        body: {
          borderBottomColor: "rgba(240,246,252,0.06)",
          fontSize: "0.875rem",
        },
      },
    },
    MuiDrawer: {
      styleOverrides: {
        paper: {
          backgroundColor: "#0d1117",
          borderRight: "1px solid rgba(240,246,252,0.08)",
        },
      },
    },
    MuiDivider: {
      styleOverrides: {
        root: {
          borderColor: "rgba(240,246,252,0.08)",
        },
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
