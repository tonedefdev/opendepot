import * as React from "react";
import Container from "@mui/material/Container";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import ResourceList from "@/components/ResourceList";
import ResourceListControls from "@/components/ResourceListControls";
import RegistryPreferencesSync from "@/components/RegistryPreferencesSync";
import RefreshIconButton from "@/components/RefreshIconButton";
import { DEFAULT_REGISTRY_PAGE_SIZE } from "@/lib/registryPreferences";
import { listResources, listNamespaces } from "@/lib/api";
import type { ListResourcesParams } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { redirect } from "next/navigation";
import GridViewIcon from "@mui/icons-material/GridView";
import PageHeader from "@/components/PageHeader";
import { DepotKindIcon, type DepotKind } from "@/components/DepotKindIcon";

// A kind filter is its own destination in the sidebar, so the page is titled and iconed for it.
const KIND_HEADERS: Record<string, { title: string; kind: DepotKind }> = {
  agent: { title: "Agents", kind: "agent" },
  skill: { title: "Skills", kind: "skill" },
  module: { title: "Modules", kind: "module" },
  provider: { title: "Providers", kind: "provider" },
};

interface PageProps {
  searchParams?: Promise<Record<string, string | string[] | undefined>>;
}

function sp(v: string | string[] | undefined): string | undefined {
  return Array.isArray(v) ? v[0] : v;
}

export default async function HomePage({ searchParams }: PageProps) {
  const params = await (searchParams ?? Promise.resolve({} as Record<string, string | string[] | undefined>));
  const token = await getServerSessionToken();
  const authError = sp(params["auth_error"]);

  const listParams: ListResourcesParams = {
    namespace: sp(params["namespace"]),
    kind: sp(params["kind"]),
    q: sp(params["q"]),
    sortBy: sp(params["sort_by"]) ?? "name",
    sortDir: (sp(params["sort_dir"]) as "asc" | "desc") ?? "asc",
    page: parseInt(sp(params["page"]) ?? "1", 10),
    pageSize: parseInt(sp(params["page_size"]) ?? String(DEFAULT_REGISTRY_PAGE_SIZE), 10),
  };

  let resourceList;
  let fetchError: string | null = null;

  try {
    [resourceList] = await Promise.all([
      listResources(listParams, token),
      listNamespaces(token),
    ]);
  } catch (err) {
    const msg = err instanceof Error ? err.message : "Failed to load resources.";
    if (msg.includes("401") || msg.includes("unauthorized")) {
      redirect("/login");
    }
    fetchError = msg;
    resourceList = { items: [], totalCount: 0, page: 1, pageSize: DEFAULT_REGISTRY_PAGE_SIZE };
  }

  // Build a clean search-params string for pagination controls (exclude page/page_size).
  const cleanParams = new URLSearchParams();
  if (listParams.namespace) cleanParams.set("namespace", listParams.namespace);
  if (listParams.kind) cleanParams.set("kind", listParams.kind);
  if (listParams.q) cleanParams.set("q", listParams.q);
  if (listParams.sortBy) cleanParams.set("sort_by", listParams.sortBy);
  if (listParams.sortDir) cleanParams.set("sort_dir", listParams.sortDir);
  const baseParams = cleanParams.toString();

  const kindHeader = KIND_HEADERS[listParams.kind ?? ""];

  return (
    <main>
      <PageHeader
        icon={kindHeader ? <DepotKindIcon kind={kindHeader.kind} fontSize="small" /> : <GridViewIcon fontSize="small" />}
        title={kindHeader?.title ?? "Registry Explorer"}
        description={kindHeader ? undefined : "Browse agents, skills, modules, and providers from your OpenDepot registry."}
        actions={<RefreshIconButton ariaLabel="refresh registry" />}
      />
      <Container maxWidth="xl" sx={{ py: 3 }}>
        <RegistryPreferencesSync />
        {authError && (
          <Alert severity="warning" sx={{ mb: 3 }}>
            Authentication error: {decodeURIComponent(authError)}
          </Alert>
        )}

        {fetchError && (
          <Alert severity="error" data-testid="empty-state" sx={{ mb: 3 }}>
            <Typography variant="body2" fontWeight={600}>
              The registry could not be loaded.
            </Typography>
            <Typography variant="body2">
              Check that the OpenDepot server is running and reachable, then
              refresh the page.
            </Typography>
            <Typography
              variant="caption"
              color="text.secondary"
              component="div"
              mt={0.5}
            >
              {fetchError}
            </Typography>
          </Alert>
        )}

        {resourceList.items.length === 0 && !fetchError ? (
          <Alert severity="info" data-testid="empty-state">
            No resources found. Label namespaces and resources with{" "}
            <code>opendepot.defdev.io/public=true</code> to make them visible,
            or sign in to see resources allowed by your GroupBinding.
          </Alert>
        ) : (
          <>
            <ResourceList
              items={resourceList.items}
              totalCount={resourceList.totalCount}
              sortBy={listParams.sortBy ?? "name"}
              sortDir={listParams.sortDir === "desc" ? "desc" : "asc"}
              pageSize={listParams.pageSize ?? DEFAULT_REGISTRY_PAGE_SIZE}
              baseParams={baseParams}
              namespace={listParams.namespace}
              query={listParams.q}
            />

            <ResourceListControls
              totalCount={resourceList.totalCount}
              page={listParams.page ?? 1}
              pageSize={listParams.pageSize ?? DEFAULT_REGISTRY_PAGE_SIZE}
              baseParams={baseParams}
            />
          </>
        )}
      </Container>
    </main>
  );
}
