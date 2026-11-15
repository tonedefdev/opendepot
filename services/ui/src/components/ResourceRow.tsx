"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import TableCell from "@mui/material/TableCell";
import TableRow from "@mui/material/TableRow";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import PublicIcon from "@mui/icons-material/Public";
import Link from "next/link";
import { useRouter } from "next/navigation";
import SeverityBadge from "./SeverityBadge";
import SourceRefChip from "./SourceRefChip";
import RiskMeter from "./RiskMeter";
import { KindLabel, SyncStatus, displayVersion, isAgentKind } from "./resourceDisplay";
import type { BrowseResource } from "@/lib/api";

interface Props {
  resource: BrowseResource;
  /** False when no row on the page has a source ref, so the list drops the column. */
  showSource: boolean;
}

export default function ResourceRow({ resource, showSource }: Props) {
  const href = `/${resource.namespace}/${resource.kind}/${resource.name}`;
  const address = `${resource.namespace}/${resource.name}`;
  const nameRef = React.useRef<HTMLAnchorElement>(null);
  const [nameTruncated, setNameTruncated] = React.useState(false);
  const router = useRouter();

  React.useEffect(() => {
    const el = nameRef.current;
    if (el) setNameTruncated([...el.children].some((part) => part.scrollWidth > part.clientWidth));
  }, [address]);

  // The name link stretches over the row, but hover targets lifted above it (status, risk chips, kind logo)
  // swallow clicks. Route those clicks to the same destination the link would have reached.
  const onRowClick = (event: React.MouseEvent<HTMLTableRowElement>) => {
    if ((event.target as HTMLElement).closest("a, button, input")) return;
    if (event.metaKey || event.ctrlKey) {
      window.open(href, "_blank", "noopener");
      return;
    }
    router.push(href);
  };

  return (
    <TableRow
      data-testid="resource-row"
      hover
      onClick={onRowClick}
      sx={{ position: "relative", cursor: "pointer", "&:last-child td": { borderBottom: 0 } }}
    >
      <TableCell>
        <KindLabel resource={resource} />
      </TableCell>

      <TableCell>
        <Box display="flex" alignItems="center" gap={1} minWidth={0}>
          <Tooltip title={nameTruncated ? address : ""} placement="top" enterDelay={300} disableInteractive>
            <Typography
              ref={nameRef}
              component={Link}
              href={href}
              variant="body2"
              sx={{
                display: "flex",
                minWidth: 0,
                whiteSpace: "nowrap",
                color: "text.secondary",
                textDecoration: "none",
                "&::after": { content: '""', position: "absolute", inset: 0 },
                "&:focus-visible": { outline: "none" },
                "&:focus-visible::after": {
                  outline: "2px solid",
                  outlineColor: "primary.main",
                  outlineOffset: -2,
                },
              }}
            >
              {/* When the column is tight the namespace gives way first, so the name stays readable. */}
              <Box component="span" sx={{ flex: "0 1000 auto", minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }}>
                {resource.namespace}
              </Box>
              <span>/</span>
              <Box
                component="span"
                sx={{ flex: "0 1 auto", minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", color: "text.primary", fontWeight: 600 }}
              >
                {resource.name}
              </Box>
            </Typography>
          </Tooltip>
          {resource.public && (
            <Tooltip title="Public: readable without signing in">
              <PublicIcon sx={{ position: "relative", zIndex: 1, fontSize: 14, color: "info.main", flexShrink: 0 }} />
            </Tooltip>
          )}
        </Box>
      </TableCell>

      <TableCell>
        <Typography
          variant="body2"
          noWrap
          sx={{ fontFamily: "var(--od-font-mono)", fontSize: "0.8125rem", color: resource.latestVersion ? "text.primary" : "text.disabled" }}
        >
          {resource.latestVersion ? displayVersion(resource.latestVersion) : "none"}
        </Typography>
      </TableCell>

      {showSource && (
        <TableCell sx={{ display: { xs: "none", lg: "table-cell" } }}>
          <SourceRefChip
            sourceRef={resource.sourceRef}
            sourceCommit={resource.sourceCommit}
            sourcePath={resource.sourcePath}
            repoUrl={resource.repoUrl}
            linked={false}
            plain
          />
        </TableCell>
      )}

      <TableCell>
        <SyncStatus resource={resource} testId="resource-row-sync-status" />
      </TableCell>

      <TableCell>
        <Box display="flex" alignItems="center" gap={0.5} sx={{ overflow: "hidden" }}>
          {isAgentKind(resource.kind) && resource.jevRiskLevel && <RiskMeter level={resource.jevRiskLevel} />}
          {resource.scanCounts && <SeverityBadge counts={resource.scanCounts} />}
        </Box>
      </TableCell>
    </TableRow>
  );
}
