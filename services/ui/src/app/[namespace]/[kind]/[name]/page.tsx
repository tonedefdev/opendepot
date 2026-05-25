import * as React from "react";
import Container from "@mui/material/Container";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import Chip from "@mui/material/Chip";
import Divider from "@mui/material/Divider";
import Alert from "@mui/material/Alert";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import ErrorIcon from "@mui/icons-material/Error";
import SeverityBadge from "@/components/SeverityBadge";
import ScanDrillDown from "@/components/ScanDrillDown";
import { getResourceDetail } from "@/lib/api";
import { notFound } from "next/navigation";

interface PageProps {
  params: Promise<{
    namespace: string;
    kind: string;
    name: string;
  }>;
}

export default async function ResourceDetailPage({ params }: PageProps) {
  const { namespace, kind, name } = await params;

  let detail;
  try {
    detail = await getResourceDetail(namespace, kind, name);
  } catch (err) {
    const msg = err instanceof Error ? err.message : "";
    if (msg.includes("404")) {
      notFound();
    }
    return (
      <Container maxWidth="lg" sx={{ py: 4 }}>
        <Alert severity="error">
          Failed to load resource detail: {msg}
        </Alert>
      </Container>
    );
  }

  return (
    <Container maxWidth="lg" sx={{ py: 4 }}>
      <Box mb={1}>
        <Typography variant="caption" color="text.secondary">
          {detail.namespace}
        </Typography>
      </Box>

      <Typography variant="h4" component="h1" gutterBottom>
        {detail.name}
      </Typography>

      <Box display="flex" flexWrap="wrap" gap={1} mb={2}>
        <Chip label={detail.kind} color="primary" variant="outlined" />
        {detail.provider && <Chip label={detail.provider} variant="outlined" />}
        {detail.latestVersion && <Chip label={`v${detail.latestVersion}`} />}
        <Box display="flex" alignItems="center" gap={0.5}>
          {detail.synced ? (
            <CheckCircleIcon sx={{ fontSize: 16, color: "success.main" }} />
          ) : (
            <ErrorIcon sx={{ fontSize: 16, color: "warning.main" }} />
          )}
          <Typography variant="caption">
            {detail.syncStatus || (detail.synced ? "Synced" : "Not synced")}
          </Typography>
        </Box>
      </Box>

      {detail.scanCounts && (
        <Box display="flex" flexWrap="wrap" gap={0.5} mb={2}>
          <SeverityBadge counts={detail.scanCounts} />
        </Box>
      )}

      <Divider sx={{ my: 3 }} />

      {detail.versions && detail.versions.length > 0 && (
        <Box mb={4}>
          <Typography variant="h6" gutterBottom>
            Versions
          </Typography>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Version</TableCell>
                <TableCell>Sync Status</TableCell>
                <TableCell>OS</TableCell>
                <TableCell>Arch</TableCell>
                <TableCell>Last Scanned</TableCell>
                <TableCell>Findings</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {detail.versions.map((v) => (
                <TableRow key={`${v.version}-${v.os}-${v.arch}`} hover>
                  <TableCell>{v.version}</TableCell>
                  <TableCell>{v.syncStatus}</TableCell>
                  <TableCell>{v.os}</TableCell>
                  <TableCell>{v.arch}</TableCell>
                  <TableCell>{v.lastScanned || "—"}</TableCell>
                  <TableCell>
                    <Box display="flex" gap={0.5}>
                      <SeverityBadge counts={v.scanCounts} />
                    </Box>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Box>
      )}

      <Divider sx={{ my: 3 }} />

      <Typography variant="h6" gutterBottom>
        Scan Findings
      </Typography>
      <ScanDrillDown
        sourceScanFindings={detail.sourceScanFindings ?? []}
        binaryScanFindings={detail.binaryScanFindings ?? {}}
      />
    </Container>
  );
}
