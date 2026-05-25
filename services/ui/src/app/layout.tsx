import type { Metadata } from "next";
import Box from "@mui/material/Box";
import ThemeRegistry from "@/components/ThemeRegistry";
import Sidebar, { DRAWER_WIDTH } from "@/components/Sidebar";
import { listNamespaces } from "@/lib/api";
import { getServerSessionToken, parseJWTClaims } from "@/lib/session";
import { Suspense } from "react";

export const metadata: Metadata = {
  title: "OpenDepot Registry Explorer",
  description: "Browse Terraform modules and providers in your OpenDepot registry.",
};

export default async function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const token = await getServerSessionToken();

  let namespaces: { name: string; public: boolean }[] = [];
  try {
    const nsData = await listNamespaces(token);
    namespaces = nsData.items ?? [];
  } catch {
    // If server is unavailable during layout render, sidebar will fetch client-side
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

  return (
    <html lang="en">
      <body>
        <ThemeRegistry>
          <Box sx={{ display: "flex", minHeight: "100vh", bgcolor: "background.default" }}>
            <Suspense fallback={null}>
              <Sidebar
                initialNamespaces={namespaces}
                userInfo={userInfo}
                devTokenEnabled={devTokenEnabled}
              />
            </Suspense>
            <Box
              component="main"
              sx={{
                flexGrow: 1,
                ml: { xs: 0, sm: `${DRAWER_WIDTH}px` },
                minHeight: "100vh",
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
