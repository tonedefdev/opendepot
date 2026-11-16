// Serves one fixed worst-case agent row for the registry layout test. Only the two endpoints the registry page reads are answered.
import { createServer } from "node:http";

const port = Number(process.env.PORT ?? 8787);

// Four-digit counts on every chip are the widest case the Risk column has to fit.
const worstCaseRow = {
  kind: "agent",
  namespace: "platform",
  name: "very-long-agent-name-1-with-suffix",
  latestVersion: "v6.60.0",
  syncStatus: "Synced",
  synced: true,
  public: true,
  provider: "",
  repoUrl: "",
  providerNamespace: "",
  upstreamRegistry: "",
  platforms: [],
  scanCounts: { critical: 9999, high: 9999, medium: 9999, low: 9999, unknown: 9999, exempted: 9999 },
  lastScanned: "2026-01-01T00:00:00Z",
  sourceRef: "refs/heads/main",
  sourceCommit: "0123456789abcdef0123456789abcdef01234567",
  jevRiskLevel: "Critical",
  blocked: false,
  blockReasons: [],
};

// A provider row has no Jev meter, so its Risk cell holds only the five severity chips.
const providerRow = {
  kind: "provider",
  namespace: "platform",
  name: "aws",
  latestVersion: "v5.1.0",
  syncStatus: "Synced",
  synced: true,
  public: true,
  provider: "",
  repoUrl: "",
  providerNamespace: "",
  upstreamRegistry: "",
  platforms: [],
  scanCounts: { critical: 9999, high: 9999, medium: 9999, low: 9999, unknown: 9999, exempted: 0 },
  lastScanned: "2026-01-01T00:00:00Z",
  sourceRef: "",
  sourceCommit: "",
  jevRiskLevel: "",
  blocked: false,
  blockReasons: [],
};

const responses = {
  "/opendepot/ui/v1/resources": { items: [worstCaseRow, providerRow], totalCount: 2, page: 1, pageSize: 12 },
  "/opendepot/ui/v1/namespaces": { items: [{ name: "platform", public: true }] },
};

createServer((req, res) => {
  const pathname = new URL(req.url ?? "/", "http://localhost").pathname;
  const body = responses[pathname];
  res.setHeader("Content-Type", "application/json");
  if (!body) {
    res.statusCode = 404;
    res.end(JSON.stringify({ error: "not found" }));
    return;
  }
  res.end(JSON.stringify(body));
}).listen(port, "127.0.0.1");
