import { describe, expect, it } from "vitest";
import type { JevAssessment, JevExemptionGovernance, ScanPolicy } from "@/lib/api";
import { describeJevUnblock } from "./jevUnblock";

const digest = "c".repeat(64);
const now = new Date("2026-01-15T00:00:00Z");
const resource = { kind: "Skill" as const, name: "lint-helper" };

function assessment(overrides: Partial<JevAssessment> = {}): JevAssessment {
  return { evaluatedAt: "2026-01-01T00:00:00Z", needsReview: true, blocked: true, contentDigest: digest, ...overrides };
}

function policy(name: string, priority: number, jevDigest?: string, expires?: string, scoped = true): ScanPolicy {
  return {
    apiVersion: "opendepot.defdev.io/v1alpha1",
    kind: "ScanPolicy",
    metadata: { name, namespace: "platform" },
    spec: {
      priority,
      ...(scoped ? { targetRefs: [{ kind: "Skill", name: "lint-helper" }] } : {}),
      ...(jevDigest ? { jevExemptions: [{ contentDigest: jevDigest, reason: "reviewed", expires }] } : {}),
    },
  };
}

function run(overrides: Partial<Parameters<typeof describeJevUnblock>[0]> = {}) {
  return describeJevUnblock({
    assessment: assessment(),
    namespace: "platform",
    resource,
    policies: [],
    now,
    ...overrides,
  });
}

function governance(overrides: Partial<JevExemptionGovernance> = {}): JevExemptionGovernance {
  return {
    governingPolicy: "",
    governingPriority: 0,
    matchingPolicy: "",
    matchingPriority: 0,
    matchingState: "none",
    superseded: false,
    ...overrides,
  };
}

describe("describeJevUnblock with server governance", () => {
  it("returns null when the assessment is not blocked and nothing is exempted", () => {
    expect(run({ assessment: assessment({ blocked: false }), governance: governance() })).toBeNull();
  });

  it("reports exempted from the server's governing policy and expiry when the Version is not blocked", () => {
    expect(run({
      assessment: assessment({ blocked: false }),
      governance: governance({ governingPolicy: "policy-a", matchingPolicy: "policy-a", matchingState: "exempted", matchingExpires: "2026-02-01T00:00:00Z" }),
      policies: null,
    })).toEqual({ state: "exempted", policy: "policy-a", expires: "2026-02-01T00:00:00Z" });
  });

  it("ignores the ScanPolicy inference when governance is reported", () => {
    // The shadowed policy would infer superseded, but the server says the entry is active and governed by "winner".
    expect(run({
      policies: [policy("winner", 9), policy("shadowed", 2, digest)],
      governance: governance({ governingPolicy: "winner", governingPriority: 9, matchingState: "none" }),
    })).toEqual({ state: "needs-entry", digest, policy: "winner" });
  });

  it("asks for forceSync when governance is reported and the assessment has no digest", () => {
    expect(run({
      assessment: assessment({ contentDigest: undefined }),
      governance: governance({ matchingState: "noDigest" }),
    })).toEqual({ state: "no-digest", namespace: "platform" });
  });

  it("maps noDigest to the no-digest state even when a digest is present", () => {
    expect(run({ governance: governance({ matchingState: "noDigest" }) })).toEqual({ state: "no-digest", namespace: "platform" });
  });

  it("maps none to needs-entry with the governing policy name", () => {
    expect(run({ governance: governance({ governingPolicy: "current", governingPriority: 3 }) }))
      .toEqual({ state: "needs-entry", digest, policy: "current" });
  });

  it("maps none with no governing name to needs-entry without a policy name", () => {
    expect(run({ governance: governance() })).toEqual({ state: "needs-entry", digest, policy: undefined });
  });

  it("maps exempted while blocked to pending with the governing policy", () => {
    expect(run({
      governance: governance({ governingPolicy: "winner", matchingPolicy: "winner", matchingState: "exempted", matchingExpires: "2026-02-01T00:00:00Z" }),
    })).toEqual({ state: "pending", namespace: "platform", digest, policy: "winner" });
  });

  it("maps expired to expired with the expiry from governance", () => {
    expect(run({
      governance: governance({ governingPolicy: "winner", matchingPolicy: "winner", matchingState: "expired", matchingExpires: "2026-01-01T00:00:00Z" }),
    })).toEqual({ state: "expired", digest, policy: "winner", expires: "2026-01-01T00:00:00Z" });
  });

  it("maps superseded to superseded with the matching policy and both priorities", () => {
    expect(run({
      governance: governance({
        governingPolicy: "winner",
        governingPriority: 9,
        matchingPolicy: "shadowed",
        matchingPriority: 2,
        matchingState: "superseded",
        superseded: true,
      }),
    })).toEqual({
      state: "superseded",
      digest,
      policy: "shadowed",
      winner: "winner",
      policyPriority: 2,
      winnerPriority: 9,
    });
  });

  describe("with OIDC redaction (empty policy names)", () => {
    it("reports exempted without naming the policy when the Version is not blocked", () => {
      expect(run({
        assessment: assessment({ blocked: false }),
        governance: governance({ matchingState: "exempted", matchingExpires: "2026-02-01T00:00:00Z", governingPriority: 4 }),
      })).toEqual({
        state: "unknown",
        digest,
        reason: "A Jev exemption for this digest is active, but policy names are hidden for this account, so the policy that grants it cannot be named.",
        candidates: [],
        expires: "2026-02-01T00:00:00Z",
      });
    });

    it("reports unknown with the active exemption when the Version is still blocked and the name is hidden", () => {
      expect(run({ governance: governance({ matchingState: "exempted" }) })).toMatchObject({
        state: "unknown",
        digest,
        candidates: [],
      });
    });

    it("reports unknown with the expiry when an expired entry's policy name is hidden", () => {
      expect(run({
        governance: governance({ matchingState: "expired", matchingExpires: "2026-01-01T00:00:00Z" }),
      })).toMatchObject({ state: "unknown", digest, expires: "2026-01-01T00:00:00Z" });
    });

    it("reports superseded with both priorities when the names are hidden", () => {
      const result = run({
        governance: governance({ governingPriority: 9, matchingPriority: 2, matchingState: "superseded", superseded: true }),
      });
      expect(result).toMatchObject({ state: "unknown", digest, candidates: [] });
      expect(result).toMatchObject({ reason: expect.stringContaining("priority 9") });
      expect(result).toMatchObject({ reason: expect.stringContaining("priority 2") });
    });

    it("keeps needs-entry without a name, so the UI does not claim no policy governs the Version", () => {
      expect(run({ governance: governance({ matchingState: "none" }) })).toEqual({ state: "needs-entry", digest, policy: undefined });
    });
  });
});

describe("describeJevUnblock inferred fallback", () => {
  it("returns null when the assessment is not blocked", () => {
    expect(run({ assessment: assessment({ blocked: false }) })).toBeNull();
  });

  it("reports exempted with the policy name and expiry when the server marks the exemption", () => {
    expect(run({
      assessment: assessment({ exempted: true, exemptedBy: "policy-a", exemptionExpires: "2026-02-01T00:00:00Z" }),
      policies: null,
    })).toEqual({ state: "exempted", policy: "policy-a", expires: "2026-02-01T00:00:00Z" });
  });

  it("asks for forceSync when the assessment has no digest", () => {
    expect(run({ assessment: assessment({ contentDigest: undefined }) })).toEqual({
      state: "no-digest",
      namespace: "platform",
    });
  });

  it("reports unknown when the policy list could not be read", () => {
    expect(run({ policies: null })).toMatchObject({ state: "unknown", digest, candidates: [] });
  });

  it("reports unknown and lists candidates when the winner cannot be named", () => {
    const uncertain: ScanPolicy = {
      apiVersion: "opendepot.defdev.io/v1alpha1",
      kind: "ScanPolicy",
      metadata: { name: "by-label", namespace: "platform" },
      spec: { priority: 5, selector: { matchLabels: { env: "dev" } } },
    };
    expect(run({ policies: [uncertain] })).toMatchObject({ state: "unknown", candidates: ["by-label"] });
  });

  it("asks for an entry in the governing policy when none matches the digest", () => {
    expect(run({ policies: [policy("current", 3)] })).toEqual({ state: "needs-entry", digest, policy: "current" });
  });

  it("asks for an entry with no policy named when nothing governs the Version", () => {
    const elsewhere: ScanPolicy = {
      apiVersion: "opendepot.defdev.io/v1alpha1",
      kind: "ScanPolicy",
      metadata: { name: "elsewhere", namespace: "platform" },
      spec: { priority: 3, targetRefs: [{ kind: "Agent", name: "other" }] },
    };
    expect(run({ policies: [elsewhere] })).toEqual({ state: "needs-entry", digest });
  });

  it("names the winner and the shadowed policy when the matching entry is ignored for priority", () => {
    expect(run({
      policies: [policy("winner", 9), policy("shadowed", 2, digest)],
    })).toEqual({
      state: "superseded",
      digest,
      policy: "shadowed",
      winner: "winner",
      policyPriority: 2,
      winnerPriority: 9,
    });
  });

  it("reports expired with the expiry when the winner's matching entry has lapsed", () => {
    expect(run({
      policies: [policy("winner", 9, digest, "2026-01-01T00:00:00Z")],
    })).toEqual({ state: "expired", digest, policy: "winner", expires: "2026-01-01T00:00:00Z" });
  });

  it("treats an entry without expires as active, so the Version is pending re-evaluation", () => {
    expect(run({
      policies: [policy("winner", 9, digest)],
    })).toEqual({ state: "pending", namespace: "platform", digest, policy: "winner" });
  });
});
