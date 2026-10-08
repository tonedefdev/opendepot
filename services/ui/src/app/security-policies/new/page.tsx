import SecurityPoliciesSurface from "@/components/security-policies/SecurityPoliciesSurface";
import { getScanPolicyCatalog } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";

export default async function NewSecurityPolicyPage({
  searchParams,
}: {
  searchParams: Promise<{ namespace?: string }>;
}) {
  const { namespace } = await searchParams;
  let catalog: Awaited<ReturnType<typeof getScanPolicyCatalog>> = { writesEnabled: false, items: [] };
  try {
    catalog = await getScanPolicyCatalog(await getServerSessionToken());
  } catch {
    // The surface renders an unavailable catalog state.
  }
  return <SecurityPoliciesSurface mode="create" namespace={namespace} catalog={catalog} />;
}
