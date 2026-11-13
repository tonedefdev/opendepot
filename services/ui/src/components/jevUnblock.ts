import type { BrowseVersionSummary, JevAssessment, ScanPolicy, ScanPolicyJevExemption } from "@/lib/api";

type VersionIdentity = Pick<BrowseVersionSummary, "name" | "blocked">;
import {
  governance,
  governingPolicies,
  winnerFor,
  type GovernedResource,
  type Governance,
} from "./security-policies/jevGovernance";

// JevUnblock describes what a blocked Jev assessment needs before it can be exempted. Each state is one condition the
// UI shows; "unknown" covers the cases where existing data cannot decide which policy governs the Version.
export type JevUnblock =
  | { state: "no-digest"; namespace: string; versionName?: string }
  | { state: "needs-entry"; digest: string; policy?: string }
  | { state: "superseded"; digest: string; policy: string; winner: string; policyPriority: number; winnerPriority: number }
  | { state: "expired"; digest: string; policy: string; expires: string }
  | { state: "pending"; namespace: string; digest: string; policy: string; versionName?: string }
  | { state: "exempted"; policy: string; expires?: string }
  | { state: "unknown"; digest: string; reason: string; candidates: string[] };

interface MatchingEntry {
  policy: ScanPolicy;
  entry: ScanPolicyJevExemption;
  governs: Governance;
}

// blockedVersionName returns the Version object name only when exactly one blocked Version is known. The assessment does
// not say which Version it belongs to, so a guess would put the wrong name into the kubectl command.
export function blockedVersionName(versions: VersionIdentity[] = []): string | undefined {
  const names = versions.filter((version) => version.blocked && version.name).map((version) => version.name as string);
  return names.length === 1 ? names[0] : undefined;
}

// isExpired mirrors the server: an entry without expires never expires, and an entry expires once expires is not after now.
function isExpired(entry: ScanPolicyJevExemption, now: Date): boolean {
  return entry.expires !== undefined && !(Date.parse(entry.expires) > now.getTime());
}

// describeJevUnblock returns the unblock state for an assessment. It returns null when there is nothing to unblock.
// policies is null when the ScanPolicy list could not be read.
export function describeJevUnblock({
  assessment,
  namespace,
  resource,
  versions,
  policies,
  now = new Date(),
}: {
  assessment: JevAssessment;
  namespace: string;
  resource: GovernedResource;
  versions?: VersionIdentity[];
  policies: ScanPolicy[] | null;
  now?: Date;
}): JevUnblock | null {
  if (assessment.exempted) {
    return { state: "exempted", policy: assessment.exemptedBy ?? "an unnamed policy", expires: assessment.exemptionExpires };
  }
  if (!assessment.blocked) return null;

  const versionName = blockedVersionName(versions);
  const digest = assessment.contentDigest;
  if (!digest) return { state: "no-digest", namespace, versionName };
  if (!policies) {
    return { state: "unknown", digest, reason: "The ScanPolicies in this namespace could not be read.", candidates: [] };
  }

  const winner = winnerFor(governingPolicies(policies, resource));
  if (winner.kind === "unknown") {
    return {
      state: "unknown",
      digest,
      reason: "A policy that uses a selector or a version constraint may govern this Version. The UI cannot tell which one wins.",
      candidates: winner.candidates.map((policy) => policy.metadata.name),
    };
  }

  // Exact digest match, the same comparison the server makes. Shadowed and unknown policies are checked below.
  const matches: MatchingEntry[] = policies.flatMap((policy) =>
    (policy.spec.jevExemptions ?? [])
      .filter((entry) => entry.contentDigest === digest)
      .map((entry) => ({ policy, entry, governs: governance(policy, resource) })),
  );
  if (winner.kind === "known") {
    const winnerName = winner.policy.metadata.name;
    const winningMatch = matches.find((match) => match.governs === "yes" && match.policy.metadata.name === winnerName);
    if (winningMatch) {
      if (isExpired(winningMatch.entry, now)) {
        return { state: "expired", digest, policy: winnerName, expires: winningMatch.entry.expires ?? "" };
      }
      return { state: "pending", namespace, digest, policy: winnerName, versionName };
    }

    // A matching entry in a policy that governs this Version but does not win is ignored in full, so the winner is named.
    const shadowed = matches.find((match) => match.governs === "yes" && match.policy.metadata.name !== winnerName);
    if (shadowed) {
      return {
        state: "superseded",
        digest,
        policy: shadowed.policy.metadata.name,
        winner: winnerName,
        policyPriority: shadowed.policy.spec.priority ?? 0,
        winnerPriority: winner.policy.spec.priority ?? 0,
      };
    }
    return { state: "needs-entry", digest, policy: winnerName };
  }

  return { state: "needs-entry", digest };
}
