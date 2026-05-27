"use client";

import * as React from "react";
import IconButton from "@mui/material/IconButton";
import Tooltip from "@mui/material/Tooltip";
import RefreshIcon from "@mui/icons-material/Refresh";
import { keyframes, styled } from "@mui/system";
import { useRouter } from "next/navigation";

const spin = keyframes`
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
`;

const SpinIcon = styled("span", {
  shouldForwardProp: (prop) => prop !== "spinning",
})<{ spinning?: boolean }>(({ spinning }) => ({
  display: "flex",
  alignItems: "center",
  animation: spinning ? `${spin} 0.7s linear infinite` : "none",
}));

interface Props {
  ariaLabel?: string;
}

export default function RefreshIconButton({ ariaLabel = "refresh" }: Props) {
  const router = useRouter();
  const [spinning, setSpinning] = React.useState(false);

  const handleRefresh = React.useCallback(() => {
    setSpinning(true);
    router.refresh();
    const id = setTimeout(() => setSpinning(false), 800);
    return () => clearTimeout(id);
  }, [router]);

  return (
    <Tooltip title="Refresh">
      <IconButton size="small" onClick={handleRefresh} aria-label={ariaLabel}>
        <SpinIcon spinning={spinning}>
          <RefreshIcon fontSize="small" />
        </SpinIcon>
      </IconButton>
    </Tooltip>
  );
}
