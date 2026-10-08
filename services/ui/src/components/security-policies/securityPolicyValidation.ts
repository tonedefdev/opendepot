import type { ScanPolicyMutation } from "@/lib/api";

export function validateScanPolicy(value: ScanPolicyMutation): Record<string, string> {
  const errors: Record<string, string> = {};
  if (!/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(value.metadata.name)) {
    errors.name = "Use lowercase letters, numbers, and hyphens; start and end with a letter or number.";
  }
  if (!value.metadata.namespace.trim()) errors.namespace = "Namespace is required.";
  value.spec.targetRefs?.forEach((target, index) => {
    if (!target.name.trim()) errors[`target-${index}`] = "Target resource name is required.";
  });
  value.spec.exemptions?.forEach((exemption, index) => {
    if (!exemption.reason.trim()) errors[`exemption-${index}`] = "A reason is required for every exemption.";
    if (exemption.expires && Number.isNaN(Date.parse(exemption.expires))) {
      errors[`exemption-${index}`] = "Expiry must be a valid RFC3339 timestamp.";
    }
  });
  return errors;
}
