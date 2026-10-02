import * as React from "react";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import PrecisionManufacturingIcon from "@mui/icons-material/PrecisionManufacturing";
import { listResources } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { redirect } from "next/navigation";
import AssemblyCanvas from "@/components/assembly/AssemblyCanvas";
import { isAssemblyProvider } from "@/lib/registrySource";
import PageHeader from "@/components/PageHeader";
import MobileMapButton from "@/components/MobileMapButton";

export default async function AssemblyPage() {
  const token = await getServerSessionToken();

  let modules: Awaited<ReturnType<typeof listResources>>["items"] = [];
  let providers: Awaited<ReturnType<typeof listResources>>["items"] = [];
  let fetchError: string | null = null;

  try {
    const [moduleResult, providerResult] = await Promise.all([
      listResources({ kind: "module", pageSize: 500, sortBy: "name", sortDir: "asc" }, token),
      listResources({ kind: "provider", pageSize: 500, sortBy: "name", sortDir: "asc" }, token),
    ]);
    modules = moduleResult.items;
    providers = providerResult.items.filter((provider) => isAssemblyProvider(provider.upstreamRegistry));
  } catch (err) {
    const msg = err instanceof Error ? err.message : "Failed to load modules.";
    if (msg.includes("401") || msg.includes("unauthorized")) {
      redirect("/auth/login");
    }
    fetchError = msg;
  }

  return (
    <Box sx={{ display: "flex", flexDirection: "column", height: "100vh" }}>
      <PageHeader
        icon={<PrecisionManufacturingIcon color="primary" fontSize="small" />}
        title="Assembly Line"
        description="Compose modules visually by wiring outputs to inputs."
        actions={<MobileMapButton />}
      />
      {fetchError && (
        <Alert severity="error" sx={{ m: 2 }}>
          {fetchError}
        </Alert>
      )}
      <Box sx={{ flex: 1, minHeight: 0 }}>
        <AssemblyCanvas modules={modules} providers={providers} />
      </Box>
    </Box>
  );
}
