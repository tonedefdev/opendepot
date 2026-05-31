import * as React from "react";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Grid from "@mui/material/Grid";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import ResourceCard from "@/components/ResourceCard";
import ResourceListControls from "@/components/ResourceListControls";
import RefreshIconButton from "@/components/RefreshIconButton";
import { listResources, listNamespaces } from "@/lib/api";
import type { ListResourcesParams } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { redirect } from "next/navigation";

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
    pageSize: parseInt(sp(params["page_size"]) ?? "24", 10),
  };

  let resourceList;
  let namespaceList;
  let fetchError: string | null = null;

  try {
    [resourceList, namespaceList] = await Promise.all([
      listResources(listParams, token),
      listNamespaces(token),
    ]);
  } catch (err) {
    const msg = err instanceof Error ? err.message : "Failed to load resources.";
    if (msg.includes("401") || msg.includes("unauthorized")) {
      redirect("/auth/login");
    }
    fetchError = msg;
    resourceList = { items: [], totalCount: 0, page: 1, pageSize: 24 };
    namespaceList = { items: [] };
  }

  // Build a clean search-params string for pagination controls (exclude page/page_size).
  const cleanParams = new URLSearchParams();
  if (listParams.namespace) cleanParams.set("namespace", listParams.namespace);
  if (listParams.kind) cleanParams.set("kind", listParams.kind);
  if (listParams.q) cleanParams.set("q", listParams.q);
  if (listParams.sortBy) cleanParams.set("sort_by", listParams.sortBy);
  if (listParams.sortDir) cleanParams.set("sort_dir", listParams.sortDir);
  const baseParams = cleanParams.toString();

  return (
    <main>
    <Container maxWidth="xl" sx={{ py: 4 }}>
      <Box mb={4}>
        <Box display="flex" alignItems="center" gap={1}>
          <Typography variant="h4" component="h1">
            Registry Explorer
          </Typography>
          <RefreshIconButton ariaLabel="refresh registry" />
        </Box>
        <Typography variant="body1" color="text.secondary" mt={1}>
          Browse modules and providers from your OpenDepot registry.
        </Typography>
      </Box>

      {authError && (
        <Alert severity="warning" sx={{ mb: 3 }}>
          Authentication error: {decodeURIComponent(authError)}
        </Alert>
      )}

      {fetchError && (
        <Alert severity="error" data-testid="empty-state" sx={{ mb: 3 }}>
          {fetchError}
        </Alert>
      )}

      {resourceList.items.length === 0 && !fetchError ? (
        <Alert severity="info" data-testid="empty-state">
          No resources found. Label namespaces and resources with{" "}
          <code>opendepot.defdev.io/public=true</code> to make them visible, or
          sign in to see resources allowed by your GroupBinding.
        </Alert>
      ) : (
        <>
          <Grid container spacing={2}>
            {resourceList.items.map((r) => (
              <Grid
                key={`${r.namespace}/${r.kind}/${r.name}`}
                size={{ xs: 12, sm: 6, md: 4, lg: 3 }}
              >
                <ResourceCard resource={r} />
              </Grid>
            ))}
          </Grid>

          <ResourceListControls
            totalCount={resourceList.totalCount}
            page={listParams.page ?? 1}
            pageSize={listParams.pageSize ?? 24}
            baseParams={baseParams}
          />
        </>
      )}
    </Container>
    </main>
  );
}
