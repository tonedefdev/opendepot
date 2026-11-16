import type {
  ScanPolicyCapabilities,
  ScanPolicyCatalog,
  ScanPolicyCatalogItem,
  ScanPolicyJevExemption,
} from "@/lib/api";

// JEV_DIGEST_PATTERN mirrors the server: exactly 64 lowercase hex characters, with no algorithm prefix.
export const JEV_DIGEST_PATTERN = /^[0-9a-f]{64}$/;

const RFC3339_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

// validateJevExemptions returns errors keyed by jev-{index}-{field}. Duplicate digests are flagged on the later entry.
export function validateJevExemptions(exemptions: ScanPolicyJevExemption[]): Record<string, string> {
  const errors: Record<string, string> = {};
  const seenDigests = new Set<string>();
  exemptions.forEach((exemption, index) => {
    if (!JEV_DIGEST_PATTERN.test(exemption.contentDigest)) {
      errors[`jev-${index}-digest`] = "Enter 64 lowercase hex characters (a SHA-256 digest without a prefix).";
    } else if (seenDigests.has(exemption.contentDigest)) {
      errors[`jev-${index}-digest`] = "This digest already has a Jev exemption in this policy.";
    } else {
      seenDigests.add(exemption.contentDigest);
    }
    if (!exemption.reason.trim()) errors[`jev-${index}-reason`] = "A reason is required for every Jev exemption.";
    if (exemption.expires && (!RFC3339_PATTERN.test(exemption.expires) || Number.isNaN(Date.parse(exemption.expires)))) {
      errors[`jev-${index}-expires`] = "Expiry must be a valid RFC3339 timestamp.";
    }
  });
  return errors;
}

// parseJevDigest accepts a route parameter only when it is a valid digest; anything else is ignored.
export function parseJevDigest(value: string | undefined): string | undefined {
  return value && JEV_DIGEST_PATTERN.test(value) ? value : undefined;
}

// jevExemptionDraft prefills a new exemption. The reason is a draft the author must review before saving.
export function jevExemptionDraft(contentDigest: string, versionName?: string): ScanPolicyJevExemption {
  return { contentDigest, reason: versionName ? `Jev exemption for version ${versionName}` : "" };
}

// canManageJevExemptions mirrors the server rule: only namespace-wide policy managers may change jevExemptions.
export function canManageJevExemptions(
  item: ScanPolicyCatalogItem | undefined,
  capabilities: ScanPolicyCapabilities | null | undefined,
): boolean {
  return Boolean(item?.canManageNamespaceWidePolicies && capabilities?.canManageNamespaceWidePolicies);
}

// canEditJevExemptions adds the write checks that gate saving, so the JevPanel action is only offered when a save can succeed.
export function canEditJevExemptions(
  catalog: ScanPolicyCatalog,
  namespace: string,
  capabilities: ScanPolicyCapabilities,
): boolean {
  const item = catalog.items.find((entry) => entry.namespace === namespace);
  return Boolean(
    catalog.writesEnabled &&
      item?.canWrite &&
      capabilities.writesEnabled &&
      capabilities.canWrite &&
      canManageJevExemptions(item, capabilities),
  );
}
