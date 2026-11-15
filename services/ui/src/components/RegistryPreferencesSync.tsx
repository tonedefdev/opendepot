"use client";

import * as React from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { loadRegistryPreferences } from "@/lib/registryPreferences";

// Fills in saved page size and sort when the URL does not specify them. The server reads those values from the URL.
export default function RegistryPreferencesSync() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  React.useEffect(() => {
    const prefs = loadRegistryPreferences();
    const params = new URLSearchParams(searchParams.toString());
    let changed = false;

    if (!params.has("page_size") && prefs.pageSize !== undefined) {
      params.set("page_size", String(prefs.pageSize));
      changed = true;
    }
    if (!params.has("sort_by") && prefs.sortBy !== undefined) {
      params.set("sort_by", prefs.sortBy);
      params.set("sort_dir", prefs.sortDir ?? "asc");
      changed = true;
    }

    if (changed) router.replace(`${pathname}?${params.toString()}`, { scroll: false });
  }, [pathname, router, searchParams]);

  return null;
}
