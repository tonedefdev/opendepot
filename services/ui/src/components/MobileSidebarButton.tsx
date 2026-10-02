"use client";

import IconButton from "@mui/material/IconButton";
import MenuIcon from "@mui/icons-material/Menu";

export const TOGGLE_SIDEBAR_EVENT = "opendepot:toggle-sidebar";

export default function MobileSidebarButton() {
  return (
    <IconButton
      aria-label="open sidebar"
      onClick={() => window.dispatchEvent(new Event(TOGGLE_SIDEBAR_EVENT))}
      sx={{
        display: { xs: "flex", sm: "none" },
        color: "text.secondary",
        borderRadius: "50%",
        flexShrink: 0,
        "&:hover": { bgcolor: "action.hover" },
      }}
    >
      <MenuIcon sx={{ fontSize: 20 }} />
    </IconButton>
  );
}
