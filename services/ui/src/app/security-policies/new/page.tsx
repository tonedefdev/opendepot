import SecurityPoliciesSurface from "@/components/security-policies/SecurityPoliciesSurface";
import { parseJevDigest } from "@/components/security-policies/jevExemptions";
import { jevExemptionPrefill, type GovernedResource, type JevExemptionPrefill } from "@/components/security-policies/jevGovernance";
import { getScanPolicyCatalog, listScanPolicies, type ScanPolicy } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";

// jevResource scopes the prefilled policy to the blocked Version's resource. Only Skill and Agent can carry Jev exemptions.
function jevResource(kind?: string, name?: string): GovernedResource | undefined {
  if (!name) return undefined;
  if (kind === "Skill" || kind === "Agent") return { kind, name };
  return undefined;
}

export default async function NewSecurityPolicyPage({
  searchParams,
}: {
  searchParams: Promise<{ namespace?: string; jevDigest?: string; jevVersion?: string; jevKind?: string; jevName?: string; jevGoverningHidden?: string }>;
}) {
  const { namespace, jevDigest, jevVersion, jevKind, jevName, jevGoverningHidden } = await searchParams;
  const contentDigest = parseJevDigest(jevDigest);
  const token = await getServerSessionToken().catch(() => undefined);
  let catalog: Awaited<ReturnType<typeof getScanPolicyCatalog>> = { writesEnabled: false, items: [] };
  try {
    catalog = await getScanPolicyCatalog(token);
  } catch {
    // The surface renders an unavailable catalog state.
  }
  let jevPrefill: JevExemptionPrefill | undefined;
  if (contentDigest) {
    // The existing policies set the priority. If they cannot be read, the prefill reports a null priority instead of guessing.
    let policies: ScanPolicy[] | null = null;
    if (namespace) {
      try {
        policies = (await listScanPolicies(namespace, token)).items ?? [];
      } catch {
        // Leave policies null so the surface warns that the priority is unverified.
      }
    }
    jevPrefill = jevExemptionPrefill({
      contentDigest,
      versionName: jevVersion,
      resource: jevResource(jevKind, jevName),
      policies,
      // The Version's browse data withheld the governing policy name, so the prefill must not say there is none.
      governingNameHidden: jevGoverningHidden === "true",
    });
  }
  return <SecurityPoliciesSurface mode="create" namespace={namespace} catalog={catalog} jevPrefill={jevPrefill} />;
}
