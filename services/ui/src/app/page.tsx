import * as React from "react";
import Container from "@mui/material/Container";
import Paper from "@mui/material/Paper";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableContainer from "@mui/material/TableContainer";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import ResourceRow from "@/components/ResourceRow";
import ResourceListControls from "@/components/ResourceListControls";
import RefreshIconButton from "@/components/RefreshIconButton";
import { listResources, listNamespaces } from "@/lib/api";
import type { ListResourcesParams } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { redirect } from "next/navigation";
import GridViewIcon from "@mui/icons-material/GridView";
import PageHeader from "@/components/PageHeader";

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
    resourceList = { items: [], totalCount: 0, page: 1, pageSize: 24 };
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
      <PageHeader
        icon={<GridViewIcon color="primary" fontSize="small" />}
        title="Registry Explorer"
        description="Browse agents, skills, modules, and providers from your OpenDepot registry."
        actions={<RefreshIconButton ariaLabel="refresh registry" />}
      />
      <Container maxWidth="xl" sx={{ py: 3 }}>
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
            <TableContainer component={Paper} variant="outlined">
              <Table aria-label="Registry resources">
                <TableHead>
                  <TableRow>
                    <TableCell>Name</TableCell>
                    <TableCell>Kind</TableCell>
                    <TableCell sx={{ display: { xs: "none", md: "table-cell" } }}>Version</TableCell>
                    <TableCell>Status</TableCell>
                    <TableCell>Risk and scans</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {resourceList.items.map((r) => (
                    <ResourceRow
                      key={`${r.namespace}/${r.kind}/${r.name}`}
                      resource={r}
                    />
                  ))}
                </TableBody>
              </Table>
            </TableContainer>

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
