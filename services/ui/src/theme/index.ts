import { createTheme } from "@mui/material/styles";

// Brand tokens derived from docs/stylesheets/extra.css
const primary = "#047df1";
const accent = "#03deb8";
const secondary = "#04cfd0";

const theme = createTheme({
  palette: {
    mode: "dark",
    primary: {
      main: primary,
    },
    secondary: {
      main: secondary,
    },
    background: {
      default: "#0d1117",
      paper: "#161b22",
    },
    // Custom semantic colours for scan severity bands.
    error: { main: "#f85149" },
    warning: { main: "#d29922" },
    info: { main: accent },
    success: { main: "#3fb950" },
  },
  typography: {
    fontFamily: '"Inter", "Roboto", "Helvetica Neue", Arial, sans-serif',
    h1: { fontWeight: 700 },
    h2: { fontWeight: 700 },
    h3: { fontWeight: 600 },
    h4: { fontWeight: 600 },
  },
  components: {
    MuiCard: {
      styleOverrides: {
        root: {
          backgroundImage: "none",
          border: "1px solid rgba(240,246,252,0.1)",
        },
      },
    },
    MuiChip: {
      styleOverrides: {
        root: {
          fontWeight: 600,
          fontSize: "0.75rem",
        },
      },
    },
  },
});

export default theme;
