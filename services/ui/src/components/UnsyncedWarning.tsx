"use client";

import * as React from "react";
import Alert from "@mui/material/Alert";

interface UnsyncedWarningProps {
  initialHasUnsynced: boolean;
  clearRef: React.MutableRefObject<(() => void) | null>;
}

export default function UnsyncedWarning({ initialHasUnsynced, clearRef }: UnsyncedWarningProps) {
  const [hasUnsynced, setHasUnsynced] = React.useState(initialHasUnsynced);

  React.useEffect(() => {
    clearRef.current = () => setHasUnsynced(false);
  }, [clearRef]);

  if (!hasUnsynced) return null;

  return (
    <Alert severity="warning" sx={{ mb: 2 }}>
      Some versions are out of sync
    </Alert>
  );
}
