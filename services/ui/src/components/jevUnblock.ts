import type { JevAssessment, JevExemptionGovernance, ScanPolicy, ScanPolicyJevExemption } from "@/lib/api";
import {
  governance,
  governingPolicies,
  winnerFor,
  type GovernedResource,
  type Governance,
} from "./security-policies/jevGovernance";

// JevUnblock describes what a blocked Jev assessment needs before it can be exempted. Each state is one condition the
// UI shows. "unknown" covers the cases where the UI cannot name the policy that governs the Version, either because the
// server hides the name from this caller or because the inferred data cannot decide it.
export type JevUnblock =
  | { state: "no-digest"; namespace: string }
  | { state: "needs-entry"; digest: string; policy?: string }
  | { state: "superseded"; digest: string; policy: string; winner: string; policyPriority: number; winnerPriority: number }
  | { state: "expired"; digest: string; policy: string; expires: string }
  | { state: "pending"; namespace: string; digest: string; policy: string }
  | { state: "exempted"; policy: string; expires?: string }
  | { state: "unknown"; digest?: string; reason: string; candidates: string[]; expires?: string };

interface MatchingEntry {
  policy: ScanPolicy;
  entry: ScanPolicyJevExemption;
  governs: Governance;
}

// isExpired mirrors the server: an entry without expires never expires, and an entry expires once expires is not after now.
function isExpired(entry: ScanPolicyJevExemption, now: Date): boolean {
  return entry.expires !== undefined && !(Date.parse(entry.expires) > now.getTime());
}

// nameHidden is the unknown state for a policy whose name the server withheld from this caller. Browse hides names from
// OIDC callers, so an empty name here does not mean there is no policy.
function nameHidden(reason: string, digest?: string, expires?: string): JevUnblock {
  return { state: "unknown", digest, reason, candidates: [], expires };
}

// describeFromGovernance maps the server's jevExemptionGovernance decision onto an unblock state. The server made the
// governing and matching decisions, so the UI reads them rather than re-deriving them from ScanPolicy specs.
function describeFromGovernance({
  assessment,
  namespace,
  decision,
}: {
  assessment: JevAssessment;
  namespace: string;
  decision: JevExemptionGovernance;
}): JevUnblock | null {
  const digest = assessment.contentDigest;
  const { matchingState, governingPolicy, matchingPolicy, matchingExpires, governingPriority, matchingPriority } = decision;

  if (!assessment.blocked) {
    if (matchingState !== "exempted") return null;
    if (!governingPolicy) {
      return nameHidden("A Jev exemption for this digest is active, but policy names are hidden for this account, so the policy that grants it cannot be named.", digest, matchingExpires);
    }
    return { state: "exempted", policy: governingPolicy, expires: matchingExpires };
  }

  if (matchingState === "noDigest" || !digest) return { state: "no-digest", namespace };

  if (matchingState === "exempted") {
    // The entry is active but the Version is still blocked, so it has not been re-evaluated since the exemption was added.
    if (!governingPolicy) {
      return nameHidden("A Jev exemption for this digest is active, but policy names are hidden for this account, so the policy that grants it cannot be named.", digest, matchingExpires);
    }
    return { state: "pending", namespace, digest, policy: governingPolicy };
  }

  if (matchingState === "expired") {
    if (!governingPolicy) {
      return nameHidden("A Jev exemption for this digest expired, but policy names are hidden for this account, so the policy that held it cannot be named.", digest, matchingExpires);
    }
    return { state: "expired", digest, policy: governingPolicy, expires: matchingExpires ?? "" };
  }

  if (matchingState === "superseded") {
    if (!governingPolicy || !matchingPolicy) {
      return nameHidden(
        `A Jev exemption for this digest is in a policy that does not govern this Version, so it is ignored. Policy names are hidden for this account. The governing policy has priority ${governingPriority} and the policy with the exemption has priority ${matchingPriority}.`,
        digest,
      );
    }
    return {
      state: "superseded",
      digest,
      policy: matchingPolicy,
      winner: governingPolicy,
      policyPriority: matchingPriority,
      winnerPriority: governingPriority,
    };
  }

  // matchingState "none": no entry matches the digest. An empty governingPolicy is not proof that no policy governs the
  // Version, because the server clears the name for OIDC callers, so the policy is left unnamed rather than claimed absent.
  if (matchingState === "none") return { state: "needs-entry", digest, policy: governingPolicy || undefined };

  return nameHidden(`The server reported a matching state this UI does not recognize: ${matchingState}.`, digest);
}

// describeInferred is the fallback for responses without jevExemptionGovernance. It infers the governing policy from the
// ScanPolicy specs, which the server may evaluate differently, so it is used only when the server did not report one.
function describeInferred({
  assessment,
  namespace,
  resource,
  policies,
  now,
}: {
  assessment: JevAssessment;
  namespace: string;
  resource: GovernedResource;
  policies: ScanPolicy[] | null;
  now: Date;
}): JevUnblock | null {
  if (assessment.exempted) {
    return { state: "exempted", policy: assessment.exemptedBy ?? "an unnamed policy", expires: assessment.exemptionExpires };
  }
  if (!assessment.blocked) return null;

  const digest = assessment.contentDigest;
  if (!digest) return { state: "no-digest", namespace };
  if (!policies) {
    return {
      state: "unknown",
      digest,
      reason: "The UI cannot determine which policy governs this Version. The ScanPolicies in this namespace could not be read.",
      candidates: [],
    };
  }

  const winner = winnerFor(governingPolicies(policies, resource));
  if (winner.kind === "unknown") {
    return {
      state: "unknown",
      digest,
      reason: "The UI cannot determine which policy governs this Version. A policy that uses a selector or a version constraint may govern this Version, and the UI cannot tell which one wins.",
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
      return { state: "pending", namespace, digest, policy: winnerName };
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

// describeJevUnblock returns the unblock state for an assessment. It returns null when there is nothing to unblock.
// When the server reports governance, that decision is used. Otherwise the inferred fallback runs. policies is null when
// the ScanPolicy list could not be read, and is only read by the fallback.
export function describeJevUnblock({
  assessment,
  namespace,
  resource,
  governance: decision,
  policies,
  now = new Date(),
}: {
  assessment: JevAssessment;
  namespace: string;
  resource: GovernedResource;
  governance?: JevExemptionGovernance | null;
  policies: ScanPolicy[] | null;
  now?: Date;
}): JevUnblock | null {
  if (decision) return describeFromGovernance({ assessment, namespace, decision });
  return describeInferred({ assessment, namespace, resource, policies, now });
}
