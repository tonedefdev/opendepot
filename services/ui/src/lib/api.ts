/**
 * Server-side API client for the OpenDepot browse endpoints.
 *
 * Called from Next.js Server Components and Route Handlers.  The base URL
 * points to the OpenDepot server service and is never exposed to the browser.
 */

const serverHost = process.env.OPENDEPOT_SERVER_HOST ?? "localhost:80";
const BASE_URL = (
  process.env.OPENDEPOT_SERVER_URL ?? `http://${serverHost}`
).replace(/\/$/, "");

export interface BrowseScanCounts {
  critical: number;
  high: number;
  medium: number;
  low: number;
  unknown: number;
  exempted: number;
}

export interface BrowseResource {
  kind: string;
  namespace: string;
  name: string;
  latestVersion: string;
  syncStatus: string;
  synced: boolean;
  hasUnsyncedVersions?: boolean;
  public: boolean;
  provider: string;
  repoUrl: string;
  providerNamespace: string;
  upstreamRegistry: string;
  platforms: Array<{ os: string; arch: string }>;
  scanCounts: BrowseScanCounts | null;
  lastScanned: string;
  totalDownloads?: number;
  lastDownloadedAt?: string;
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
  name?: string;
  version: string;
  syncStatus: string;
  os: string;
  arch: string;
  lastScanned: string;
  synced: boolean;
  scanCounts: BrowseScanCounts | null;
  fileName?: string;
  checksum?: string;
  downloadCount?: number;
  lastDownloadedAt?: string;
  archiveSizeBytes?: number;
  schemaState?: string;
  schemaMessage?: string;
}

export interface SecurityFinding {
  id: string;
  severity: string;
  title: string;
  message: string;
  resolution: string;
  exempted?: boolean;
  exemptionReason?: string;
  exemptedBy?: string;
  vulnerabilityID?: string;
  pkgName?: string;
  installedVersion?: string;
  fixedVersion?: string;
  fileName?: string;
  checksum?: string;
}

export interface BrowseScanFindings {
  sourceScanFindings?: SecurityFinding[];
  binaryScanFindings?: Record<string, SecurityFinding[]>;
  selectedVersion?: string;
  scannedVersions?: string[];
  binaryVersions?: string[];
}

export interface BrowseStorageConfig {
  backend: string;
  bucket?: string;
  region?: string;
  key?: string;
  directoryPath?: string;
  accountName?: string;
  accountUrl?: string;
  subscriptionID?: string;
  resourceGroup?: string;
  presignEnabled?: boolean;
  presignTTL?: string;
}

export interface BrowseGithubConfig {
  useAuthenticatedClient: boolean;
}

export interface BrowseDepotRef {
  namespace: string;
  name: string;
}

export interface AgentMetadata {
  name?: string;
  description?: string;
  tools?: string[];
  model?: string;
}

export type JevRiskLevel = "Minimal" | "Low" | "Moderate" | "High" | "Critical";

export interface JevAssessment {
  evaluatedAt: string;
  model?: string;
  safeProbability?: number;
  injectionProbability?: number;
  exfiltrationProbability?: number;
  destructiveProbability?: number;
  hiddenInstructionsProbability?: number;
  scopeMismatchProbability?: number;
  remoteExecutionProbability?: number;
  riskScore?: number;
  riskLevel?: JevRiskLevel;
  riskConfidence?: number;
  needsReview: boolean;
  blocked: boolean;
  blockReasons?: string[];
  error?: string;
}

// Thresholds from the governing ScanPolicy's jevPolicy. Each field is only
// present when the operator configured it, so the UI shows configured limits only.
export interface JevThresholds {
  minSafeProbability?: number;
  maxInjectionProbability?: number;
  maxExfiltrationProbability?: number;
  maxDestructiveProbability?: number;
  maxHiddenInstructionsProbability?: number;
  maxScopeMismatchProbability?: number;
  maxRemoteExecutionProbability?: number;
  maxRiskScore?: number;
  minConfidence?: number;
}

export interface BrowseResourceDetail extends BrowseResource {
  versions: BrowseVersionSummary[];
  sourceScanFindings: SecurityFinding[];
  binaryScanFindings: Record<string, SecurityFinding[]>;
  storageConfig?: BrowseStorageConfig;
  githubConfig?: BrowseGithubConfig;
  depotRef?: BrowseDepotRef;
  repoOwner?: string;
  versionHistoryLimit?: number;
  versionConstraints?: string;
  sourceRepository?: string;
  readmeContent?: string;
  agentMetadata?: AgentMetadata;
  jevAssessment?: JevAssessment;
  jevThresholds?: JevThresholds;
}

export interface BrowseDepot {
  namespace: string;
  name: string;
  modules: string[];
  providers: string[];
  pollingIntervalMinutes?: number;
  storageBackend: string;
}

export interface BrowseDepotList {
  items: BrowseDepot[];
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

export class ApiRequestError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly body?: unknown,
  ) {
    super(message);
    this.name = "ApiRequestError";
  }
}

async function apiFetch<T>(
  path: string,
  token?: string,
  init: RequestInit = {},
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
    ...(init.headers as Record<string, string> | undefined),
  };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE_URL}${path}`, { ...init, headers, cache: "no-store" });
  if (!res.ok) {
    let body: unknown;
    try {
      body = await res.json();
    } catch {
      body = undefined;
    }
    throw new ApiRequestError(`API request failed: ${res.status} ${res.statusText}`, res.status, body);
  }
  if (res.status === 204) return undefined as T;
  return res.json() as Promise<T>;
}

// ── Security policy contract ──────────────────────────────────────────────

export type ScanSeverityThreshold = "CRITICAL" | "HIGH" | "MEDIUM" | "LOW" | "NONE";
export type ScanType = "binary" | "source" | "module" | "agent";
export type ScanPolicyTargetKind = "Module" | "Provider" | "Skill" | "Agent";

export interface ScanPolicyMetadata {
  name: string;
  namespace: string;
  resourceVersion?: string;
  creationTimestamp?: string;
  generation?: number;
}

export interface ScanPolicyTargetRef {
  kind: ScanPolicyTargetKind;
  name: string;
  versions?: string;
}

export interface ScanPolicyMatchExpression {
  key: string;
  operator: "In" | "NotIn" | "Exists" | "DoesNotExist";
  values?: string[];
}

export interface ScanPolicySelector {
  matchLabels?: Record<string, string>;
  matchExpressions?: ScanPolicyMatchExpression[];
}

export interface ScanPolicyExemption {
  vulnerabilityIDs?: string[];
  pkgNames?: string[];
  scanTypes?: ScanType[];
  severities?: string[];
  reason: string;
  expires?: string;
}

export interface ScanPolicySpec {
  priority?: number;
  selector?: ScanPolicySelector;
  targetRefs?: ScanPolicyTargetRef[];
  severityThreshold?: ScanSeverityThreshold;
  exemptions?: ScanPolicyExemption[];
}

export interface ScanPolicyStatus {
  matchedVersions?: number;
  activeExemptions?: number;
  expiredExemptions?: number;
  supersededBy?: string;
  conditions?: Array<{ type: string; status: string; reason?: string; message?: string }>;
}

export interface ScanPolicy {
  apiVersion: "opendepot.defdev.io/v1alpha1";
  kind: "ScanPolicy";
  metadata: ScanPolicyMetadata;
  spec: ScanPolicySpec;
  status?: ScanPolicyStatus;
}

export interface ScanPolicyList {
  apiVersion?: string;
  kind?: string;
  metadata?: { resourceVersion?: string; continue?: string };
  items: ScanPolicy[];
}

export interface ScanPolicyMutation {
  apiVersion: "opendepot.defdev.io/v1alpha1";
  kind: "ScanPolicy";
  metadata: Pick<ScanPolicyMetadata, "name" | "namespace"> & { resourceVersion?: string };
  spec: ScanPolicySpec;
}

export interface ScanPolicyCapabilities {
  writesEnabled: boolean;
  canRead: boolean;
  canWrite: boolean;
  canManageNamespaceWidePolicies: boolean;
  methods: string[];
  supportsPreview: boolean;
}

export interface ScanPolicyCatalogResource {
  name: string;
}

export interface ScanPolicyCatalogItem {
  namespace: string;
  canRead: boolean;
  canWrite: boolean;
  canManageNamespaceWidePolicies: boolean;
  modules: ScanPolicyCatalogResource[];
  providers: ScanPolicyCatalogResource[];
  skills: ScanPolicyCatalogResource[];
  agents: ScanPolicyCatalogResource[];
}

export interface ScanPolicyCatalog {
  writesEnabled: boolean;
  items: ScanPolicyCatalogItem[];
}

export interface ScanPolicyPreview {
  valid: boolean;
  policy: ScanPolicy;
}

function scanPolicyPath(namespace: string, name?: string): string {
  const base = `/opendepot/ui/v1/scan-policies/${encodeURIComponent(namespace)}`;
  return name ? `${base}/${encodeURIComponent(name)}` : base;
}

export async function listScanPolicies(namespace: string, token?: string): Promise<ScanPolicyList> {
  return apiFetch<ScanPolicyList>(scanPolicyPath(namespace), token);
}

export async function getScanPolicy(namespace: string, name: string, token?: string): Promise<ScanPolicy> {
  return apiFetch<ScanPolicy>(
    scanPolicyPath(namespace, name),
    token,
  );
}

export async function getScanPolicyCapabilities(namespace: string, token?: string): Promise<ScanPolicyCapabilities> {
  return apiFetch<ScanPolicyCapabilities>(`${scanPolicyPath(namespace)}/capabilities`, token);
}

export async function getScanPolicyCatalog(token?: string): Promise<ScanPolicyCatalog> {
  return apiFetch<ScanPolicyCatalog>("/opendepot/ui/v1/scan-policies/catalog", token);
}

export async function createScanPolicy(namespace: string, mutation: ScanPolicyMutation, token?: string): Promise<ScanPolicy> {
  return apiFetch<ScanPolicy>(scanPolicyPath(namespace), token, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(mutation),
  });
}

export async function updateScanPolicy(
  namespace: string,
  name: string,
  mutation: ScanPolicyMutation,
  resourceVersion?: string,
  token?: string,
): Promise<ScanPolicy> {
  return apiFetch<ScanPolicy>(
    scanPolicyPath(namespace, name),
    token,
    {
      method: "PUT",
      headers: { "Content-Type": "application/json", ...(resourceVersion ? { "If-Match": resourceVersion } : {}) },
      body: JSON.stringify({
        ...mutation,
        metadata: { ...mutation.metadata, ...(resourceVersion ? { resourceVersion } : {}) },
      }),
    },
  );
}

export async function deleteScanPolicy(
  namespace: string,
  name: string,
  resourceVersion?: string,
  token?: string,
): Promise<void> {
  await apiFetch<void>(
    scanPolicyPath(namespace, name),
    token,
    { method: "DELETE", headers: resourceVersion ? { "If-Match": resourceVersion } : undefined },
  );
}

export async function previewScanPolicy(
  namespace: string,
  mutation: ScanPolicyMutation,
  token?: string,
): Promise<ScanPolicyPreview> {
  return apiFetch<ScanPolicyPreview>(`${scanPolicyPath(namespace)}/preview`, token, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(mutation),
  });
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

export async function listDepots(token?: string): Promise<BrowseDepotList> {
  return apiFetch<BrowseDepotList>("/opendepot/ui/v1/depots", token);
}

// ── Assembly Line contract types ───────────────────────────────────────────

/**
 * A cty type constraint encoded the same way `tofu providers schema -json`
 * encodes it: either a primitive name ("string", "number", "bool", "dynamic")
 * or a nested tuple such as ["list", "string"] or ["object", { ... }].
 */
export type CtyType = string | unknown[];

export interface ContractValidation {
  condition?: string;
  errorMessage?: string;
}

export interface ContractVariable {
  name: string;
  type: CtyType;
  optionalAttributes?: Record<string, boolean>;
  optionalAttributePaths?: string[];
  required: boolean;
  sensitive?: boolean;
  description?: string;
  default?: unknown;
  validations?: ContractValidation[];
}

export interface ContractOutput {
  name: string;
  type: CtyType;
  confidence: "exact" | "inferred" | "unknown";
  sensitive?: boolean;
  description?: string;
  reason?: string;
}

export interface ContractRequiredProvider {
  localName: string;
  source: string;
  versionConstraint?: string;
}

export interface ContractCompatibility {
  grade: "full" | "partial" | "unsupported";
  warnings?: string[];
}

export interface ContractProvenanceSchema {
  provider: string;
  version: string;
  digest: string;
}

export interface ContractProvenance {
  derivedAt: string;
  schemas?: ContractProvenanceSchema[];
}

export interface BrowseContract {
  schemaVersion: string;
  module: {
    namespace: string;
    name: string;
    provider?: string;
    version: string;
    source?: string;
  };
  variables?: ContractVariable[];
  outputs?: ContractOutput[];
  requiredProviders?: ContractRequiredProvider[];
  compatibility: ContractCompatibility;
  provenance: ContractProvenance;
}

export async function getContract(
  namespace: string,
  kind: string,
  name: string,
  version?: string,
  token?: string,
): Promise<BrowseContract | null> {
  const qs = version ? `?version=${encodeURIComponent(version)}` : "";
  const headers: Record<string, string> = { Accept: "application/json" };
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(
    `${BASE_URL}/opendepot/ui/v1/resources/${encodeURIComponent(namespace)}/${encodeURIComponent(kind)}/${encodeURIComponent(name)}/contract${qs}`,
    { headers, cache: "no-store" },
  );

  if (res.status === 404) {
    return null;
  }

  if (!res.ok) {
    throw new Error(`API request failed: ${res.status} ${res.statusText}`);
  }

  return res.json() as Promise<BrowseContract>;
}

export interface ProviderSchemaAttribute {
  type: CtyType;
  required?: boolean;
  optional?: boolean;
  computed?: boolean;
}

export interface ProviderSchemaNestedBlock {
  nesting: string;
  minItems?: number;
  maxItems?: number;
  block: ProviderSchemaBlock;
}

export interface ProviderSchemaBlock {
  attributes?: Record<string, ProviderSchemaAttribute>;
  blocks?: Record<string, ProviderSchemaNestedBlock>;
}

export interface BrowseProviderSchema {
  namespace: string;
  name: string;
  providerNamespace: string;
  providerName: string;
  version: string;
  schemaVersion: "assembly.provider.v1";
  configuration: ProviderSchemaBlock;
}

export interface AssemblyDiagnostic {
  code: string;
  message: string;
  path?: string;
  nodeId?: string;
}

export interface AssemblyErrorResponse {
  error: string;
  message: string;
  diagnostics?: AssemblyDiagnostic[];
  output?: string;
}

export async function getProviderSchema(
  namespace: string,
  name: string,
  version: string,
): Promise<BrowseProviderSchema | null> {
  const response = await fetch(
    `/api/provider-schema/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}?version=${encodeURIComponent(version)}`,
    { cache: "no-store" },
  );

  if (response.status === 404) {
    return null;
  }
  if (!response.ok) {
    throw new Error(`Provider schema request failed: ${response.status} ${response.statusText}`);
  }

  return response.json() as Promise<BrowseProviderSchema>;
}

export interface AssemblyProgressEvent {
  phase?: string;
  output: string;
}

interface AssemblyStreamEvent {
  type: "started" | "output" | "complete" | "error";
  phase?: string;
  output?: string;
  archive?: string;
  error?: AssemblyErrorResponse;
}

export async function exportAssembly(
  document: unknown,
  onProgress?: (event: AssemblyProgressEvent) => void,
): Promise<{ blob?: Blob; error?: AssemblyErrorResponse }> {
  const response = await fetch("/api/assembly/export", {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/x-ndjson, application/json" },
    body: JSON.stringify(document),
  });

  if (!response.ok) {
    return { error: (await response.json()) as AssemblyErrorResponse };
  }

  if (response.headers.get("Content-Type")?.includes("application/x-ndjson")) {
    return readAssemblyStream(response, onProgress);
  }

  const bytes = await response.arrayBuffer();
  if (!isCompleteAssemblyZip(bytes)) {
    return {
      error: {
        error: "invalid_export_response",
        message: "The export server returned an invalid ZIP archive. Please try again.",
      },
    };
  }

  return { blob: new Blob([bytes], { type: "application/zip" }) };
}

export async function readAssemblyStream(
  response: Response,
  onProgress?: (event: AssemblyProgressEvent) => void,
): Promise<{ blob?: Blob; error?: AssemblyErrorResponse }> {
  if (!response.body) {
    return { error: { error: "invalid_export_response", message: "The export server returned an empty response." } };
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let pending = "";
  let result: { blob?: Blob; error?: AssemblyErrorResponse } | undefined;

  const consume = (line: string) => {
    if (!line.trim()) return;

    const event = JSON.parse(line) as AssemblyStreamEvent;
    if ((event.type === "started" || event.type === "output") && event.output) {
      onProgress?.({ phase: event.phase, output: event.output });
    } else if (event.type === "error" && event.error) {
      result = { error: event.error };
    } else if (event.type === "complete" && event.archive) {
      const binary = window.atob(event.archive);
      const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
      if (!isCompleteAssemblyZip(bytes.buffer)) {
        result = { error: { error: "invalid_export_response", message: "The export server returned an invalid ZIP archive. Please try again." } };
      } else {
        result = { blob: new Blob([bytes], { type: "application/zip" }) };
      }
    }
  };

  while (true) {
    const { done, value } = await reader.read();
    pending += decoder.decode(value, { stream: !done });
    const lines = pending.split("\n");
    pending = lines.pop() ?? "";
    lines.forEach(consume);
    if (done) break;
  }
  consume(pending);

  return result ?? { error: { error: "invalid_export_response", message: "The export stream ended before completion." } };
}

function isCompleteAssemblyZip(bytes: ArrayBuffer): boolean {
  const data = new Uint8Array(bytes);
  if (data.length < 22 || data[0] !== 0x50 || data[1] !== 0x4b || data[2] !== 0x03 || data[3] !== 0x04) {
    return false;
  }

  const view = new DataView(bytes);
  const minimumOffset = Math.max(0, data.length - 22 - 0xffff);
  for (let offset = data.length - 22; offset >= minimumOffset; offset -= 1) {
    if (view.getUint32(offset, true) !== 0x06054b50) {
      continue;
    }

    const commentLength = view.getUint16(offset + 20, true);
    const entryCount = view.getUint16(offset + 10, true);
    const centralDirectorySize = view.getUint32(offset + 12, true);
    const centralDirectoryOffset = view.getUint32(offset + 16, true);

    return (
      commentLength === data.length - offset - 22 &&
      entryCount === 2 &&
      centralDirectoryOffset + centralDirectorySize === offset &&
      centralDirectoryOffset + 4 <= data.length &&
      view.getUint32(centralDirectoryOffset, true) === 0x02014b50
    );
  }

  return false;
}

// ── Graph types ────────────────────────────────────────────────────────────

export interface BrowseGraphDepot {
  id: string;
  namespace: string;
  name: string;
  storageBackend?: string;
  pollingIntervalMinutes?: number;
  managedModuleNames?: string[];
  managedProviderNames?: string[];
}

export interface BrowseGraphModule {
  id: string;
  namespace: string;
  name: string;
  provider?: string;
  synced: boolean;
  syncStatus?: string;
  repoURL?: string;
  latestVersion?: string;
  depotID?: string;
  scanCounts?: BrowseScanCounts;
}

export interface BrowseGraphProvider {
  id: string;
  namespace: string;
  name: string;
  providerNamespace?: string;
  upstreamRegistry?: string;
  synced: boolean;
}

export interface BrowseGraphEdge {
  id: string;
  source: string;
  target: string;
}

export interface BrowseGraphSummary {
  totalDepots: number;
  totalModules: number;
  totalProviders: number;
}

export interface BrowseDepotGraph {
  depots: BrowseGraphDepot[];
  modules: BrowseGraphModule[];
  providers: BrowseGraphProvider[];
  edges: BrowseGraphEdge[];
  summary: BrowseGraphSummary;
  generatedAt: string;
}

export async function getDepotsGraph(namespace?: string, token?: string): Promise<BrowseDepotGraph> {
  const params = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<BrowseDepotGraph>(`/opendepot/ui/v1/depots/graph${params}`, token);
}

// ── Stats types ────────────────────────────────────────────────────────────

export interface SyncHealthStats {
  syncedVersions: number;
  unsyncedVersions: number;
  failedVersions: number;
}

export interface SecurityPostureStats {
  critical: number;
  high: number;
  medium: number;
  low: number;
  unknown: number;
  exempted: number;
  totalAffectedResources: number;
}

export interface StorageBackendStat {
  backend: string;
  count: number;
}

export interface PopularResource {
  namespace: string;
  kind: string;
  name: string;
  version: string;
  downloadCount: number;
  lastDownloadedAt?: string;
}

export interface BrowseStats {
  totalModules: number;
  totalProviders: number;
  totalVersions: number;
  totalStorageBytes: number;
  totalDownloads: number;
  downloadWindow: string;
  syncHealth: SyncHealthStats;
  securityPosture: SecurityPostureStats;
  storageDistribution: StorageBackendStat[];
  mostDownloaded: PopularResource[];
}

export async function getStats(namespace?: string, token?: string): Promise<BrowseStats> {
  const params = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  return apiFetch<BrowseStats>(`/opendepot/ui/v1/stats${params}`, token);
}
