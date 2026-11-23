"use client";

import IconButton from "@mui/material/IconButton";
import MenuIcon from "@mui/icons-material/Menu";
import type { SxProps, Theme } from "@mui/material/styles";

export const TOGGLE_SIDEBAR_EVENT = "opendepot:toggle-sidebar";

interface MobileSidebarButtonProps {
  // Floating places the button over the page for pages without a PageHeader to anchor it.
  floating?: boolean;
}

const inlineSx: SxProps<Theme> = {
  display: { xs: "flex", sm: "none" },
  color: "text.secondary",
  borderRadius: "50%",
  flexShrink: 0,
  "&:hover": { bgcolor: "action.hover" },
};

// The fill is mostly opaque so content does not show through clearly, and the blur softens whatever passes beneath.
const floatingSx: SxProps<Theme> = (theme) => {
  const paper =
    theme.vars?.palette.background.paper ?? theme.palette.background.paper;
  const divider = theme.vars?.palette.divider ?? theme.palette.divider;
  return {
    display: { xs: "flex", sm: "none" },
    position: "fixed",
    right: 16,
    top: "calc(16px + env(safe-area-inset-top))",
    zIndex: theme.zIndex.appBar,
    // Sized to match the repository icon below it so both share one column.
    width: 38,
    height: 38,
    color: "text.primary",
    borderRadius: "50%",
    backgroundColor: `color-mix(in srgb, ${paper} 78%, transparent)`,
    backdropFilter: "blur(14px) saturate(140%)",
    WebkitBackdropFilter: "blur(14px) saturate(140%)",
    border: "1px solid",
    borderColor: `color-mix(in srgb, ${divider} 80%, transparent)`,
    boxShadow: "0 6px 20px rgba(0, 0, 0, 0.18)",
    "&:hover": {
      backgroundColor: `color-mix(in srgb, ${paper} 90%, transparent)`,
    },
  };
};

export default function MobileSidebarButton({
  floating = false,
}: MobileSidebarButtonProps) {
  return (
    <IconButton
      aria-label="open sidebar"
      onClick={() => window.dispatchEvent(new Event(TOGGLE_SIDEBAR_EVENT))}
      sx={floating ? floatingSx : inlineSx}
    >
      <MenuIcon sx={{ fontSize: 20 }} />
    </IconButton>
  );
}
