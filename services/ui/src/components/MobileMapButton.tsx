"use client";

import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import MapOutlinedIcon from "@mui/icons-material/MapOutlined";
import { useState } from "react";

export const TOGGLE_NODE_MAP_EVENT = "opendepot:toggle-node-map";

export default function MobileMapButton() {
  const [mapVisible, setMapVisible] = useState(false);

  return (
    <Tooltip title="Toggle node map">
      <IconButton
        aria-label={mapVisible ? "Hide node map" : "Show node map"}
        onClick={() => {
          setMapVisible((visible) => !visible);
          window.dispatchEvent(new Event(TOGGLE_NODE_MAP_EVENT));
        }}
        sx={{
          display: { xs: "flex", sm: "none" },
          color: "text.secondary",
          borderRadius: "50%",
          flexShrink: 0,
          "&:hover": { bgcolor: "action.hover" },
        }}
      >
        <MapOutlinedIcon />
      </IconButton>
    </Tooltip>
  );
}
