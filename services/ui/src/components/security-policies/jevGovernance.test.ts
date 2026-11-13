import { describe, expect, it } from "vitest";
import type { ScanPolicy, ScanPolicyTargetKind } from "@/lib/api";
import {
  governance,
  governingPolicies,
  jevExemptionPrefill,
  nextJevExemptionPriority,
  winnerFor,
} from "./jevGovernance";

const digest = "b".repeat(64);
const skill = { kind: "Skill" as const, name: "lint-helper" };

function policy(name: string, overrides: Partial<ScanPolicy["spec"]> = {}, creationTimestamp?: string): ScanPolicy {
  return {
    apiVersion: "opendepot.defdev.io/v1alpha1",
    kind: "ScanPolicy",
    metadata: { name, namespace: "platform", creationTimestamp },
    spec: { priority: 0, ...overrides },
  };
}

const scoped = (name: string, priority: number, creationTimestamp?: string) =>
  policy(name, { priority, targetRefs: [{ kind: "Skill", name: "lint-helper" }] }, creationTimestamp);

describe("governance", () => {
  it("applies a policy with neither selector nor targetRefs to every Version in its namespace", () => {
    expect(governance(policy("ns-wide"), skill)).toBe("yes");
  });

  it("matches a targetRef by kind case-insensitively and name exactly, with * as a wildcard", () => {
    expect(governance(policy("p", { targetRefs: [{ kind: "skill" as ScanPolicyTargetKind, name: "LINT-HELPER" }] }), skill)).toBe("yes");
    expect(governance(policy("p", { targetRefs: [{ kind: "Skill", name: "*" }] }), skill)).toBe("yes");
    expect(governance(policy("p", { targetRefs: [{ kind: "Skill", name: "other" }] }), skill)).toBe("no");
    expect(governance(policy("p", { targetRefs: [{ kind: "Agent", name: "lint-helper" }] }), skill)).toBe("no");
  });

  it("reports a version constraint as unknown rather than evaluating it", () => {
    expect(governance(policy("p", { targetRefs: [{ kind: "Skill", name: "lint-helper", versions: ">=1.0.0" }] }), skill)).toBe("unknown");
  });

  it("treats a selector with requirements as unknown and an empty selector as matching everything", () => {
    expect(governance(policy("p", { selector: { matchLabels: { env: "dev" } } }), skill)).toBe("unknown");
    expect(governance(policy("p", { selector: {} }), skill)).toBe("yes");
  });

  it("lets a certain match win over an uncertain one in the same policy", () => {
    const mixed = policy("p", {
      selector: { matchLabels: { env: "dev" } },
      targetRefs: [{ kind: "Skill", name: "lint-helper" }],
    });
    expect(governance(mixed, skill)).toBe("yes");
  });
});

describe("governingPolicies and winnerFor", () => {
  it("orders by priority descending, then oldest creationTimestamp, then name", () => {
    const ordered = governingPolicies(
      [
        scoped("b-low", 1, "2024-01-01T00:00:00Z"),
        scoped("z-tie-new", 5, "2024-06-01T00:00:00Z"),
        scoped("a-tie-old", 5, "2024-01-01T00:00:00Z"),
        scoped("a-tie-same", 5, "2024-06-01T00:00:00Z"),
        scoped("m-tie-same", 5, "2024-06-01T00:00:00Z"),
      ],
      skill,
    ).map((entry) => entry.policy.metadata.name);
    expect(ordered).toEqual(["a-tie-old", "a-tie-same", "m-tie-same", "z-tie-new", "b-low"]);
  });

  it("drops policies that do not govern the resource", () => {
    const other = policy("other", { priority: 9, targetRefs: [{ kind: "Agent", name: "lint-helper" }] });
    expect(governingPolicies([other], skill)).toEqual([]);
    expect(winnerFor([])).toEqual({ kind: "none" });
  });

  it("names the winner when its match is certain and reports candidates when it is not", () => {
    const certain = governingPolicies([scoped("win", 3)], skill);
    expect(winnerFor(certain)).toEqual({ kind: "known", policy: certain[0].policy });

    const uncertain = governingPolicies([policy("sel", { priority: 9, selector: { matchLabels: { a: "b" } } })], skill);
    expect(winnerFor(uncertain).kind).toBe("unknown");
  });
});

describe("nextJevExemptionPriority", () => {
  it("returns 0 when no policy governs the resource", () => {
    expect(nextJevExemptionPriority([policy("other", { targetRefs: [{ kind: "Module", name: "x" }] })], skill)).toBe(0);
    expect(nextJevExemptionPriority([], skill)).toBe(0);
  });

  it("returns one more than the highest governing priority, ignoring non-governing policies", () => {
    const policies = [
      scoped("low", 4),
      scoped("high", 101),
      policy("elsewhere", { priority: 500, targetRefs: [{ kind: "Module", name: "terraform-aws-acm" }] }),
    ];
    expect(nextJevExemptionPriority(policies, skill)).toBe(102);
  });

  it("counts a policy with an uncertain match, so the new policy is not superseded by it", () => {
    expect(nextJevExemptionPriority([policy("sel", { priority: 7, selector: { matchLabels: { a: "b" } } })], skill)).toBe(8);
  });

  it("treats a missing priority as 0", () => {
    expect(nextJevExemptionPriority([policy("none", { priority: undefined, targetRefs: [{ kind: "Skill", name: "lint-helper" }] })], skill)).toBe(1);
  });
});

describe("jevExemptionPrefill", () => {
  it("scopes the policy to the resource and sets the priority above existing governing policies", () => {
    const prefill = jevExemptionPrefill({
      contentDigest: digest,
      versionName: "lint-helper-1-0-0",
      resource: skill,
      policies: [scoped("current", 2)],
    });
    expect(prefill.targetRefs).toEqual([{ kind: "Skill", name: "lint-helper" }]);
    expect(prefill.priority).toBe(3);
    expect(prefill.winner).toEqual({ name: "current", priority: 2, threshold: undefined });
    expect(prefill.uncertainPolicies).toEqual([]);
    expect(prefill.exemption).toMatchObject({ contentDigest: digest });
  });

  it("returns a null priority when the policies could not be read", () => {
    const prefill = jevExemptionPrefill({ contentDigest: digest, resource: skill, policies: null });
    expect(prefill.priority).toBeNull();
    expect(prefill.targetRefs).toHaveLength(1);
  });

  it("returns no targets and a null priority when no resource is known", () => {
    const prefill = jevExemptionPrefill({ contentDigest: digest, policies: [scoped("a", 1)] });
    expect(prefill.targetRefs).toEqual([]);
    expect(prefill.priority).toBeNull();
  });

  it("lists uncertain policies and omits a winner when the winner cannot be named", () => {
    const prefill = jevExemptionPrefill({
      contentDigest: digest,
      resource: skill,
      policies: [policy("sel", { priority: 6, selector: { matchLabels: { a: "b" } } })],
    });
    expect(prefill.winner).toBeUndefined();
    expect(prefill.uncertainPolicies).toEqual(["sel"]);
    expect(prefill.priority).toBe(7);
  });
});
