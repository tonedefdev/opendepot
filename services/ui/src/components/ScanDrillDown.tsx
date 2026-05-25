"use client";

import * as React from "react";
import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Box,
  Chip,
  Divider,
  FormControl,
  InputLabel,
  MenuItem,
  Select,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TablePagination,
  TableRow,
  TableContainer,
  TextField,
  Typography,
} from "@mui/material";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import CopyButton from "@/components/CopyButton";
import type { BrowseVersionSummary, SecurityFinding } from "@/lib/api";

interface Props {
  sourceScanFindings: SecurityFinding[];
  binaryScanFindings: Record<string, SecurityFinding[]>;
  versions: BrowseVersionSummary[];
}

function findingId(f: SecurityFinding): string {
  return f.id || f.vulnerabilityID || "—";
}

function findingSeverity(f: SecurityFinding): string {
  return (f.severity || "UNKNOWN").toUpperCase();
}

function findingTitle(f: SecurityFinding): string {
  if (f.title) {
    return f.title;
  }
  if (f.pkgName) {
    return f.pkgName;
  }
  return "Untitled finding";
}

function findingMessage(f: SecurityFinding): string {
  if (f.message) {
    return f.message;
  }
  if (f.pkgName && f.installedVersion) {
    return `${f.pkgName} (${f.installedVersion})`;
  }
  if (f.pkgName) {
    return `Package: ${f.pkgName}`;
  }
  return "—";
}

function findingResolution(f: SecurityFinding): string {
  if (f.resolution) {
    return f.resolution;
  }
  if (f.fixedVersion) {
    return `Upgrade to ${f.fixedVersion}`;
  }
  return "—";
}

const SEVERITY_ORDER: Record<string, number> = {
  CRITICAL: 5,
  HIGH: 4,
  MEDIUM: 3,
  LOW: 2,
  UNKNOWN: 1,
};

type FindingSortBy = "severity" | "id" | "title";
type FindingSortDir = "asc" | "desc";

function compareFindings(a: SecurityFinding, b: SecurityFinding, sortBy: FindingSortBy, sortDir: FindingSortDir): number {
  const direction = sortDir === "asc" ? 1 : -1;
  if (sortBy === "severity") {
    const aRank = SEVERITY_ORDER[findingSeverity(a)] ?? 0;
    const bRank = SEVERITY_ORDER[findingSeverity(b)] ?? 0;
    if (aRank !== bRank) {
      return (aRank - bRank) * direction;
    }
    return findingId(a).localeCompare(findingId(b)) * direction;
  }
  if (sortBy === "title") {
    return findingTitle(a).localeCompare(findingTitle(b)) * direction;
  }
  return findingId(a).localeCompare(findingId(b)) * direction;
}

function FindingsTable({
  findings,
  fileName,
  checksum,
  includeArtifactColumns,
  includeResolutionColumn,
}: {
  findings: SecurityFinding[];
  fileName?: string;
  checksum?: string;
  includeArtifactColumns: boolean;
  includeResolutionColumn: boolean;
}) {
  const [page, setPage] = React.useState(0);
  const [rowsPerPage, setRowsPerPage] = React.useState(10);
  const [severityFilter, setSeverityFilter] = React.useState<string>("ALL");
  const [query, setQuery] = React.useState("");
  const [sortBy, setSortBy] = React.useState<FindingSortBy>("severity");
  const [sortDir, setSortDir] = React.useState<FindingSortDir>("desc");

  const filteredAndSorted = React.useMemo(() => {
    const q = query.trim().toLowerCase();
    return findings
      .filter((f) => {
        if (severityFilter !== "ALL" && findingSeverity(f) !== severityFilter) {
          return false;
        }
        if (!q) {
          return true;
        }
        const haystack = [
          findingId(f),
          findingTitle(f),
          findingMessage(f),
          findingResolution(f),
          f.pkgName ?? "",
          f.vulnerabilityID ?? "",
        ]
          .join(" ")
          .toLowerCase();
        return haystack.includes(q);
      })
      .sort((a, b) => compareFindings(a, b, sortBy, sortDir));
  }, [findings, query, severityFilter, sortBy, sortDir]);

  const pagedRows = React.useMemo(() => {
    const start = page * rowsPerPage;
    return filteredAndSorted.slice(start, start + rowsPerPage);
  }, [filteredAndSorted, page, rowsPerPage]);

  React.useEffect(() => {
    setPage(0);
  }, [query, severityFilter, sortBy, sortDir]);

  if (findings.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary" sx={{ py: 1 }}>
        No findings.
      </Typography>
    );
  }

  return (
    <Box>
      <Box
        sx={{
          mb: 1.5,
          display: "grid",
          gap: 1,
          gridTemplateColumns: {
            xs: "1fr",
            sm: "repeat(2, minmax(0, 1fr))",
            lg: "minmax(0, 2fr) repeat(3, minmax(0, 1fr))",
          },
          alignItems: "start",
        }}
      >
        <TextField
          size="small"
          label="Filter findings"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="ID, title, package..."
          sx={{ width: "100%" }}
        />
        <FormControl size="small" sx={{ width: "100%" }}>
          <InputLabel id="severity-filter-label">Severity</InputLabel>
          <Select
            labelId="severity-filter-label"
            label="Severity"
            value={severityFilter}
            onChange={(e) => setSeverityFilter(e.target.value)}
          >
            <MenuItem value="ALL">All severities</MenuItem>
            <MenuItem value="CRITICAL">CRITICAL</MenuItem>
            <MenuItem value="HIGH">HIGH</MenuItem>
            <MenuItem value="MEDIUM">MEDIUM</MenuItem>
            <MenuItem value="LOW">LOW</MenuItem>
            <MenuItem value="UNKNOWN">UNKNOWN</MenuItem>
          </Select>
        </FormControl>
        <FormControl size="small" sx={{ width: "100%" }}>
          <InputLabel id="sort-by-label">Sort by</InputLabel>
          <Select
            labelId="sort-by-label"
            label="Sort by"
            value={sortBy}
            onChange={(e) => setSortBy(e.target.value as FindingSortBy)}
          >
            <MenuItem value="severity">Severity</MenuItem>
            <MenuItem value="id">ID</MenuItem>
            <MenuItem value="title">Title</MenuItem>
          </Select>
        </FormControl>
        <FormControl size="small" sx={{ width: "100%" }}>
          <InputLabel id="sort-dir-label">Order</InputLabel>
          <Select
            labelId="sort-dir-label"
            label="Order"
            value={sortDir}
            onChange={(e) => setSortDir(e.target.value as FindingSortDir)}
          >
            <MenuItem value="desc">Desc</MenuItem>
            <MenuItem value="asc">Asc</MenuItem>
          </Select>
        </FormControl>
      </Box>

      <TableContainer sx={{ width: "100%", overflowX: "auto", border: "1px solid rgba(240,246,252,0.08)", borderRadius: 1.5 }}>
        <Table size="small" sx={{ tableLayout: { xs: "auto", md: "fixed" } }}>
          <TableHead>
            <TableRow>
              <TableCell sx={{ width: { xs: 110, md: 130 } }}>ID</TableCell>
              <TableCell sx={{ width: { xs: 95, md: 110 } }}>Severity</TableCell>
              <TableCell>Title</TableCell>
              <TableCell>Message</TableCell>
              {includeArtifactColumns && <TableCell sx={{ width: { xs: 150, md: 180 } }}>Filename</TableCell>}
              {includeArtifactColumns && <TableCell sx={{ width: { xs: 150, md: 190 } }}>Checksum</TableCell>}
              {includeResolutionColumn && <TableCell>Resolution</TableCell>}
            </TableRow>
          </TableHead>
          <TableBody>
            {pagedRows.map((f, idx) => (
              <TableRow key={`${findingId(f)}-${idx}`} hover>
                <TableCell>
                  <Typography variant="caption" fontFamily="monospace" sx={{ wordBreak: "break-word" }}>
                    {findingId(f)}
                  </Typography>
                </TableCell>
                <TableCell>
                  <Chip
                    size="small"
                    label={findingSeverity(f)}
                    color={
                      findingSeverity(f) === "CRITICAL"
                        ? "error"
                        : findingSeverity(f) === "HIGH"
                          ? "warning"
                          : "default"
                    }
                  />
                </TableCell>
                <TableCell sx={{ whiteSpace: "normal", wordBreak: "break-word" }}>{findingTitle(f)}</TableCell>
                <TableCell sx={{ whiteSpace: "normal", wordBreak: "break-word" }}>{findingMessage(f)}</TableCell>
                {includeArtifactColumns && (
                  <TableCell sx={{ fontFamily: "monospace", fontSize: "0.75rem", whiteSpace: "normal", wordBreak: "break-word" }}>
                    {fileName || "—"}
                  </TableCell>
                )}
                {includeArtifactColumns && (
                  <TableCell>
                    {checksum ? (
                      <Box sx={{ display: "flex", alignItems: "flex-start", gap: 0.5 }}>
                        <Typography
                          variant="caption"
                          sx={{
                            fontFamily: "monospace",
                            whiteSpace: "normal",
                            wordBreak: "break-all",
                            maxWidth: { xs: 110, md: 160 },
                            display: "block",
                          }}
                        >
                          {checksum}
                        </Typography>
                        <CopyButton value={checksum} />
                      </Box>
                    ) : "—"}
                  </TableCell>
                )}
                {includeResolutionColumn && (
                  <TableCell sx={{ whiteSpace: "normal", wordBreak: "break-word" }}>{findingResolution(f)}</TableCell>
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </TableContainer>

      <TablePagination
        component="div"
        count={filteredAndSorted.length}
        page={page}
        onPageChange={(_, newPage) => setPage(newPage)}
        rowsPerPage={rowsPerPage}
        onRowsPerPageChange={(e) => {
          setRowsPerPage(parseInt(e.target.value, 10));
          setPage(0);
        }}
        rowsPerPageOptions={[5, 10, 25, 50]}
      />
    </Box>
  );
}

function platformKey(os?: string, arch?: string): string {
  if (!os || !arch) {
    return "";
  }
  return `${os}/${arch}`;
}

export default function ScanDrillDown({ sourceScanFindings, binaryScanFindings, versions }: Props) {
  const binaryKeys = Object.keys(binaryScanFindings ?? {});
  const versionsByPlatform = React.useMemo(() => {
    const map = new Map<string, BrowseVersionSummary>();
    for (const v of versions) {
      const key = platformKey(v.os, v.arch);
      if (key) {
        map.set(key, v);
      }
    }
    return map;
  }, [versions]);

  const sourceVersion = versions.find((v) => !v.os && !v.arch) ?? versions[0];

  return (
    <Box>
      <Accordion defaultExpanded={sourceScanFindings.length > 0}>
        <AccordionSummary expandIcon={<ExpandMoreIcon />}>
          <Typography fontWeight={600}>
            Source Scan Findings ({sourceScanFindings.length})
          </Typography>
        </AccordionSummary>
        <AccordionDetails>
          <FindingsTable
            findings={sourceScanFindings}
            fileName={sourceVersion?.fileName}
            checksum={sourceVersion?.checksum}
            includeArtifactColumns={false}
            includeResolutionColumn={false}
          />
        </AccordionDetails>
      </Accordion>

      {binaryKeys.length > 0 && (
        <Box mt={1}>
          <Divider sx={{ my: 1 }} />
          <Typography variant="subtitle2" gutterBottom>
            Binary Scan Findings
          </Typography>
          {binaryKeys.map((platform) => (
            <Accordion key={platform}>
              <AccordionSummary expandIcon={<ExpandMoreIcon />}>
                <Typography variant="body2">
                  {platform} ({(binaryScanFindings[platform] ?? []).length} findings)
                </Typography>
              </AccordionSummary>
              <AccordionDetails>
                <FindingsTable
                  findings={binaryScanFindings[platform] ?? []}
                  fileName={versionsByPlatform.get(platform)?.fileName}
                  checksum={versionsByPlatform.get(platform)?.checksum}
                  includeArtifactColumns={true}
                  includeResolutionColumn={true}
                />
              </AccordionDetails>
            </Accordion>
          ))}
        </Box>
      )}
    </Box>
  );
}
