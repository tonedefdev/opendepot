import type { JevUnblock as ServerJevUnblock } from "@/lib/api";

// JevUnblockView is the display model for a blocked Jev assessment. Each state is one condition the UI shows. The server
// computes the state, and describeJevUnblock only maps it onto this model. policy and winner are absent when the server
// hid the name, or sent an empty one, so the UI never shows an empty name as if it were real.
export type JevUnblockView =
  | { state: "no-digest"; namespace: string }
  | { state: "needs-entry"; digest: string; policy?: string }
  | { state: "superseded"; digest: string; policy?: string; winner?: string; policyPriority: number; winnerPriority: number }
  | { state: "expired"; digest: string; policy?: string; expires?: string }
  | { state: "pending"; namespace: string; digest: string; policy?: string }
  | { state: "exempted"; policy?: string; expires?: string }
  | { state: "unknown"; digest?: string; expires?: string };

// visibleName returns the policy name only when the server shows it. An empty name counts as absent.
function visibleName(name: string | undefined, namesHidden: boolean): string | undefined {
  if (namesHidden || !name) return undefined;
  return name;
}

// describeJevUnblock maps the server's jevUnblock onto the display model. It returns null when nothing needs unblocking,
// which is the none state or no jevUnblock at all. A state that needs a digest but has none maps to unknown, because the
// UI has no digest to show for it.
export function describeJevUnblock({
  unblock,
  namespace,
}: {
  unblock: ServerJevUnblock | undefined;
  namespace: string;
}): JevUnblockView | null {
  if (!unblock || unblock.state === "none") return null;

  const digest = unblock.digest;
  const policy = visibleName(unblock.governingPolicy, unblock.namesHidden);
  const matchingPolicy = visibleName(unblock.matchingPolicy, unblock.namesHidden);
  const expires = unblock.expires;

  switch (unblock.state) {
    case "no-digest":
      return { state: "no-digest", namespace };
    case "exempted":
      return { state: "exempted", policy, expires };
    case "unknown":
      return { state: "unknown", digest, expires };
    case "needs-entry":
      return digest ? { state: "needs-entry", digest, policy } : { state: "unknown" };
    case "superseded":
      return digest
        ? {
            state: "superseded",
            digest,
            policy: matchingPolicy,
            winner: policy,
            policyPriority: unblock.matchingPriority,
            winnerPriority: unblock.governingPriority,
          }
        : { state: "unknown" };
    case "expired":
      return digest ? { state: "expired", digest, policy, expires } : { state: "unknown" };
    case "pending":
      return digest ? { state: "pending", namespace, digest, policy } : { state: "unknown" };
    default:
      // A state this UI does not know is shown as unknown rather than dropped, so the Version is not silently treated as unblocked.
      return { state: "unknown", digest };
  }
}
