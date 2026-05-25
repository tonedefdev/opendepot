"use client";

import * as React from "react";
import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Box,
  Chip,
  Divider,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import ExpandMoreIcon from "@mui/icons-material/ExpandMore";
import type { SecurityFinding } from "@/lib/api";

interface Props {
  sourceScanFindings: SecurityFinding[];
  binaryScanFindings: Record<string, SecurityFinding[]>;
}

function FindingsTable({ findings }: { findings: SecurityFinding[] }) {
  if (findings.length === 0) {
    return (
      <Typography variant="body2" color="text.secondary" sx={{ py: 1 }}>
        No findings.
      </Typography>
    );
  }
  return (
    <Table size="small">
      <TableHead>
        <TableRow>
          <TableCell>ID</TableCell>
          <TableCell>Severity</TableCell>
          <TableCell>Title</TableCell>
          <TableCell>Message</TableCell>
          <TableCell>Resolution</TableCell>
        </TableRow>
      </TableHead>
      <TableBody>
        {findings.map((f) => (
          <TableRow key={f.id} hover>
            <TableCell>
              <Typography variant="caption" fontFamily="monospace">
                {f.id}
              </Typography>
            </TableCell>
            <TableCell>
              <Chip
                size="small"
                label={f.severity}
                color={
                  f.severity === "CRITICAL"
                    ? "error"
                    : f.severity === "HIGH"
                      ? "warning"
                      : "default"
                }
              />
            </TableCell>
            <TableCell>{f.title}</TableCell>
            <TableCell sx={{ maxWidth: 200, whiteSpace: "normal", wordBreak: "break-word" }}>{f.message}</TableCell>
            <TableCell>{f.resolution}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export default function ScanDrillDown({ sourceScanFindings, binaryScanFindings }: Props) {
  const binaryKeys = Object.keys(binaryScanFindings ?? {});

  return (
    <Box>
      <Accordion defaultExpanded={sourceScanFindings.length > 0}>
        <AccordionSummary expandIcon={<ExpandMoreIcon />}>
          <Typography fontWeight={600}>
            Source Scan Findings ({sourceScanFindings.length})
          </Typography>
        </AccordionSummary>
        <AccordionDetails>
          <FindingsTable findings={sourceScanFindings} />
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
                <FindingsTable findings={binaryScanFindings[platform] ?? []} />
              </AccordionDetails>
            </Accordion>
          ))}
        </Box>
      )}
    </Box>
  );
}
