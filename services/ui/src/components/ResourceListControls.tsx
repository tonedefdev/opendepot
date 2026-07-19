"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Pagination from "@mui/material/Pagination";
import Select from "@mui/material/Select";
import MenuItem from "@mui/material/MenuItem";
import FormControl from "@mui/material/FormControl";
import InputLabel from "@mui/material/InputLabel";
import Typography from "@mui/material/Typography";
import { useRouter } from "next/navigation";

interface ResourceListControlsProps {
  totalCount: number;
  page: number;
  pageSize: number;
  baseParams: string;
}

const PAGE_SIZE_OPTIONS = [12, 24, 48, 96];

export default function ResourceListControls({
  totalCount,
  page,
  pageSize,
  baseParams,
}: ResourceListControlsProps) {
  const router = useRouter();
  const totalPages = Math.max(1, Math.ceil(totalCount / pageSize));

  const buildUrl = (newPage: number, newPageSize: number) => {
    const params = new URLSearchParams(baseParams);
    params.set("page", String(newPage));
    params.set("page_size", String(newPageSize));
    return `/?${params.toString()}`;
  };

  const handlePageChange = (_: React.ChangeEvent<unknown>, value: number) => {
    router.push(buildUrl(value, pageSize));
  };

  const handlePageSizeChange = (value: number) => {
    router.push(buildUrl(1, value));
  };

  if (totalCount === 0) return null;

  const start = (page - 1) * pageSize + 1;
  const end = Math.min(page * pageSize, totalCount);

  return (
    <Box
      sx={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        flexWrap: "wrap",
        gap: 2,
        mt: 3,
        pt: 2,
        borderTop: "1px solid",
        borderColor: "divider",
      }}
    >
      <Typography variant="body2" color="text.secondary">
        {start}–{end} of {totalCount} resource{totalCount !== 1 ? "s" : ""}
      </Typography>

      <Box sx={{ display: "flex", alignItems: "center", gap: 2 }}>
        <FormControl size="small" variant="outlined" sx={{ minWidth: 110 }}>
          <InputLabel id="page-size-label" sx={{ fontSize: "0.8rem" }}>
            Per page
          </InputLabel>
          <Select
            labelId="page-size-label"
            value={pageSize}
            label="Per page"
            onChange={(e) => handlePageSizeChange(Number(e.target.value))}
            sx={{ fontSize: "0.8rem" }}
          >
            {PAGE_SIZE_OPTIONS.map((s) => (
              <MenuItem key={s} value={s} sx={{ fontSize: "0.8rem" }}>
                {s}
              </MenuItem>
            ))}
          </Select>
        </FormControl>

        <Pagination
          count={totalPages}
          page={page}
          onChange={handlePageChange}
          color="primary"
          size="small"
          siblingCount={1}
          boundaryCount={1}
        />
      </Box>
    </Box>
  );
}
