"use client";

import * as React from "react";
import { ThemeProvider, CssBaseline, useColorScheme } from "@mui/material";
import { AppRouterCacheProvider } from "@mui/material-nextjs/v15-appRouter";
import theme, { COLOR_MODE_COOKIE } from "@/theme";

const COLOR_MODE_COOKIE_MAX_AGE = 60 * 60 * 24 * 365; // 1 year

// Mirrors the resolved light/dark scheme into a cookie on every change so the
// server can render the matching `data-mui-color-scheme` attribute on <html>
// for returning visitors (see RootLayout). Renders nothing.
function ColorModeCookieSync() {
  const { mode, systemMode } = useColorScheme();

  React.useEffect(() => {
    const resolved = mode === "system" ? systemMode : mode;
    if (!resolved) {
      return;
    }
    document.cookie = `${COLOR_MODE_COOKIE}=${resolved}; path=/; max-age=${COLOR_MODE_COOKIE_MAX_AGE}; samesite=lax`;
  }, [mode, systemMode]);

  return null;
}

export default function ThemeRegistry({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <AppRouterCacheProvider>
      <ThemeProvider theme={theme} defaultMode="system">
        <CssBaseline />
        <ColorModeCookieSync />
        {children}
      </ThemeProvider>
    </AppRouterCacheProvider>
  );
}
