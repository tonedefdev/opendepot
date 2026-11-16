"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Grid from "@mui/material/Grid";
import IconButton from "@mui/material/IconButton";
import MenuItem from "@mui/material/MenuItem";
import Paper from "@mui/material/Paper";
import Select from "@mui/material/Select";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableContainer from "@mui/material/TableContainer";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import TableSortLabel from "@mui/material/TableSortLabel";
import ToggleButton from "@mui/material/ToggleButton";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import { useTheme } from "@mui/material/styles";
import useMediaQuery from "@mui/material/useMediaQuery";
import ArrowDownwardIcon from "@mui/icons-material/ArrowDownward";
import ArrowUpwardIcon from "@mui/icons-material/ArrowUpward";
import ViewListIcon from "@mui/icons-material/ViewList";
import ViewModuleIcon from "@mui/icons-material/ViewModule";
import { useRouter } from "next/navigation";
import ResourceCard from "./ResourceCard";
import ResourceRow from "./ResourceRow";
import type { BrowseResource } from "@/lib/api";
import { loadRegistryPreferences, saveRegistryPreferences } from "@/lib/registryPreferences";

type ListView = "rows" | "cards";
type SortDir = "asc" | "desc";

const SORT_OPTIONS = [
  { value: "name", label: "Name" },
  { value: "namespace", label: "Namespace" },
  { value: "latest_version", label: "Latest version" },
  { value: "kind", label: "Kind" },
  { value: "status", label: "Status" },
  { value: "source", label: "Source" },
  { value: "risk", label: "Risk" },
];

interface Props {
  items: BrowseResource[];
  totalCount: number;
  sortBy: string;
  sortDir: SortDir;
  pageSize: number;
  /** The active filters and sort as a query string, without paging. */
  baseParams: string;
  namespace?: string;
  query?: string;
}

// Below md the table cannot fit its columns, so cards are the only layout there.
export default function ResourceList({ items, totalCount, sortBy, sortDir, pageSize, baseParams, namespace, query }: Props) {
  const theme = useTheme();
  const router = useRouter();
  const compact = useMediaQuery(theme.breakpoints.down("lg"));
  const [view, setView] = React.useState<ListView>("rows");
  // Read after mount so the server-rendered markup matches the first client render.
  React.useEffect(() => {
    const saved = loadRegistryPreferences().view;
    if (saved) setView(saved);
  }, []);
  const showCards = compact || view === "cards";
  // Only skills and agents carry a source ref. A page of modules or providers would show an empty column.
  const showSource = items.some((r) => !!r.sourceRef);

  const navigate = (updates: Record<string, string | null>) => {
    // Keep the current page size so a sort or filter change does not briefly fall back to the default.
    const params = new URLSearchParams(baseParams);
    params.set("page_size", String(pageSize));
    for (const [key, value] of Object.entries(updates)) {
      if (value === null) params.delete(key);
      else params.set(key, value);
    }
    router.push(`/?${params.toString()}`);
  };

  // Choosing the active column again flips its direction, as a table header is expected to.
  const sortByColumn = (column: string) => {
    const nextDir: SortDir = sortBy === column && sortDir === "asc" ? "desc" : "asc";
    saveRegistryPreferences({ sortBy: column, sortDir: nextDir });
    navigate({ sort_by: column, sort_dir: nextDir });
  };

  const sortableHead = (column: string, label: string) => (
    <TableSortLabel
      active={sortBy === column}
      direction={sortBy === column ? sortDir : "asc"}
      onClick={() => sortByColumn(column)}
      sx={{ verticalAlign: "baseline" }}
    >
      {label}
    </TableSortLabel>
  );

  return (
    <>
      <Box sx={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: 1, mb: 1.5, minHeight: 34 }}>
        <Typography variant="body2" color="text.secondary" sx={{ fontVariantNumeric: "tabular-nums" }}>
          {totalCount} {totalCount === 1 ? "artifact" : "artifacts"}
        </Typography>
        {namespace && <Chip size="small" variant="outlined" label={`Namespace: ${namespace}`} onDelete={() => navigate({ namespace: null })} />}
        {query && <Chip size="small" variant="outlined" label={`Search: ${query}`} onDelete={() => navigate({ q: null })} />}

        <Box sx={{ display: "flex", alignItems: "center", gap: 0.5, ml: "auto" }}>
          <Select
            size="small"
            value={SORT_OPTIONS.some((o) => o.value === sortBy) ? sortBy : "name"}
            onChange={(e) => {
              saveRegistryPreferences({ sortBy: e.target.value, sortDir });
              navigate({ sort_by: e.target.value });
            }}
            renderValue={(value) => `Sort: ${SORT_OPTIONS.find((o) => o.value === value)?.label ?? value}`}
            inputProps={{ "aria-label": "Sort by" }}
            sx={{ fontSize: "0.8125rem", height: 34 }}
          >
            {SORT_OPTIONS.map((o) => (
              <MenuItem key={o.value} value={o.value} sx={{ fontSize: "0.8125rem" }}>
                {o.label}
              </MenuItem>
            ))}
          </Select>
          <Tooltip title={sortDir === "asc" ? "Ascending. Switch to descending" : "Descending. Switch to ascending"}>
            <IconButton
              size="small"
              aria-label="Reverse sort order"
              onClick={() => {
                const nextDir: SortDir = sortDir === "asc" ? "desc" : "asc";
                saveRegistryPreferences({ sortBy, sortDir: nextDir });
                navigate({ sort_dir: nextDir });
              }}
              sx={{ borderRadius: "6px" }}
            >
              {sortDir === "asc" ? <ArrowUpwardIcon fontSize="small" /> : <ArrowDownwardIcon fontSize="small" />}
            </IconButton>
          </Tooltip>
          <ToggleButtonGroup
            exclusive
            size="small"
            aria-label="Registry view"
            value={view}
            onChange={(_, next: ListView | null) => {
              if (next) {
                setView(next);
                saveRegistryPreferences({ view: next });
              }
            }}
            sx={{ display: { xs: "none", lg: "inline-flex" }, ml: 1, "& .MuiToggleButton-root": { height: 34, px: 1 } }}
          >
            <ToggleButton value="rows" aria-label="Table view">
              <ViewListIcon fontSize="small" />
            </ToggleButton>
            <ToggleButton value="cards" aria-label="Card view">
              <ViewModuleIcon fontSize="small" />
            </ToggleButton>
          </ToggleButtonGroup>
        </Box>
      </Box>

      {showCards ? (
        <Grid container spacing={2}>
          {items.map((r) => (
            <Grid key={`${r.namespace}/${r.kind}/${r.name}`} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
              <ResourceCard resource={r} />
            </Grid>
          ))}
        </Grid>
      ) : (
        <TableContainer component={Paper} variant="outlined">
          {/* Risk is content-sized: Name and Source take the remaining width (width 100%, maxWidth 0), so other cells fit their content. */}
          <Table aria-label="Registry resources" sx={{ "& th, & td": { px: 1, whiteSpace: "nowrap" } }}>
            <TableHead>
              <TableRow>
                <TableCell>{sortableHead("kind", "Kind")}</TableCell>
                <TableCell>{sortableHead("name", "Name")}</TableCell>
                <TableCell>{sortableHead("latest_version", "Version")}</TableCell>
                {showSource && (
                  <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>{sortableHead("source", "Source")}</TableCell>
                )}
                <TableCell>{sortableHead("status", "Status")}</TableCell>
                <TableCell>{sortableHead("risk", "Risk")}</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {items.map((r) => (
                <ResourceRow key={`${r.namespace}/${r.kind}/${r.name}`} resource={r} showSource={showSource} />
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      )}
    </>
  );
}
