"use client";

import * as React from "react";
import UnsyncedWarning from "@/components/UnsyncedWarning";
import VersionsTable from "@/components/VersionsTable";

interface DrillDownWarningBridgeProps {
  initialHasUnsynced: boolean;
  namespace: string;
  kind: string;
  name: string;
}

export default function DrillDownWarningBridge({
  initialHasUnsynced,
  namespace,
  kind,
  name,
}: DrillDownWarningBridgeProps) {
  const clearRef = React.useRef<(() => void) | null>(null);

  return (
    <>
      <UnsyncedWarning initialHasUnsynced={initialHasUnsynced} clearRef={clearRef} />
      <VersionsTable
        namespace={namespace}
        kind={kind}
        name={name}
        onAllSynced={() => clearRef.current?.()}
      />
    </>
  );
}
