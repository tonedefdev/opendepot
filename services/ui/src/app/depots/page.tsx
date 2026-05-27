import * as React from "react";
import Container from "@mui/material/Container";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import type { BrowseStorageConfig } from "@/lib/api";
import { getDepotsGraph, getResourceDetail, listResources } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import DepotsGraphClient from "@/components/DepotsGraphClient";
import RefreshIconButton from "@/components/RefreshIconButton";

export default async function DepotsPage() {
  const token = await getServerSessionToken();
  let moduleVersionsByKey: Record<string, string[]> = {};
  let providerVersionsByKey: Record<string, string[]> = {};
  let providerVersionMetaByKey: Record<string, Array<{ version: string; name?: string; fileName?: string; checksum?: string; lastScanned?: string }>> = {};
  let moduleVersionMetaByKey: Record<string, Array<{ version: string; fileName?: string; checksum?: string; lastScanned?: string }>> = {};
  let moduleDetailByKey: Record<string, { storageConfig?: BrowseStorageConfig; githubAuthenticated?: boolean }> = {};

  let graph;
  let fetchError: string | null = null;

  try {
    graph = await getDepotsGraph(undefined, token);

    try {
      const [moduleResources, providerResources] = await Promise.all([
        listResources({ kind: "module", page: 1, pageSize: 500 }, token),
        listResources({ kind: "provider", page: 1, pageSize: 500 }, token),
      ]);

      const moduleVersionEntries = await Promise.all(
        graph.modules.map(async (module) => {
          const key = `${module.namespace}/${module.name}`;
          try {
            const detail = await getResourceDetail(module.namespace, "module", module.name, token);
            const versionMeta = (detail.versions ?? [])
              .filter((version) => Boolean(version.version))
              .map((version) => ({
                version: version.version,
                fileName: version.fileName,
                checksum: version.checksum,
                lastScanned: version.lastScanned,
              }));

            const moduleDetail = {
              storageConfig: detail.storageConfig,
              githubAuthenticated: detail.githubConfig?.useAuthenticatedClient,
            };

            const dedupedVersionMeta = Array.from(
              new Map(versionMeta.map((entry) => [entry.version, entry])).values(),
            );

            const versions = dedupedVersionMeta.map((entry) => entry.version);
            if (versions.length > 0) {
              return [key, { versions, versionMeta: dedupedVersionMeta, detail: moduleDetail }] as const;
            }

            return [key, { versions: [], versionMeta: [], detail: moduleDetail }] as const;
          } catch {
            // Keep fallback behavior when detail fetch for a module fails.
          }
          const fallbackVersions = module.latestVersion ? [module.latestVersion] : [];
          return [
            key,
            {
              versions: fallbackVersions,
              versionMeta: fallbackVersions.map((version) => ({ version })),
              detail: {},
            },
          ] as const;
        }),
      );
      moduleVersionsByKey = Object.fromEntries(
        moduleVersionEntries.map(([key, value]) => [key, value.versions]),
      ) as Record<string, string[]>;

      const providerVersionEntries = await Promise.all(
        graph.providers.map(async (provider) => {
          const key = `${provider.namespace}/${provider.name}`;
          try {
            const detail = await getResourceDetail(provider.namespace, "provider", provider.name, token);
            const versionMeta = (detail.versions ?? [])
              .filter((version) => Boolean(version.version))
              .map((version) => ({
                version: version.version,
                name: version.name,
                fileName: version.fileName,
                checksum: version.checksum,
                lastScanned: version.lastScanned,
              }));

            const dedupedVersionMeta = Array.from(
              new Map(
                versionMeta.map((entry) => [
                  `${entry.version}|${entry.name ?? ""}`,
                  entry,
                ]),
              ).values(),
            );

            return [key, dedupedVersionMeta] as const;
          } catch {
            return [key, []] as const;
          }
        }),
      );
      providerVersionMetaByKey = Object.fromEntries(providerVersionEntries) as Record<string, Array<{ version: string; name?: string; fileName?: string; checksum?: string; lastScanned?: string }>>;
      providerVersionsByKey = Object.fromEntries(
        providerVersionEntries.map(([key, value]) => [key, value.map((entry) => entry.version)]),
      ) as Record<string, string[]>;

      moduleVersionMetaByKey = Object.fromEntries(
        moduleVersionEntries.map(([key, value]) => [key, value.versionMeta]),
      ) as Record<string, Array<{ version: string; fileName?: string; checksum?: string; lastScanned?: string }>>;
      moduleDetailByKey = Object.fromEntries(
        moduleVersionEntries.map(([key, value]) => [key, value.detail]),
      ) as Record<string, { storageConfig?: BrowseStorageConfig; githubAuthenticated?: boolean }>;

      const moduleSyncByKey = new Map(
        moduleResources.items.map((resource) => [
          `${resource.namespace}/${resource.name}`,
          { synced: resource.synced, syncStatus: resource.syncStatus },
        ]),
      );

      const providerSyncByKey = new Map(
        providerResources.items.map((resource) => [
          `${resource.namespace}/${resource.name}`,
          resource.synced,
        ]),
      );

      graph = {
        ...graph,
        modules: graph.modules.map((module) => {
          const key = `${module.namespace}/${module.name}`;
          const fromResource = moduleSyncByKey.get(key);
          if (!fromResource) {
            return module;
          }
          return {
            ...module,
            synced: fromResource.synced,
            syncStatus: fromResource.syncStatus,
          };
        }),
        providers: graph.providers.map((provider) => {
          const key = `${provider.namespace}/${provider.name}`;
          const fromResource = providerSyncByKey.get(key);
          if (fromResource === undefined) {
            return provider;
          }
          return {
            ...provider,
            synced: fromResource,
          };
        }),
      };
    } catch {
      // Keep graph data if sync-enrichment calls fail.
    }
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    if (msg.includes("401") || msg.includes("unauthorized")) {
      return (
        <Container maxWidth="xl" sx={{ py: 4 }}>
          <Alert severity="warning">
            You must be signed in to view the Depots graph.
          </Alert>
        </Container>
      );
    }
    fetchError = msg;
    graph = { depots: [], modules: [], providers: [], edges: [], summary: { totalDepots: 0, totalModules: 0, totalProviders: 0 }, generatedAt: "" };
  }

  return (
    <main>
      <Container maxWidth="xl" sx={{ py: 4 }}>
        <Box mb={3}>
          <Box display="flex" alignItems="center" gap={1}>
            <Typography variant="h4" component="h1">
              Depots
            </Typography>
            <RefreshIconButton ariaLabel="refresh depots" />
          </Box>
          <Typography variant="body1" color="text.secondary" mt={1} mb={2}>
            Visualise the relationships between Depots and their managed Modules and Providers.
          </Typography>
        </Box>

        {fetchError ? (
          <Alert severity="error">Failed to load depot graph: {fetchError}</Alert>
        ) : (
          <DepotsGraphClient
            graph={graph}
            moduleVersionsByKey={moduleVersionsByKey}
            providerVersionsByKey={providerVersionsByKey}
            providerVersionMetaByKey={providerVersionMetaByKey}
            moduleVersionMetaByKey={moduleVersionMetaByKey}
            moduleDetailByKey={moduleDetailByKey}
          />
        )}
      </Container>
    </main>
  );
}
