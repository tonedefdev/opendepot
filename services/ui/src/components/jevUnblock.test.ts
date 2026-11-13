import { describe, expect, it } from "vitest";
import type { JevAssessment, ScanPolicy } from "@/lib/api";
import { blockedVersionName, describeJevUnblock } from "./jevUnblock";

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

describe("describeJevUnblock", () => {
  it("returns null when the assessment is not blocked", () => {
    expect(run({ assessment: assessment({ blocked: false }) })).toBeNull();
  });

  it("reports exempted with the policy name and expiry when the server marks the exemption", () => {
    expect(run({
      assessment: assessment({ exempted: true, exemptedBy: "policy-a", exemptionExpires: "2026-02-01T00:00:00Z" }),
      policies: null,
    })).toEqual({ state: "exempted", policy: "policy-a", expires: "2026-02-01T00:00:00Z" });
  });

  it("asks for forceSync when the assessment has no digest, and carries the Version name only when unique", () => {
    const versions = [{ name: "lint-helper-1-0-0", blocked: true }];
    expect(run({ assessment: assessment({ contentDigest: undefined }), versions })).toEqual({
      state: "no-digest",
      namespace: "platform",
      versionName: "lint-helper-1-0-0",
    });
    expect(run({ assessment: assessment({ contentDigest: undefined }) })).toEqual({
      state: "no-digest",
      namespace: "platform",
      versionName: undefined,
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
    })).toEqual({ state: "pending", namespace: "platform", digest, policy: "winner", versionName: undefined });
  });
});

describe("blockedVersionName", () => {
  it("returns the name only when exactly one blocked Version is known", () => {
    expect(blockedVersionName([{ name: "a", blocked: true }])).toBe("a");
    expect(blockedVersionName([{ name: "a", blocked: true }, { name: "b", blocked: true }])).toBeUndefined();
    expect(blockedVersionName([{ name: "a", blocked: false }])).toBeUndefined();
    expect(blockedVersionName(undefined)).toBeUndefined();
  });
});
