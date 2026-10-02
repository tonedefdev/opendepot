import SecurityPoliciesSurface from "@/components/security-policies/SecurityPoliciesSurface";
import { getScanPolicyCatalog } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";

export default async function SecurityPoliciesPage() {
  let catalog: Awaited<ReturnType<typeof getScanPolicyCatalog>> = { writesEnabled: false, items: [] };
  try {
    catalog = await getScanPolicyCatalog(await getServerSessionToken());
  } catch {
    // The surface renders an unavailable catalog state.
  }
  return <SecurityPoliciesSurface mode="list" catalog={catalog} />;
}
