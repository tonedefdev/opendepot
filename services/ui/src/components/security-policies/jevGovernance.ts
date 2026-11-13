import type {
  ScanPolicy,
  ScanPolicyJevExemption,
  ScanPolicyTargetKind,
  ScanPolicyTargetRef,
  ScanSeverityThreshold,
} from "@/lib/api";
import { jevExemptionDraft } from "./jevExemptions";

// Governance answers whether a ScanPolicy applies to one resource's Versions. "unknown" means the UI cannot decide
// from the data it has: selectors need Version labels and version constraints need a semver check, neither of which
// the browse API exposes. The server's Matches and Select rules are mirrored in the order they are applied.
export type Governance = "yes" | "no" | "unknown";

export interface GovernedResource {
  kind: ScanPolicyTargetKind;
  name: string;
}

export interface GoverningPolicy {
  policy: ScanPolicy;
  governance: Exclude<Governance, "no">;
}

export type Winner =
  | { kind: "none" }
  | { kind: "known"; policy: ScanPolicy }
  | { kind: "unknown"; candidates: ScanPolicy[] };

function anyOf(states: Governance[]): Governance {
  if (states.includes("yes")) return "yes";
  if (states.includes("unknown")) return "unknown";
  return "no";
}

// targetRefGovernance mirrors matchesTargetRef. A version constraint is reported as unknown, never evaluated here.
function targetRefGovernance(ref: ScanPolicyTargetRef, resource: GovernedResource): Governance {
  if (ref.kind.toLowerCase() !== resource.kind.toLowerCase()) return "no";
  const refName = ref.name.trim();
  if (refName !== "*" && refName.toLowerCase() !== resource.name.toLowerCase()) return "no";
  if (ref.versions?.trim()) return "unknown";
  return "yes";
}

// governance mirrors policy.Matches. A policy with neither a selector nor target references applies to every Version
// in its namespace. A non-nil selector with no requirements matches everything; any other selector is unknown.
export function governance(policy: ScanPolicy, resource: GovernedResource): Governance {
  const selector = policy.spec.selector;
  const refs = policy.spec.targetRefs ?? [];
  if (!selector && refs.length === 0) return "yes";

  const states: Governance[] = refs.map((ref) => targetRefGovernance(ref, resource));
  if (selector) {
    const matchesEverything = Object.keys(selector.matchLabels ?? {}).length === 0 && (selector.matchExpressions ?? []).length === 0;
    states.push(matchesEverything ? "yes" : "unknown");
  }
  return anyOf(states);
}

// compareSelectOrder mirrors policy.Select: higher priority first, then the oldest creation timestamp, then name ascending.
function compareSelectOrder(a: ScanPolicy, b: ScanPolicy): number {
  const priorityDiff = (b.spec.priority ?? 0) - (a.spec.priority ?? 0);
  if (priorityDiff !== 0) return priorityDiff;
  const createdDiff = createdAt(a) - createdAt(b);
  if (createdDiff !== 0) return createdDiff;
  return a.metadata.name < b.metadata.name ? -1 : a.metadata.name > b.metadata.name ? 1 : 0;
}

// createdAt treats a missing timestamp as the earliest possible time, like the zero metav1.Time on the server.
function createdAt(policy: ScanPolicy): number {
  const time = policy.metadata.creationTimestamp ? Date.parse(policy.metadata.creationTimestamp) : Number.NaN;
  return Number.isNaN(time) ? Number.NEGATIVE_INFINITY : time;
}

// governingPolicies returns every policy that may govern the resource, in the order the server would select them.
export function governingPolicies(policies: ScanPolicy[], resource: GovernedResource): GoverningPolicy[] {
  return policies
    .map((policy) => ({ policy, governance: governance(policy, resource) }))
    .filter((entry): entry is GoverningPolicy => entry.governance !== "no")
    .sort((a, b) => compareSelectOrder(a.policy, b.policy));
}

// winnerFor returns the policy that governs the resource. The first candidate wins only when it is certain to match;
// if it is uncertain, the winner cannot be named.
export function winnerFor(governing: GoverningPolicy[]): Winner {
  const [first] = governing;
  if (!first) return { kind: "none" };
  if (first.governance === "yes") return { kind: "known", policy: first.policy };
  return { kind: "unknown", candidates: governing.map((entry) => entry.policy) };
}

// nextJevExemptionPriority returns one more than the highest priority among policies that may govern the resource, so a
// new policy outranks all of them. It is 0 when nothing governs the resource.
export function nextJevExemptionPriority(policies: ScanPolicy[], resource: GovernedResource): number {
  const governing = governingPolicies(policies, resource);
  if (governing.length === 0) return 0;
  return Math.max(...governing.map((entry) => entry.policy.spec.priority ?? 0)) + 1;
}

export interface JevExemptionPrefill {
  exemption: ScanPolicyJevExemption;
  // targetRefs scope the new policy to the resource so a raised priority cannot reach other Versions in the namespace.
  targetRefs: ScanPolicyTargetRef[];
  // priority is null when the existing policies could not be read.
  priority: number | null;
  winner?: { name: string; priority: number; threshold?: ScanSeverityThreshold };
  uncertainPolicies: string[];
}

// jevExemptionPrefill builds the new-policy prefill. The resource is required; without it the policy would be namespace
// wide and the priority could not be computed safely.
export function jevExemptionPrefill({
  contentDigest,
  versionName,
  resource,
  policies,
}: {
  contentDigest: string;
  versionName?: string;
  resource?: GovernedResource;
  policies: ScanPolicy[] | null;
}): JevExemptionPrefill {
  const exemption = jevExemptionDraft(contentDigest, versionName);
  if (!resource) {
    return { exemption, targetRefs: [], priority: null, uncertainPolicies: [] };
  }
  const targetRefs: ScanPolicyTargetRef[] = [{ kind: resource.kind, name: resource.name }];
  if (!policies) {
    return { exemption, targetRefs, priority: null, uncertainPolicies: [] };
  }

  const governing = governingPolicies(policies, resource);
  const winner = winnerFor(governing);
  const uncertainPolicies = governing.filter((entry) => entry.governance === "unknown").map((entry) => entry.policy.metadata.name);
  const priority = nextJevExemptionPriority(policies, resource);
  if (winner.kind !== "known") {
    return { exemption, targetRefs, priority, uncertainPolicies };
  }

  return {
    exemption,
    targetRefs,
    priority,
    winner: {
      name: winner.policy.metadata.name,
      priority: winner.policy.spec.priority ?? 0,
      threshold: winner.policy.spec.severityThreshold,
    },
    uncertainPolicies,
  };
}
