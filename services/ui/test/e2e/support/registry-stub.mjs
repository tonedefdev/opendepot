// Serves one fixed worst-case agent row for the registry layout test. Only the two endpoints the registry page reads are answered.
import { createServer } from "node:http";

const port = Number(process.env.PORT ?? 8787);

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
  scanCounts: { critical: 8888, high: 8888, medium: 8888, low: 8888, unknown: 8888, exempted: 8888 },
  lastScanned: "2026-01-01T00:00:00Z",
  sourceRef: "refs/heads/main",
  sourceCommit: "0123456789abcdef0123456789abcdef01234567",
  jevRiskLevel: "Critical",
  blocked: false,
  blockReasons: [],
};

const responses = {
  "/opendepot/ui/v1/resources": { items: [worstCaseRow], totalCount: 1, page: 1, pageSize: 12 },
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
