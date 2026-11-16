import { describe, expect, it } from "vitest";
import {
  canEditJevExemptions,
  canManageJevExemptions,
  jevExemptionDraft,
  parseJevDigest,
  validateJevExemptions,
} from "./jevExemptions";
import type { ScanPolicyCapabilities, ScanPolicyCatalog } from "@/lib/api";

const digest = "a".repeat(64);

const capabilities: ScanPolicyCapabilities = {
  writesEnabled: true,
  canRead: true,
  canWrite: true,
  canManageNamespaceWidePolicies: true,
  methods: ["GET", "POST", "PUT", "DELETE"],
  supportsPreview: true,
};

const catalog: ScanPolicyCatalog = {
  writesEnabled: true,
  items: [{
    namespace: "platform",
    canRead: true,
    canWrite: true,
    canManageNamespaceWidePolicies: true,
    modules: [],
    providers: [],
    skills: [],
    agents: [],
  }],
};

describe("jevExemptions helpers", () => {
  it("accepts only 64 lowercase hex digests", () => {
    expect(validateJevExemptions([{ contentDigest: digest, reason: "ok" }])).toEqual({});
    expect(validateJevExemptions([{ contentDigest: "A".repeat(64), reason: "ok" }])["jev-0-digest"]).toBeTruthy();
    expect(validateJevExemptions([{ contentDigest: "a".repeat(63), reason: "ok" }])["jev-0-digest"]).toBeTruthy();
  });

  it("flags an invalid expiry timestamp", () => {
    expect(validateJevExemptions([{ contentDigest: digest, reason: "ok", expires: "tomorrow" }])["jev-0-expires"]).toContain("RFC3339");
    expect(validateJevExemptions([{ contentDigest: digest, reason: "ok", expires: "2099-01-01T00:00:00Z" }])).toEqual({});
  });

  it("parses only valid route digests", () => {
    expect(parseJevDigest(digest)).toBe(digest);
    expect(parseJevDigest("sha256:" + digest)).toBeUndefined();
    expect(parseJevDigest(undefined)).toBeUndefined();
  });

  it("drafts a reason that names the version and leaves it blank otherwise", () => {
    expect(jevExemptionDraft(digest, "1.2.3")).toEqual({ contentDigest: digest, reason: "Jev exemption for version 1.2.3" });
    expect(jevExemptionDraft(digest)).toEqual({ contentDigest: digest, reason: "" });
  });

  it("allows Jev edits only for namespace-wide managers", () => {
    expect(canManageJevExemptions(catalog.items[0], capabilities)).toBe(true);
    expect(canManageJevExemptions(catalog.items[0], { ...capabilities, canManageNamespaceWidePolicies: false })).toBe(false);
    expect(canManageJevExemptions(undefined, capabilities)).toBe(false);
  });

  it("gates the JevPanel action on writable, namespace-wide access", () => {
    expect(canEditJevExemptions(catalog, "platform", capabilities)).toBe(true);
    expect(canEditJevExemptions(catalog, "platform", { ...capabilities, canWrite: false })).toBe(false);
    expect(canEditJevExemptions({ ...catalog, writesEnabled: false }, "platform", capabilities)).toBe(false);
    expect(canEditJevExemptions(catalog, "other", capabilities)).toBe(false);
    expect(canEditJevExemptions(catalog, "platform", { ...capabilities, canManageNamespaceWidePolicies: false })).toBe(false);
  });
});
