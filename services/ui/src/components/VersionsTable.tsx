"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableHead from "@mui/material/TableHead";
import TablePagination from "@mui/material/TablePagination";
import TableRow from "@mui/material/TableRow";
import TextField from "@mui/material/TextField";
import Select from "@mui/material/Select";
import MenuItem from "@mui/material/MenuItem";
import FormControl from "@mui/material/FormControl";
import InputLabel from "@mui/material/InputLabel";
import Typography from "@mui/material/Typography";
import Skeleton from "@mui/material/Skeleton";
import Button from "@mui/material/Button";
import IconButton from "@mui/material/IconButton";
import Chip from "@mui/material/Chip";
import Tooltip from "@mui/material/Tooltip";
import CheckCircleIcon from "@mui/icons-material/CheckCircle";
import SyncProblemIcon from "@mui/icons-material/SyncProblem";
import ErrorIcon from "@mui/icons-material/Error";
import FilterListIcon from "@mui/icons-material/FilterList";
import ClearIcon from "@mui/icons-material/Clear";
import RefreshIcon from "@mui/icons-material/Refresh";
import { keyframes, styled } from "@mui/system";
import SeverityBadge from "@/components/SeverityBadge";

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
import CopyButton from "@/components/CopyButton";
import type { BrowseVersionSummary } from "@/lib/api";

interface BrowseVersionList {
  items: BrowseVersionSummary[];
  totalCount: number;
  page: number;
  pageSize: number;
  availableOS?: string[];
  availableArch?: string[];
}

interface VersionsTableProps {
  namespace: string;
  kind: string;
  name: string;
}

function displayVersion(v: string): string {
  return v.startsWith("v") ? v : `v${v}`;
}

const SKELETON_ROWS = 5;
const PAGE_SIZE_OPTIONS = [10, 20, 50, 100] as const;

export default function VersionsTable({ namespace, kind, name }: VersionsTableProps) {
  const isProvider = kind === "provider";

  const [page, setPage] = React.useState(1);
  const [pageSize, setPageSize] = React.useState(20);
  const [q, setQ] = React.useState("");
  const [debouncedQ, setDebouncedQ] = React.useState("");
  const [synced, setSynced] = React.useState("");
  const [os, setOs] = React.useState("");
  const [arch, setArch] = React.useState("");
  const [data, setData] = React.useState<BrowseVersionList | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [refreshNonce, setRefreshNonce] = React.useState(0);
  const [isRefreshing, setIsRefreshing] = React.useState(false);
  // Track whether we've ever received data so we can skip the skeleton flash on re-fetches.
  const hasData = React.useRef(false);

  const handleRefresh = React.useCallback(() => {
    setIsRefreshing(true);
    setRefreshNonce((n) => n + 1);
    setTimeout(() => setIsRefreshing(false), 600);
  }, []);

  // Debounce the q input so we don't fire a request on every keystroke.
  React.useEffect(() => {
    const id = setTimeout(() => setDebouncedQ(q), 300);
    return () => clearTimeout(id);
  }, [q]);

  // Reset to page 1 whenever any filter changes.
  React.useEffect(() => {
    setPage(1);
  }, [debouncedQ, synced, os, arch, pageSize]);

  // Fetch from the Next.js route handler proxy.
  React.useEffect(() => {
    let cancelled = false;
    // Only show the skeleton state on the very first load. Once we have data,
    // keep the current rows visible while the re-fetch runs (avoids the flash/jank).
    if (!hasData.current) setLoading(true);
    setError(null);

    const params = new URLSearchParams();
    params.set("page", String(page));
    params.set("page_size", String(pageSize));
    if (debouncedQ) params.set("q", debouncedQ);
    if (synced) params.set("synced", synced);
    if (os) params.set("os", os);
    if (arch) params.set("arch", arch);

    fetch(`/api/versions/${encodeURIComponent(namespace)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}?${params.toString()}`)
      .then((res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
        return res.json() as Promise<BrowseVersionList>;
      })
      .then((json) => {
        if (!cancelled) {
          hasData.current = true;
          setData(json);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "Failed to load versions");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => { cancelled = true; };
  }, [namespace, kind, name, page, pageSize, debouncedQ, synced, os, arch, refreshNonce]);

  const hasActiveFilter = debouncedQ !== "" || synced !== "" || os !== "" || arch !== "";

  const clearFilters = () => {
    setQ("");
    setSynced("");
    setOs("");
    setArch("");
  };

  const availableOS = data?.availableOS ?? [];
  const availableArch = data?.availableArch ?? [];
  const totalCount = data?.totalCount ?? 0;
  const items = data?.items ?? [];

  // Table column count for skeleton/empty colspans.
  const colCount = isProvider ? 8 : 6;

  return (
    <Box mb={4}>
      <Box display="flex" alignItems="center" gap={1} mb={2}>
        <Typography variant="h6" sx={{ fontWeight: 600 }}>
          Versions
        </Typography>
        {!loading && (
          <Chip label={totalCount} size="small" sx={{ fontSize: "0.72rem" }} />
        )}
        <Tooltip title="Refresh">
          <IconButton
            size="small"
            onClick={handleRefresh}
            aria-label="refresh versions"
          >
            <SpinIcon spinning={isRefreshing}>
              <RefreshIcon fontSize="small" />
            </SpinIcon>
          </IconButton>
        </Tooltip>
      </Box>

      {/* Filter bar */}
      <Box
        display="flex"
        flexWrap="wrap"
        gap={1.5}
        mb={2}
        alignItems="center"
        sx={{ p: 1.5, borderRadius: 1.5, background: "rgba(240,246,252,0.04)", border: "1px solid rgba(240,246,252,0.06)" }}
      >
        <FilterListIcon sx={{ fontSize: 18, color: "text.secondary" }} />

        <TextField
          size="small"
          placeholder="Search versions…"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          sx={{ minWidth: 180, "& .MuiInputBase-input": { fontSize: "0.8125rem" } }}
        />

        <FormControl size="small" sx={{ minWidth: 130 }}>
          <InputLabel sx={{ fontSize: "0.8125rem" }}>Sync status</InputLabel>
          <Select
            value={synced}
            label="Sync status"
            onChange={(e) => setSynced(e.target.value)}
            sx={{ fontSize: "0.8125rem" }}
          >
            <MenuItem value="">All</MenuItem>
            <MenuItem value="true">Synced</MenuItem>
            <MenuItem value="false">Failed</MenuItem>
          </Select>
        </FormControl>

        {isProvider && availableOS.length > 0 && (
          <FormControl size="small" sx={{ minWidth: 110 }}>
            <InputLabel sx={{ fontSize: "0.8125rem" }}>OS</InputLabel>
            <Select
              value={os}
              label="OS"
              onChange={(e) => setOs(e.target.value)}
              sx={{ fontSize: "0.8125rem" }}
            >
              <MenuItem value="">All</MenuItem>
              {availableOS.map((o) => (
                <MenuItem key={o} value={o} sx={{ fontFamily: "monospace", fontSize: "0.8125rem" }}>
                  {o}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        )}

        {isProvider && availableArch.length > 0 && (
          <FormControl size="small" sx={{ minWidth: 110 }}>
            <InputLabel sx={{ fontSize: "0.8125rem" }}>Arch</InputLabel>
            <Select
              value={arch}
              label="Arch"
              onChange={(e) => setArch(e.target.value)}
              sx={{ fontSize: "0.8125rem" }}
            >
              <MenuItem value="">All</MenuItem>
              {availableArch.map((a) => (
                <MenuItem key={a} value={a} sx={{ fontFamily: "monospace", fontSize: "0.8125rem" }}>
                  {a}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        )}

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

      {error && (
        <Typography variant="body2" color="error" sx={{ mb: 2 }}>
          {error}
        </Typography>
      )}

      <Box sx={{ overflowX: "auto", borderRadius: 2, border: "1px solid rgba(240,246,252,0.08)" }}>
        <Table size="small" sx={{ minWidth: 860 }}>
          <TableHead>
            <TableRow>
              <TableCell sx={{ whiteSpace: "nowrap" }}>Version</TableCell>
              <TableCell sx={{ whiteSpace: "nowrap" }}>Sync Status</TableCell>
              {isProvider && <TableCell sx={{ whiteSpace: "nowrap" }}>OS</TableCell>}
              {isProvider && <TableCell sx={{ whiteSpace: "nowrap" }}>Arch</TableCell>}
              <TableCell sx={{ whiteSpace: "nowrap" }}>File Name</TableCell>
              <TableCell sx={{ whiteSpace: "nowrap" }}>Checksum</TableCell>
              <TableCell sx={{ whiteSpace: "nowrap" }}>Last Scanned</TableCell>
              <TableCell sx={{ whiteSpace: "nowrap" }}>Findings</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {loading ? (
              Array.from({ length: SKELETON_ROWS }).map((_, i) => (
                <TableRow key={i}>
                  {Array.from({ length: colCount }).map((_, j) => (
                    <TableCell key={j}>
                      <Skeleton variant="text" width="80%" />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : items.length === 0 ? (
              <TableRow>
                <TableCell colSpan={colCount} align="center" sx={{ py: 4, color: "text.secondary" }}>
                  No versions match the current filters.
                </TableCell>
              </TableRow>
            ) : (
              items.map((v, idx) => (
                <TableRow key={`${v.version}-${v.os ?? ""}-${v.arch ?? ""}-${idx}`} hover>
                  <TableCell sx={{ fontFamily: "monospace", fontSize: "0.8125rem", verticalAlign: "top" }}>
                    {displayVersion(v.version)}
                  </TableCell>
                  <TableCell sx={{ verticalAlign: "top" }}>
                    <Box display="flex" alignItems="center" gap={0.5}>
                      {/failed|error/i.test(v.syncStatus) ? (
                        <ErrorIcon sx={{ fontSize: 12, color: "error.main" }} />
                      ) : v.synced ? (
                        <CheckCircleIcon sx={{ fontSize: 12, color: "success.main" }} />
                      ) : (
                        <SyncProblemIcon sx={{ fontSize: 12, color: "warning.main" }} />
                      )}
                      <Typography variant="caption">{v.syncStatus}</Typography>
                    </Box>
                  </TableCell>
                  {isProvider && (
                    <TableCell sx={{ fontFamily: "monospace", fontSize: "0.8125rem", verticalAlign: "top" }}>
                      {v.os || "—"}
                    </TableCell>
                  )}
                  {isProvider && (
                    <TableCell sx={{ fontFamily: "monospace", fontSize: "0.8125rem", verticalAlign: "top" }}>
                      {v.arch || "—"}
                    </TableCell>
                  )}
                  <TableCell
                    sx={{ fontFamily: "monospace", fontSize: "0.8125rem", maxWidth: 240, whiteSpace: "normal", wordBreak: "break-word", verticalAlign: "top" }}
                  >
                    {v.fileName || "—"}
                  </TableCell>
                  <TableCell sx={{ fontFamily: "monospace", fontSize: "0.75rem", maxWidth: 220, verticalAlign: "top" }}>
                    {v.checksum ? (
                      <Box sx={{ display: "flex", alignItems: "flex-start", gap: 0.5 }}>
                        <Typography
                          variant="caption"
                          sx={{ fontFamily: "monospace", whiteSpace: "normal", wordBreak: "break-all", maxWidth: { xs: 120, md: 180 }, display: "block" }}
                        >
                          {v.checksum}
                        </Typography>
                        <CopyButton value={v.checksum} />
                      </Box>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                  <TableCell sx={{ fontSize: "0.8125rem", verticalAlign: "top", whiteSpace: "nowrap" }}>
                    {v.lastScanned || "—"}
                  </TableCell>
                  <TableCell sx={{ verticalAlign: "top" }}>
                    <Box display="flex" gap={0.5} flexWrap="wrap" sx={{ maxWidth: 160 }}>
                      <SeverityBadge counts={v.scanCounts} />
                    </Box>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Box>

      <TablePagination
        component="div"
        count={totalCount}
        page={page - 1}
        rowsPerPage={pageSize}
        rowsPerPageOptions={[...PAGE_SIZE_OPTIONS]}
        onPageChange={(_, newPage) => setPage(newPage + 1)}
        onRowsPerPageChange={(e) => {
          setPageSize(Number(e.target.value));
        }}
        sx={{ "& .MuiToolbar-root": { fontSize: "0.8125rem" } }}
      />
    </Box>
  );
}
