"use client";

import * as React from "react";
import { useEffect, useState } from "react";
import Link from "next/link";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import CircularProgress from "@mui/material/CircularProgress";
import Drawer from "@mui/material/Drawer";
import IconButton from "@mui/material/IconButton";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import Typography from "@mui/material/Typography";
import CloseIcon from "@mui/icons-material/Close";
import MobileRecordCard, { MobileRecordList } from "@/components/MobileRecordCard";
import type { JevReviewQueue, JevReviewQueueItem } from "@/lib/api";
import { RISK_COLORS } from "@/lib/jevSeverity";

/** Window event that opens the Needs review drawer from anywhere in the app. */
export const OPEN_NEEDS_REVIEW_EVENT = "opendepot:open-needs-review";

interface NeedsReviewDrawerProps {
  open: boolean;
  onClose: () => void;
}

function kindLabel(kind: string): string {
  return kind === "agent" ? "Agent" : "Skill";
}

/**
 * Lists Skills and Agent Versions whose jevAssessment.needsReview is true.
 *
 * Limitation: only each resource&apos;s latest-assessed Version is read, because
 * no list endpoint exposes needsReview. Older Versions that need review are
 * not shown.
 *
 * Cost: opening the drawer lists Skills and Agents, then fetches each
 * resource's detail in batches of 8, so one open costs roughly one detail
 * request per resource.
 */
export default function NeedsReviewDrawer({ open, onClose }: NeedsReviewDrawerProps) {
  const [queue, setQueue] = useState<JevReviewQueue | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!open) return;

    let cancelled = false;
    setLoading(true);
    setError(null);

    fetch("/api/review-queue", { cache: "no-store" })
      .then(async (response) => {
        const body: unknown = await response.json();
        if (!response.ok) {
          const message = typeof body === "object" && body !== null && "message" in body ? String(body.message) : "The review queue could not be loaded.";
          throw new Error(message);
        }
        return body as JevReviewQueue;
      })
      .then((result) => {
        if (!cancelled) setQueue(result);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : "The review queue could not be loaded.");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [open]);

  const items: JevReviewQueueItem[] = queue?.items ?? [];

  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={onClose}
      sx={{ "& .MuiDrawer-paper": { width: { xs: "100%", sm: 640 }, maxWidth: "100%", boxSizing: "border-box" } }}
    >
      <Box sx={{ display: "flex", flexDirection: "column", gap: 2, p: 3 }}>
        <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 2 }}>
          <Typography variant="h6">Needs review</Typography>
          <IconButton aria-label="Close needs review" onClick={onClose} sx={{ mr: -1 }}>
            <CloseIcon />
          </IconButton>
        </Box>
        <Typography variant="body2" color="text.secondary" sx={{ mt: -1.5, lineHeight: 1.6 }}>
          Jev flags a Skill or Agent when its Critical verdict comes from a fully scored assessment, or when a
          chunk&apos;s confidence falls below the minimum. Only each resource&apos;s latest assessed version is checked.
        </Typography>

        {error && <Alert severity="error">{error}</Alert>}

        {loading && (
          <Box sx={{ display: "flex", justifyContent: "center", py: 4 }}>
            <CircularProgress size={28} aria-label="Loading review queue" />
          </Box>
        )}

        {!loading && !error && queue && items.length === 0 && (
          <Box sx={{ pt: 1 }}>
            <Typography variant="body1" color="text.primary" fontWeight={600}>
              Nothing needs review right now.
            </Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5, lineHeight: 1.6 }}>
              Versions appear here when Jev returns a Critical verdict or cannot score a chunk with enough confidence.
              A Version under an active exemption stays out of this list and returns when the exemption expires.
            </Typography>
          </Box>
        )}

        {!loading && items.length > 0 && (
          <>
            <Box sx={{ display: { xs: "none", lg: "block" }, overflowX: "auto" }}>
              <Table size="small" aria-label="Items that need review">
                <TableHead>
                  <TableRow>
                    <TableCell sx={{ whiteSpace: "nowrap" }}>Name</TableCell>
                    <TableCell sx={{ whiteSpace: "nowrap" }}>Namespace</TableCell>
                    <TableCell sx={{ whiteSpace: "nowrap" }}>Kind</TableCell>
                    <TableCell sx={{ whiteSpace: "nowrap" }}>Risk</TableCell>
                    <TableCell sx={{ whiteSpace: "nowrap" }}>Blocked</TableCell>
                    <TableCell sx={{ whiteSpace: "nowrap" }}>Exempted</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {items.map((item) => (
                    <TableRow key={`${item.namespace}/${item.kind}/${item.name}`}>
                      <TableCell>
                        <Typography component={Link} href={`/${item.namespace}/${item.kind}/${item.name}`} variant="body2" sx={{ color: "primary.main" }}>
                          {item.name}
                        </Typography>
                      </TableCell>
                      <TableCell>{item.namespace}</TableCell>
                      <TableCell>{kindLabel(item.kind)}</TableCell>
                      <TableCell>
                        {item.riskLevel ? (
                          <Chip label={item.riskLevel} size="small" variant="outlined" color={RISK_COLORS[item.riskLevel] ?? "default"} />
                        ) : (
                          "—"
                        )}
                      </TableCell>
                      <TableCell>
                        {item.blocked ? <Chip label="Blocked" size="small" color="error" /> : "No"}
                      </TableCell>
                      <TableCell>
                        {item.exempted ? (
                          <Chip label="Expired" size="small" color="warning" variant="outlined" title="The exemption has expired, so this Version is back in review." />
                        ) : (
                          "No"
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Box>

            <MobileRecordList>
              {items.map((item) => (
                <MobileRecordCard
                  key={`${item.namespace}/${item.kind}/${item.name}`}
                  title={
                    <Typography component={Link} href={`/${item.namespace}/${item.kind}/${item.name}`} variant="body2" sx={{ color: "primary.main", fontWeight: 600 }}>
                      {item.name}
                    </Typography>
                  }
                  fields={[
                    { label: "Namespace", value: item.namespace },
                    { label: "Kind", value: kindLabel(item.kind) },
                    {
                      label: "Risk",
                      value: item.riskLevel ? (
                        <Chip label={item.riskLevel} size="small" variant="outlined" color={RISK_COLORS[item.riskLevel] ?? "default"} />
                      ) : (
                        "—"
                      ),
                    },
                    { label: "Blocked", value: item.blocked ? <Chip label="Blocked" size="small" color="error" /> : "No" },
                    {
                      label: "Exempted",
                      value: item.exempted ? (
                        <Chip label="Expired" size="small" color="warning" variant="outlined" title="The exemption has expired, so this Version is back in review." />
                      ) : (
                        "No"
                      ),
                    },
                  ]}
                />
              ))}
            </MobileRecordList>
          </>
        )}
      </Box>
    </Drawer>
  );
}
