import type { Metadata } from "next";
import Box from "@mui/material/Box";
import InitColorSchemeScript from "@mui/material/InitColorSchemeScript";
import { cookies } from "next/headers";
import ThemeRegistry from "@/components/ThemeRegistry";
import Sidebar, { DRAWER_WIDTH, SIDEBAR_COLLAPSED_COOKIE } from "@/components/Sidebar";
import { getScanPolicyCatalog, listNamespaces } from "@/lib/api";
import { getServerSessionToken, parseJWTClaims } from "@/lib/session";
import { COLOR_MODE_COOKIE } from "@/theme";
import { Suspense } from "react";

export const metadata: Metadata = {
  title: "OpenDepot",
  description: "Browse Terraform modules and providers in your OpenDepot registry.",
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const token = await getServerSessionToken();

  let namespaces: { name: string; public: boolean }[] = [];
  let securityPoliciesEnabled = false;
  if (token) {
    try {
      const nsData = await listNamespaces(token);
      namespaces = nsData.items ?? [];
    } catch {
      // If server is unavailable during layout render, sidebar will fetch client-side
    }
    try {
      securityPoliciesEnabled = (await getScanPolicyCatalog(token)).writesEnabled;
    } catch {
      // Hide the policy entry when the server does not expose policy management.
    }
  }

  // Extract display claims from the id_token JWT payload (no signature verification —
  // only used for display purposes; actual auth is enforced server-side on every request).
  let userInfo: { email: string; name?: string } | null = null;
  if (token) {
    const claims = parseJWTClaims(token);
    if (claims) {
      const email = typeof claims["email"] === "string" ? claims["email"] : undefined;
      const name =
        (typeof claims["name"] === "string" ? claims["name"] : undefined) ??
        (typeof claims["preferred_username"] === "string" ? claims["preferred_username"] : undefined);
      if (email) {
        userInfo = { email, name };
      }
    }
  }

  const devTokenEnabled = process.env.DEV_TOKEN_INPUT_ENABLED === "true";

  // Read the persisted color-mode cookie so returning visitors get the
  // correct scheme rendered server-side with zero flash. First-time visitors
  // (no cookie yet) fall through to InitColorSchemeScript's blocking script,
  // which detects the OS preference before paint.
  const cookieStore = await cookies();
  const savedColorScheme = cookieStore.get(COLOR_MODE_COOKIE)?.value;
  const sidebarCollapsed = cookieStore.get(SIDEBAR_COLLAPSED_COOKIE)?.value === "true";
  const colorSchemeAttr =
    savedColorScheme === "light" || savedColorScheme === "dark" ? savedColorScheme : undefined;

  return (
    <html lang="en" data-mui-color-scheme={colorSchemeAttr} suppressHydrationWarning>
      <body>
        <InitColorSchemeScript attribute="data-mui-color-scheme" />
        <ThemeRegistry>
          <Box sx={{ display: "flex", minHeight: "100vh", bgcolor: "background.default" }}>
            <Suspense fallback={null}>
              <Sidebar
                initialNamespaces={namespaces}
                userInfo={userInfo}
                devTokenEnabled={devTokenEnabled}
                securityPoliciesEnabled={securityPoliciesEnabled}
                initialCollapsed={sidebarCollapsed}
              />
            </Suspense>
            <Box
              component="main"
              sx={{
                flexGrow: 1,
                height: "100vh",
                overflow: "auto",
              }}
            >
              {children}
            </Box>
          </Box>
        </ThemeRegistry>
      </body>
    </html>
  );
}
