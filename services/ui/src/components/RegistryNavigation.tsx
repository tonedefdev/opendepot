"use client";

import * as React from "react";
import { useRouter } from "next/navigation";

interface RegistryNavigationValue {
  /** True while a list navigation (page, page size, sort, filter) is in flight. */
  isPending: boolean;
  /** The search string being navigated to while pending, or null when settled. */
  pendingSearch: string | null;
  navigate: (href: string) => void;
}

const RegistryNavigationContext = React.createContext<RegistryNavigationValue | null>(null);

// Runs list navigation in a transition, so the pending state is visible at once and the old list stays interactive.
export function RegistryNavigationProvider({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const [isPending, startTransition] = React.useTransition();
  const [pendingSearch, setPendingSearch] = React.useState<string | null>(null);

  const navigate = React.useCallback(
    (href: string) => {
      const search = href.includes("?") ? href.slice(href.indexOf("?") + 1) : "";
      // Set outside the transition so the pager moves at once. State set inside a transition waits for the navigation to commit.
      setPendingSearch(search);
      startTransition(() => {
        router.push(href);
      });
    },
    [router],
  );

  const value = React.useMemo(
    () => ({ isPending, pendingSearch: isPending ? pendingSearch : null, navigate }),
    [isPending, pendingSearch, navigate],
  );

  return <RegistryNavigationContext.Provider value={value}>{children}</RegistryNavigationContext.Provider>;
}

/** Registry navigation from the provider, or a plain router push when no provider is mounted (embedded uses). */
export function useRegistryNavigation(): RegistryNavigationValue {
  const context = React.useContext(RegistryNavigationContext);
  const router = useRouter();
  const fallback = React.useMemo<RegistryNavigationValue>(
    () => ({ isPending: false, pendingSearch: null, navigate: (href) => router.push(href) }),
    [router],
  );
  return context ?? fallback;
}
