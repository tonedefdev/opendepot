"use client";

import * as React from "react";
import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Box,
  Button,
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
  Tooltip,
  Typography,
} from "@mui/material";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import FilterListIcon from "@mui/icons-material/FilterList";
import ClearIcon from "@mui/icons-material/Clear";
import IconButton from "@mui/material/IconButton";
import RefreshIcon from "@mui/icons-material/Refresh";
import { keyframes, styled } from "@mui/system";
import CopyButton from "@/components/CopyButton";
import type { BrowseScanFindings, BrowseVersionSummary, SecurityFinding } from "@/lib/api";

const spin = keyframes`
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
`;

const SpinIcon = styled("span", {
  shouldForwardProp: (prop) => prop !== "spinning",
})<{ spinning?: boolean }>(({ spinning }) => ({
  display: "flex",
  alignItems: "center",
  animation: spinning ? `${spin} 0.7s linear infinite` : "none",
}));

interface Props {
  namespace: string;
  kind: string;
  name: string;
  initialSourceScanFindings: SecurityFinding[];
  initialBinaryScanFindings: Record<string, SecurityFinding[]>;
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
  scannableVersions,
  selectedSourceVersion,
  onVersionChange,
}: {
  findings: SecurityFinding[];
  fileName?: string;
  checksum?: string;
  includeArtifactColumns: boolean;
  includeResolutionColumn: boolean;
  scannableVersions?: string[];
  selectedSourceVersion?: string;
  onVersionChange?: (v: string) => void;
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

  const hasActiveFilter = query !== "" || severityFilter !== "ALL" || sortBy !== "severity" || sortDir !== "desc";

  function clearFilters() {
    setQuery("");
    setSeverityFilter("ALL");
    setSortBy("severity");
    setSortDir("desc");
  }

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
        display="flex"
        flexWrap="wrap"
        gap={1.5}
        mb={2}
        alignItems="center"
        sx={{ p: 1.5, borderRadius: 1.5, background: "rgba(240,246,252,0.04)", border: "1px solid rgba(240,246,252,0.06)" }}
      >
        <FilterListIcon sx={{ fontSize: 18, color: "text.secondary" }} />

        {scannableVersions && scannableVersions.length > 1 && onVersionChange && (
          <FormControl size="small" sx={{ minWidth: 140 }} onClick={(e) => e.stopPropagation()}>
            <InputLabel sx={{ fontSize: "0.8125rem" }}>Version</InputLabel>
            <Select
              label="Version"
              value={selectedSourceVersion || scannableVersions[0] || ""}
              onChange={(e) => onVersionChange(e.target.value)}
              sx={{ fontSize: "0.8125rem" }}
            >
              {scannableVersions.map((v) => (
                <MenuItem key={v} value={v} sx={{ fontSize: "0.8125rem" }}>{v}</MenuItem>
              ))}
            </Select>
          </FormControl>
        )}

        <TextField
          size="small"
          placeholder="Search findings…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          sx={{ minWidth: 180, "& .MuiInputBase-input": { fontSize: "0.8125rem" } }}
        />

        <FormControl size="small" sx={{ minWidth: 140 }}>
          <InputLabel sx={{ fontSize: "0.8125rem" }}>Severity</InputLabel>
          <Select
            label="Severity"
            value={severityFilter}
            onChange={(e) => setSeverityFilter(e.target.value)}
            sx={{ fontSize: "0.8125rem" }}
          >
            <MenuItem value="ALL">All severities</MenuItem>
            <MenuItem value="CRITICAL">CRITICAL</MenuItem>
            <MenuItem value="HIGH">HIGH</MenuItem>
            <MenuItem value="MEDIUM">MEDIUM</MenuItem>
            <MenuItem value="LOW">LOW</MenuItem>
            <MenuItem value="UNKNOWN">UNKNOWN</MenuItem>
          </Select>
        </FormControl>

        <FormControl size="small" sx={{ minWidth: 120 }}>
          <InputLabel sx={{ fontSize: "0.8125rem" }}>Sort by</InputLabel>
          <Select
            label="Sort by"
            value={sortBy}
            onChange={(e) => setSortBy(e.target.value as FindingSortBy)}
            sx={{ fontSize: "0.8125rem" }}
          >
            <MenuItem value="severity">Severity</MenuItem>
            <MenuItem value="id">ID</MenuItem>
            <MenuItem value="title">Title</MenuItem>
          </Select>
        </FormControl>

        <FormControl size="small" sx={{ minWidth: 100 }}>
          <InputLabel sx={{ fontSize: "0.8125rem" }}>Order</InputLabel>
          <Select
            label="Order"
            value={sortDir}
            onChange={(e) => setSortDir(e.target.value as FindingSortDir)}
            sx={{ fontSize: "0.8125rem" }}
          >
            <MenuItem value="desc">Desc</MenuItem>
            <MenuItem value="asc">Asc</MenuItem>
          </Select>
        </FormControl>

        {hasActiveFilter && (
          <Tooltip title="Clear all filters">
            <Button
              size="small"
              startIcon={<ClearIcon fontSize="small" />}
              onClick={clearFilters}
              sx={{ fontSize: "0.8rem", textTransform: "none" }}
            >
              Clear
            </Button>
          </Tooltip>
        )}
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
                          : findingSeverity(f) === "MEDIUM"
                            ? "info"
                            : findingSeverity(f) === "LOW"
                              ? "success"
                              : "default"
                    }
                    sx={{ color: "#fff" }}
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

export default function ScanDrillDown({ namespace, kind, name, initialSourceScanFindings, initialBinaryScanFindings, versions }: Props) {
  const [sourceScanFindings, setSourceScanFindings] = React.useState<SecurityFinding[]>(initialSourceScanFindings);
  const [binaryScanFindings, setBinaryScanFindings] = React.useState<Record<string, SecurityFinding[]>>(initialBinaryScanFindings);
  const [isRefreshing, setIsRefreshing] = React.useState(false);
  const [refreshNonce, setRefreshNonce] = React.useState(0);
  const [scannableVersions, setScannableVersions] = React.useState<string[]>([]);
  const [selectedSourceVersion, setSelectedSourceVersion] = React.useState<string>("");

  const handleRefresh = React.useCallback(() => {
    setIsRefreshing(true);
    setRefreshNonce((n) => n + 1);
    setTimeout(() => setIsRefreshing(false), 600);
  }, []);

  React.useEffect(() => {
    let url = `/api/scan-findings/${encodeURIComponent(namespace)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`;
    if (selectedSourceVersion) {
      url += `?version=${encodeURIComponent(selectedSourceVersion)}`;
    }
    fetch(url)
      .then((r) => {
        if (!r.ok) throw new Error(`${r.status}`);
        return r.json();
      })
      .then((d: BrowseScanFindings) => {
        setSourceScanFindings(d.sourceScanFindings ?? []);
        setBinaryScanFindings(d.binaryScanFindings ?? {});
        if (d.scannedVersions?.length) {
          setScannableVersions(d.scannedVersions);
        }
      })
      .catch(() => { /* keep existing data on error; user can retry */ });
  }, [namespace, kind, name, refreshNonce, selectedSourceVersion]);

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

  const totalFindings = sourceScanFindings.length + Object.values(binaryScanFindings ?? {}).reduce((acc, arr) => acc + arr.length, 0);

  return (
    <Box>
      <Box display="flex" alignItems="center" gap={1} mb={2}>
        <Typography variant="h6" sx={{ fontWeight: 600 }}>
          Scan Findings
        </Typography>
        <Chip label={totalFindings} size="small" sx={{ fontSize: "0.72rem" }} />
        <Tooltip title="Refresh">
          <IconButton size="small" onClick={handleRefresh} aria-label="refresh scan findings">
            <SpinIcon spinning={isRefreshing}>
              <RefreshIcon fontSize="small" />
            </SpinIcon>
          </IconButton>
        </Tooltip>
      </Box>
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
            scannableVersions={scannableVersions}
            selectedSourceVersion={selectedSourceVersion}
            onVersionChange={setSelectedSourceVersion}
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
