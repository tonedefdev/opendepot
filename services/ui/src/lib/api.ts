/**
 * Server-side API client for the OpenDepot browse endpoints.
 *
 * Called from Next.js Server Components and Route Handlers.  The base URL
 * points to the OpenDepot server service and is never exposed to the browser.
 */

const BASE_URL = (process.env.OPENDEPOT_SERVER_URL ?? "").replace(/\/$/, "");

export interface BrowseScanCounts {
  critical: number;
  high: number;
  medium: number;
  low: number;
  unknown: number;
}

export interface BrowseResource {
  kind: string;
  namespace: string;
  name: string;
  latestVersion: string;
  syncStatus: string;
  synced: boolean;
  public: boolean;
  provider: string;
  repoUrl: string;
  providerNamespace: string;
  platforms: Array<{ os: string; arch: string }>;
  scanCounts: BrowseScanCounts | null;
  lastScanned: string;
}

export interface BrowseResourceList {
  items: BrowseResource[];
  totalCount: number;
  page: number;
  pageSize: number;
}

export interface BrowseNamespace {
  name: string;
  public: boolean;
}

export interface BrowseNamespaceList {
  items: BrowseNamespace[];
}

export interface BrowseVersionSummary {
  version: string;
  syncStatus: string;
  os: string;
  arch: string;
  lastScanned: string;
  synced: boolean;
  scanCounts: BrowseScanCounts | null;
}

export interface SecurityFinding {
  id: string;
  severity: string;
  title: string;
  message: string;
  resolution: string;
}

export interface BrowseResourceDetail extends BrowseResource {
  versions: BrowseVersionSummary[];
  sourceScanFindings: SecurityFinding[];
  binaryScanFindings: Record<string, SecurityFinding[]>;
}

export interface ListResourcesParams {
  namespace?: string;
  kind?: string;
  q?: string;
  synced?: boolean;
  os?: string;
  arch?: string;
  severity?: string;
  publicOnly?: boolean;
  sortBy?: string;
  sortDir?: "asc" | "desc";
  page?: number;
  pageSize?: number;
}

async function apiFetch<T>(
  path: string,
  token?: string,
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE_URL}${path}`, { headers, cache: "no-store" });
  if (!res.ok) {
    throw new Error(`API request failed: ${res.status} ${res.statusText}`);
  }
  return res.json() as Promise<T>;
}

export async function listNamespaces(token?: string): Promise<BrowseNamespaceList> {
  return apiFetch<BrowseNamespaceList>("/opendepot/ui/v1/namespaces", token);
}

export async function listResources(
  params: ListResourcesParams,
  token?: string,
): Promise<BrowseResourceList> {
  const qs = new URLSearchParams();
  if (params.namespace) qs.set("namespace", params.namespace);
  if (params.kind) qs.set("kind", params.kind);
  if (params.q) qs.set("q", params.q);
  if (params.synced !== undefined) qs.set("synced", String(params.synced));
  if (params.os) qs.set("os", params.os);
  if (params.arch) qs.set("arch", params.arch);
  if (params.severity) qs.set("severity", params.severity);
  if (params.publicOnly !== undefined) qs.set("public_only", String(params.publicOnly));
  if (params.sortBy) qs.set("sort_by", params.sortBy);
  if (params.sortDir) qs.set("sort_dir", params.sortDir);
  if (params.page !== undefined) qs.set("page", String(params.page));
  if (params.pageSize !== undefined) qs.set("page_size", String(params.pageSize));

  const query = qs.toString();
  return apiFetch<BrowseResourceList>(
    `/opendepot/ui/v1/resources${query ? `?${query}` : ""}`,
    token,
  );
}

export async function getResourceDetail(
  namespace: string,
  kind: string,
  name: string,
  token?: string,
): Promise<BrowseResourceDetail> {
  return apiFetch<BrowseResourceDetail>(
    `/opendepot/ui/v1/resources/${encodeURIComponent(namespace)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}`,
    token,
  );
}
