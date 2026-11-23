import * as React from "react";
import Grid from "@mui/material/Grid";
import Paper from "@mui/material/Paper";
import Skeleton from "@mui/material/Skeleton";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableContainer from "@mui/material/TableContainer";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import { DEFAULT_REGISTRY_PAGE_SIZE } from "@/lib/registryPreferences";
import { RISK_COLUMN_MIN_PX } from "./riskColumnWidth";
import { LAST_UPDATED_COLUMN_PX, type ListView } from "./resourceListLayout";

// Matches the body text line in ResourceRow, so a loaded table does not shift height.
const LINE_PX = 20;

// Header label bars, in column order: Kind, Name, Version, Source, Status, Last updated, Risk.
const HEADER_BAR_PX = [44, 40, 56, 52, 48, 92, 36];

interface TableSkeletonProps {
  rows?: number;
  showSource?: boolean;
  riskWidth?: number;
}

// Same columns, widths, and padding as ResourceList's table, so the placeholder lines up with the loaded rows.
export function ResourceTableSkeleton({
  rows = DEFAULT_REGISTRY_PAGE_SIZE,
  showSource = true,
  riskWidth = RISK_COLUMN_MIN_PX,
}: TableSkeletonProps) {
  return (
    <TableContainer component={Paper} variant="outlined" aria-busy="true" data-testid="resource-table-skeleton">
      <Table aria-label="Loading registry resources" sx={{ tableLayout: "fixed" }}>
        <TableHead>
          <TableRow>
            <TableCell sx={{ width: 132 }}>
              <Skeleton variant="text" width={HEADER_BAR_PX[0]} height={LINE_PX} />
            </TableCell>
            <TableCell>
              <Skeleton variant="text" width={HEADER_BAR_PX[1]} height={LINE_PX} />
            </TableCell>
            <TableCell sx={{ width: 104 }}>
              <Skeleton variant="text" width={HEADER_BAR_PX[2]} height={LINE_PX} />
            </TableCell>
            {showSource && (
              <TableCell sx={{ width: 128, display: { xs: "none", lg: "table-cell" } }}>
                <Skeleton variant="text" width={HEADER_BAR_PX[3]} height={LINE_PX} />
              </TableCell>
            )}
            <TableCell sx={{ width: 156 }}>
              <Skeleton variant="text" width={HEADER_BAR_PX[4]} height={LINE_PX} />
            </TableCell>
            <TableCell sx={{ width: LAST_UPDATED_COLUMN_PX }}>
              <Skeleton variant="text" width={HEADER_BAR_PX[5]} height={LINE_PX} />
            </TableCell>
            <TableCell sx={{ width: riskWidth, pr: "16px" }}>
              <Skeleton variant="text" width={HEADER_BAR_PX[6]} height={LINE_PX} />
            </TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {Array.from({ length: rows }).map((_, i) => (
            <TableRow key={i}>
              <TableCell>
                <Skeleton variant="text" width="70%" height={LINE_PX} />
              </TableCell>
              <TableCell>
                <Skeleton variant="text" width="80%" height={LINE_PX} />
              </TableCell>
              <TableCell>
                <Skeleton variant="text" width="60%" height={LINE_PX} />
              </TableCell>
              {showSource && (
                <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
                  <Skeleton variant="text" width="75%" height={LINE_PX} />
                </TableCell>
              )}
              <TableCell>
                <Skeleton variant="text" width="65%" height={LINE_PX} />
              </TableCell>
              <TableCell>
                <Skeleton variant="text" width="70%" height={LINE_PX} />
              </TableCell>
              <TableCell sx={{ pr: "16px" }}>
                <Skeleton variant="text" width="50%" height={LINE_PX} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
  );
}

export function ResourceCardSkeleton({ rows = DEFAULT_REGISTRY_PAGE_SIZE }: { rows?: number }) {
  return (
    <Grid container spacing={2} aria-busy="true" data-testid="resource-card-skeleton">
      {Array.from({ length: rows }).map((_, i) => (
        <Grid key={i} size={{ xs: 12, sm: 6, md: 4, lg: 3 }}>
          <Skeleton variant="rounded" height={180} />
        </Grid>
      ))}
    </Grid>
  );
}

interface ResourceListSkeletonProps extends TableSkeletonProps {
  view: ListView;
}

// Picks the skeleton for the view the list will render, so table mode never shows card placeholders.
export function ResourceListSkeleton({ view, ...props }: ResourceListSkeletonProps) {
  if (view === "cards") return <ResourceCardSkeleton rows={props.rows} />;
  return <ResourceTableSkeleton {...props} />;
}
