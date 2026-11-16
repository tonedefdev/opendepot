import { describe, expect, it } from "vitest";
import { validateScanPolicy } from "./securityPolicyValidation";
import type { ScanPolicyMutation } from "@/lib/api";

const validPolicy: ScanPolicyMutation = {
  apiVersion: "opendepot.defdev.io/v1alpha1",
  kind: "ScanPolicy",
  metadata: { name: "baseline", namespace: "platform" },
  spec: {
    priority: 0,
    severityThreshold: "HIGH",
    exemptions: [{ vulnerabilityIDs: ["CVE-2026-0001"], reason: "Replacement scheduled", expires: "2099-12-31T00:00:00Z" }],
  },
};

describe("validateScanPolicy", () => {
  it("accepts a policy with a future, reasoned exemption", () => {
    expect(validateScanPolicy(validPolicy)).toEqual({});
  });

  it("requires auditable exemption reasons and future expiry dates", () => {
    const errors = validateScanPolicy({
      ...validPolicy,
      spec: {
        ...validPolicy.spec,
          targetRefs: [{ kind: "Module", name: "" }],
          exemptions: [{ reason: "" }, { reason: "Temporary", expires: "not-a-date" }],
      },
    });
    expect(errors["target-0"]).toContain("name");
    expect(errors["exemption-0"]).toContain("reason");
    expect(errors["exemption-1"]).toContain("RFC3339");
  });

  it("validates Jev exemption digests, duplicates, and reasons through the policy validator", () => {
    const errors = validateScanPolicy({
      ...validPolicy,
      spec: {
        ...validPolicy.spec,
        jevExemptions: [
          { contentDigest: "a".repeat(64), reason: "Reviewed" },
          { contentDigest: "a".repeat(64), reason: "Duplicate" },
          { contentDigest: "sha256:" + "b".repeat(64), reason: "Prefixed" },
          { contentDigest: "c".repeat(64), reason: "   " },
        ],
      },
    });
    expect(errors["jev-1-digest"]).toContain("already has a Jev exemption");
    expect(errors["jev-2-digest"]).toContain("64 lowercase hex");
    expect(errors["jev-3-reason"]).toContain("reason is required");
    expect(errors["jev-0-digest"]).toBeUndefined();
  });

  it("rejects names that cannot be addressed as Kubernetes-style resources", () => {
    expect(validateScanPolicy({ ...validPolicy, metadata: { ...validPolicy.metadata, name: "Not A Name" } }).name).toBeTruthy();
  });
});
