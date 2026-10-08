import SecurityPoliciesSurface from "@/components/security-policies/SecurityPoliciesSurface";
import { ApiRequestError, getScanPolicyCatalog } from "@/lib/api";
import { getServerSessionToken } from "@/lib/session";
import { redirect } from "next/navigation";

export default async function SecurityPoliciesPage() {
  let catalog: Awaited<ReturnType<typeof getScanPolicyCatalog>> = { writesEnabled: false, items: [] };
  try {
    catalog = await getScanPolicyCatalog(await getServerSessionToken());
  } catch (error) {
    if (error instanceof ApiRequestError && error.status === 401) {
      redirect("/auth/login");
    }
    // The surface renders an unavailable catalog state.
  }
  return <SecurityPoliciesSurface mode="list" catalog={catalog} />;
}
