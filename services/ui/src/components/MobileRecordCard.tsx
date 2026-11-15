"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Skeleton from "@mui/material/Skeleton";
import Typography from "@mui/material/Typography";

export interface RecordField {
  label: string;
  value: React.ReactNode;
  /** Spans both columns, for long text such as descriptions, messages, and checksums. */
  wide?: boolean;
}

const cardSx = {
  bgcolor: "background.paper",
  border: "1px solid",
  borderColor: "divider",
  borderRadius: 2,
  p: 2,
  minWidth: 0,
} as const;

interface MobileRecordCardProps {
  title: React.ReactNode;
  fields: RecordField[];
}

/**
 * Card stand-in for one table row on viewports below `lg`. Labels sit above
 * their values so long identifiers and descriptions get the full card width.
 */
export default function MobileRecordCard({ title, fields }: MobileRecordCardProps) {
  return (
    <Box sx={cardSx}>
      <Box sx={{ minWidth: 0, overflowWrap: "anywhere" }}>{title}</Box>
      {fields.length > 0 && (
        <Box
          component="dl"
          sx={{ m: 0, mt: 1.5, display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: 1.5 }}
        >
          {fields.map((field) => (
            <Box key={field.label} sx={{ minWidth: 0, gridColumn: field.wide ? "1 / -1" : undefined }}>
              <Typography component="dt" variant="caption" color="text.secondary" sx={{ display: "block", mb: 0.25 }}>
                {field.label}
              </Typography>
              <Box component="dd" sx={{ m: 0, minWidth: 0, fontSize: "0.8125rem", overflowWrap: "anywhere" }}>
                {field.value}
              </Box>
            </Box>
          ))}
        </Box>
      )}
    </Box>
  );
}

/** Stack of record cards. Visible below `lg`, where the matching table is hidden. */
export function MobileRecordList({ children }: { children: React.ReactNode }) {
  return <Box sx={{ display: { xs: "flex", lg: "none" }, flexDirection: "column", gap: 1.5 }}>{children}</Box>;
}

export function MobileRecordCardSkeleton({ fieldCount }: { fieldCount: number }) {
  return (
    <Box sx={cardSx}>
      <Skeleton variant="text" width="50%" />
      <Box sx={{ mt: 1.5, display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: 1.5 }}>
        {Array.from({ length: fieldCount }).map((_, i) => (
          <Skeleton key={i} variant="text" width="80%" />
        ))}
      </Box>
    </Box>
  );
}
