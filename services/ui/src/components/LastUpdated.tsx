"use client";

import * as React from "react";
import { Tooltip, Typography } from "@mui/material";
import { displayLastUpdated } from "@/lib/relativeTime";

interface Props {
  /** RFC 3339 timestamp. Omitted when the server does not know it. */
  value?: string;
  variant: "body2" | "caption";
  /** Text placed before the time, for example "Updated". */
  prefix?: string;
}

/**
 * Last updated time with a full date tooltip. The clock is read only after mount, so the server render and the
 * first client render produce the same markup.
 */
export function LastUpdated({ value, variant, prefix }: Props) {
  const [now, setNow] = React.useState<number | null>(null);

  React.useEffect(() => {
    setNow(Date.now());
  }, []);

  const { text, title } = displayLastUpdated(value, now);

  return (
    <Tooltip title={title} placement="top" enterDelay={300} disableInteractive>
      <Typography variant={variant} noWrap color="text.secondary" sx={{ fontVariantNumeric: "tabular-nums" }}>
        {prefix ? `${prefix} ${text}` : text}
      </Typography>
    </Tooltip>
  );
}
