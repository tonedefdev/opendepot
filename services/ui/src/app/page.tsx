import * as React from "react";
import Box from "@mui/material/Box";
import Container from "@mui/material/Container";
import Grid from "@mui/material/Grid";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import ResourceCard from "@/components/ResourceCard";
import { listResources, listNamespaces } from "@/lib/api";
import type { ListResourcesParams } from "@/lib/api";

interface PageProps {
  searchParams?: Promise<Record<string, string | string[] | undefined>>;
}

function sp(v: string | string[] | undefined): string | undefined {
  return Array.isArray(v) ? v[0] : v;
}

export default async function HomePage({ searchParams }: PageProps) {
  const params = await (searchParams ?? Promise.resolve({} as Record<string, string | string[] | undefined>));

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
      listResources(listParams),
      listNamespaces(),
    ]);
  } catch (err) {
    fetchError =
      err instanceof Error ? err.message : "Failed to load resources.";
    resourceList = { items: [], totalCount: 0, page: 1, pageSize: 24 };
    namespaceList = { items: [] };
  }

  return (
    <main>
    <Container maxWidth="xl" sx={{ py: 4 }}>
      <Box mb={4}>
        <Typography variant="h4" component="h1" gutterBottom>
          Registry Explorer
        </Typography>
        <Typography variant="body1" color="text.secondary">
          Browse Terraform modules and providers from your OpenDepot registry.
        </Typography>
      </Box>

      {fetchError && (
        <Alert severity="error" data-testid="empty-state" sx={{ mb: 3 }}>
          {fetchError}
        </Alert>
      )}

      <Box mb={2}>
        <Typography variant="body2" color="text.secondary">
          {resourceList.totalCount} resource{resourceList.totalCount !== 1 ? "s" : ""}
          {listParams.namespace ? ` in ${listParams.namespace}` : ""}
        </Typography>
      </Box>

      {resourceList.items.length === 0 && !fetchError ? (
        <Alert severity="info" data-testid="empty-state">
          No resources found. Label namespaces and resources with{" "}
          <code>opendepot.defdev.io/public=true</code> to make them visible, or
          sign in to see resources allowed by your GroupBinding.
        </Alert>
      ) : (
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
      )}
    </Container>
    </main>
  );
}
