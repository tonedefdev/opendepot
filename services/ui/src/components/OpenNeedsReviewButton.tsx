"use client";

import * as React from "react";
import ButtonBase from "@mui/material/ButtonBase";
import { OPEN_NEEDS_REVIEW_EVENT } from "@/components/NeedsReviewDrawer";

/** Wraps content so that activating it opens the Needs review drawer in the sidebar. */
export default function OpenNeedsReviewButton({ children }: { children: React.ReactNode }) {
  return (
    <ButtonBase
      aria-label="Open the Needs review queue"
      onClick={() => window.dispatchEvent(new Event(OPEN_NEEDS_REVIEW_EVENT))}
      sx={{
        display: "block",
        textAlign: "left",
        borderRadius: 1,
        mx: -0.5,
        px: 0.5,
        py: 0.5,
        "&:focus-visible": { outline: "2px solid", outlineColor: "primary.main", outlineOffset: 2 },
      }}
    >
      {children}
    </ButtonBase>
  );
}
