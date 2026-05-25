import * as React from "react";
import Container from "@mui/material/Container";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import Chip from "@mui/material/Chip";
import { getDepotsGraph } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import DepotGraph from "@/components/DepotGraph";

export default async function DepotsPage() {
  const token = await getServerSessionToken();

  let graph;
  let fetchError: string | null = null;

  try {
    graph = await getDepotsGraph(undefined, token);
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
          <Typography variant="h4" component="h1" gutterBottom>
            Depots
          </Typography>
          <Typography variant="body1" color="text.secondary" mb={2}>
            Visualise the relationships between Depots and their managed Modules and Providers.
          </Typography>
          {!fetchError && (
            <Box sx={{ display: "flex", gap: 1 }}>
              <Chip label={`${graph.summary.totalDepots} depots`} size="small" color="primary" variant="outlined" />
              <Chip label={`${graph.summary.totalModules} modules`} size="small" sx={{ color: "secondary.main", borderColor: "secondary.main" }} variant="outlined" />
              <Chip label={`${graph.summary.totalProviders} providers`} size="small" sx={{ color: "secondary.light", borderColor: "secondary.light" }} variant="outlined" />
            </Box>
          )}
        </Box>

        {fetchError ? (
          <Alert severity="error">Failed to load depot graph: {fetchError}</Alert>
        ) : (
          <DepotGraph graph={graph} />
        )}
      </Container>
    </main>
  );
}
