---
tags:
  - helm
  - installation
---

# Helm Chart

The OpenDepot Helm chart is published to a GitHub Pages Helm repository:

```bash
helm repo add opendepot https://opendepot.defdev.io
helm repo update
```

The chart source is also available at [`chart/opendepot/`](https://github.com/tonedefdev/opendepot/tree/main/chart/opendepot) in the repository.

See [Installation](getting-started/installation.md) for prerequisites and deployment instructions.

## Global

| Value | Type | Description |
|-------|------|-------------|
| `global.namespace` | string | Namespace for all resources. Default: `opendepot-system` |
| `global.imagePullPolicy` | string | Image pull policy. Default: `IfNotPresent` |
| `global.image.tag` | string | Image tag for all services. Defaults to `Chart.AppVersion` when blank. |

## Server Configuration

### General

| Value | Type | Description |
|-------|------|-------------|
| `server.enabled` | bool | Deploy the server. Default: `true` |
| `server.replicaCount` | int | Number of replicas. Default: `1` |
| `server.anonymousAuth` | bool | Use the server's service account for unauthenticated module access. Default: `false` |
| `server.useBearerToken` | bool | Use bearer token auth instead of kubeconfig. Default: `true` |
| `server.policyManagement.enabled` | bool | Enable server-side ScanPolicy create, update, and delete routes. Reads remain available when false; writes are disabled by default. Default: `false` |
| `server.image.repository` | string | Server image repository. Default: `gcr.io/opendepot-495604/opendepot/server` |
| `server.service.type` | string | Kubernetes Service type. Default: `LoadBalancer` |
| `server.service.port` | int | Service port. Default: `80` |
| `server.service.targetPort` | int | Container port. Default: `8080` |
| `server.tls.enabled` | bool | Enable TLS on the server. Default: `false` |
| `server.tls.certPath` | string | Path to TLS certificate. Default: `/etc/tls/tls.crt` |
| `server.tls.keyPath` | string | Path to TLS key. Default: `/etc/tls/tls.key` |
| `server.ingress.enabled` | bool | Enable Kubernetes Ingress. Default: `false` |
| `server.ingress.hosts` | list | Standard Ingress host/path rules. |
| `server.ingress.tls` | list | Standard Ingress TLS configuration. Default: `[]` |
| `server.ingress.istio.enabled` | bool | Enable Istio VirtualService. Default: `false` |
| `server.ingress.istio.hosts` | list | Istio VirtualService hosts. Default: `[opendepot.defdev.io]` |
| `server.resources.requests.cpu` | string | CPU request. Default: `100m` |
| `server.resources.requests.memory` | string | Memory request. Default: `128Mi` |
| `server.resources.limits.memory` | string | Memory limit. Default: `512Mi` |
| `server.nodeSelector` | map | Node selector. Default: `{}` |
| `server.tolerations` | list | Tolerations. Default: `[]` |
| `server.affinity` | map | Affinity rules. Default: `{}` |
| `server.podDisruptionBudget.enabled` | bool | Enable PodDisruptionBudget. Default: `false` |
| `server.podDisruptionBudget.minAvailable` | int | Minimum available pods. Default: `2` |

### OIDC Authentication

The `server.oidc` section enables OIDC JWT validation for production-ready single sign-on. See [Authentication](authentication/index.md) for detailed setup and examples.

| Value | Type | Description |
|-------|------|-------------|
| `server.oidc.enabled` | bool | When true, enables OIDC JWT validation and advertises the `login.v1` service discovery endpoint. Default: `false` |
| `server.oidc.issuerUrl` | string | OIDC issuer URL (e.g., `https://opendepot.example.com/dex`). When blank and `dex.enabled: true`, auto-derives the in-cluster Dex service URL. |
| `server.oidc.clientId` | string | OIDC client ID. Must match the Dex static client `id`. Default: `"opendepot"` |
| `server.oidc.clientSecretName` | string | Name of a Kubernetes Secret containing the `clientSecret` key. When blank, the chart creates a Secret from `server.oidc.clientSecret`. |
| `server.oidc.clientSecret` | string | Dex client secret (only used if `clientSecretName` is blank). In production, use an external secret operator instead of storing plaintext here. |
| `server.oidc.groupsClaim` | string | JWT claim name containing the user's groups, used for [GroupBinding](guides/groupbinding.md) evaluation. When blank, defaults to `groups`. Set to `cognito:groups`, `roles`, etc. for non-standard IdPs. |
| `server.oidc.allowServiceAccountFallback` | bool | When true, Kubernetes ServiceAccount bearer tokens with a non-OIDC issuer are authenticated via the bearer-token path using the SA's own RBAC. GroupBinding is bypassed for SA tokens. Requires `server.oidc.enabled: true`. Default: `false` |
| `server.oidc.allowClientCredentials` | bool | When true, Dex tokens whose audience does not match the primary client ID are accepted. The token's `sub` claim is mapped to a virtual group `"client:<sub>"` and evaluated against `GroupBinding` resources. Requires a Dex `staticClient` with `grantTypes: ["client_credentials"]`. Default: `false` |
| `server.oidc.dexProxy.enabled` | bool | When true and OIDC is enabled, the server reverse-proxies `/dex/*` requests to the bundled Dex service so Dex never needs its own public ingress or hostname. Requires `dex.enabled: true` and `server.oidc.issuerUrl` set to the external, path-based URL matching `dex.config.issuer`. Set to `false` for external OIDC providers; ignored when OIDC is disabled. Default: `true` |
| `server.oidc.authzUrl` | string | Overrides the authorization URL advertised in `login.v1` of `/.well-known/terraform.json`. Leave blank to use the URL from the OIDC provider discovery document. Not needed when `server.oidc.dexProxy.enabled: true`. Use this when the server discovers Dex via an in-cluster address but CLI clients must reach Dex at a different address (e.g. a port-forwarded URL during local Kind testing). |
| `server.oidc.tokenUrl` | string | Overrides the token URL advertised in `login.v1` of `/.well-known/terraform.json`. Same use-case as `authzUrl`. Not needed when `server.oidc.dexProxy.enabled: true`. |

**Example (recommended — Dex proxied through the server):**

```yaml
server:
  oidc:
    enabled: true
    issuerUrl: https://opendepot.example.com/dex
    clientId: opendepot
    clientSecret: $(openssl rand -base64 32)
    clientSecretName: ""  # Use the above value; or set to "my-secret" to use external secret
    dexProxy:
      enabled: true
```

!!! warning
    When both `dex.enabled` and `server.oidc.enabled` are `true`, the Helm render fails if neither `server.oidc.clientSecret` nor `server.oidc.clientSecretName` is set. For production, pre-create a Kubernetes Secret and reference it via `server.oidc.clientSecretName`.

## Dex Configuration

The `dex` section deploys Dex as an OIDC identity provider. Dex federates upstream IdPs (GitHub, Entra ID, Okta, LDAP, etc.) and issues JWTs that the server validates locally.

| Value | Type | Description |
|-------|------|-------------|
| `dex.enabled` | bool | When true, deploys Dex as a subchart. Default: `false` |
| `dex.config.issuer` | string | Public issuer URL. Recommended (server-proxied): same host as the server ingress, e.g. `https://opendepot.example.com/dex`. In-cluster (no proxy): `http://opendepot-dex.opendepot-system.svc.cluster.local:5556/dex`. Separately exposed: `https://dex.example.com/dex` |
| `dex.config.connectors` | array | Array of upstream IdP connector configurations. See examples below. Default: `[]` |
| `dex.config.enablePasswordDB` | bool | When true, enables local username/password authentication (testing only). Default: `false` |
| `dex.config.staticPasswords` | array | Array of test users for local auth. Never enable in production. Default: `[]` |

**Basic Example (GitHub):**

```yaml
dex:
  enabled: true
  config:
    issuer: https://opendepot.example.com/dex
    connectors:
      - type: github
        id: github
        name: GitHub
        config:
          clientID: <github-oauth-app-client-id>
          clientSecret: <github-oauth-app-secret>
          redirectURI: https://opendepot.example.com/dex/callback
          org: my-org  # (optional) restrict to an org
```

**Entra ID (Azure AD) Example:**

```yaml
dex:
  enabled: true
  config:
    issuer: https://opendepot.example.com/dex
    connectors:
      - type: microsoft
        id: microsoft
        name: "Azure AD"
        config:
          clientID: <azure-app-id>
          clientSecret: <azure-app-secret>
          redirectURI: https://opendepot.example.com/dex/callback
          tenant: <azure-tenant-id>
```

For connector configuration details, refer to the [Dex Connector Documentation](https://dexidp.io/docs/connectors/).

!!! warning
    Never set `enablePasswordDB: true` or `staticPasswords` in production. Use real IdP connectors instead.

## Controllers

These values apply to `version`, `module`, `depot`, and `provider` independently — substitute `<service>` with the controller name:

| Value | Type | Description |
|-------|------|-------------|
| `<service>.enabled` | bool | Deploy the controller. Default: `true` (`provider`: `false`) |
| `<service>.replicaCount` | int | Number of replicas. Default: `1` |
| `<service>.image.repository` | string | Image repository. Default: `gcr.io/opendepot-495604/opendepot/<service>-controller` |
| `<service>.image.tag` | string | Overrides `global.image.tag` when set. |
| `<service>.resources.requests.cpu` | string | CPU request. Default: `100m` |
| `<service>.resources.requests.memory` | string | Memory request. Default: `512Mi` for `version`, `128Mi` for others |
| `<service>.resources.limits.memory` | string | Memory limit. Default: `4Gi` for `version`, `512Mi` for others |
| `<service>.nodeSelector` | map | Node selector. Default: `{}` |
| `<service>.tolerations` | list | Tolerations. Default: `[]` |
| `<service>.affinity` | map | Affinity rules. Default: `{}` |

!!! note
    The provider controller is disabled by default (`provider.enabled: false`). Enable it explicitly when you are ready to sync provider binaries — provider archives can be several hundred megabytes each.

The Version controller's init containers use fixed bounded resources (`10m` CPU
and `16Mi` memory requests; `100m` CPU and `32Mi` memory limits). The controller
also runs a `token-refresh-helper` sidecar that refreshes the projected
ServiceAccount token used by the controller. The main controller does not use a
directly mounted Kubernetes ServiceAccount token.

## Assembly Line

Assembly Line derives module contracts and provider configuration schemas and enables validated root-module ZIP export. It is disabled by default (`assembly.enabled: false`) and is OpenTofu-only — see [Assembly Line](guides/assembly-line.md#opentofu-only-eligibility) for provider eligibility. `ui.baseUrl` must be a valid external HTTP(S) URL when Assembly Line is enabled; its host, including a non-default port, is used for generated module source addresses and the generated OpenTofu Provider Network Mirror URL. Generated provider source addresses use the provider's canonical short identity (e.g. `hashicorp/aws`) instead.

| Value | Type | Description |
|-------|------|-------------|
| `assembly.enabled` | bool | Enable contract/schema extraction and initialized exports. Default: `false` |
| `assembly.validationRegistryUrl` | string | HTTPS registry and Provider Network Mirror origin used only by server-side OpenTofu initialization. Defaults to `ui.baseUrl`. |
| `assembly.validationCACertPath` | string | Optional PEM CA bundle trusted only by the temporary OpenTofu initialization process. Default: `""` |
| `assembly.tofuBinPath` | string | OpenTofu binary path in the version and server images. Default: `/usr/local/bin/tofu` |
| `assembly.extractionTimeout` | duration | Provider schema extraction timeout. Default: `5m` |
| `assembly.initTimeout` | duration | Export `tofu init` timeout. Default: `2m` |
| `assembly.maxRequestBytes` | int | Maximum export request body. Default: `2097152` |
| `assembly.maxNodes` | int | Maximum total canvas nodes per export. Default: `100` |
| `assembly.maxOutputBytes` | int | Maximum captured output per OpenTofu command. Default: `65536` |
| `assembly.workDir` | string | Server validation workspace mount. Default: `/var/lib/opendepot/assembly` |
| `assembly.workspaceSizeLimit` | quantity | Validation `emptyDir` size limit. Default: `1Gi` |

The server root filesystem remains read-only. The chart mounts only `assembly.workDir` as a writable, size-limited `emptyDir`. Consider adding `ephemeral-storage` requests and limits under `server.resources` and `version.resources` for production workloads.

See [Assembly Line](guides/assembly-line.md) for provider bindings, generated files, authentication, and validation behavior.

## GPG Signing (Providers)

The server signs `SHA256SUMS` files for provider packages using a GPG key you supply. OpenTofu and Terraform verify this signature as part of the [Provider Registry Protocol](https://developer.hashicorp.com/terraform/internals/provider-registry-protocol). See [GPG Signing for Providers](configuration/gpg.md) for full setup instructions.

| Value | Type | Description |
|-------|------|-------------|
| `server.gpg.secretName` | string | Name of the Kubernetes Secret containing GPG signing credentials (`OPENDEPOT_PROVIDER_GPG_KEY_ID`, `OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR`, `OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64`). Default: `""` |

## Service Account & RBAC

| Value | Type | Description |
|-------|------|-------------|
| `serviceAccount.create` | bool | Create service accounts. Default: `true` |
| `serviceAccount.annotations` | map | Annotations (use for IRSA/Workload Identity). Default: `{}` |
| `rbac.create` | bool | Create RBAC roles and bindings. Default: `true` |
| `rbac.scopeToNamespace` | bool | Use namespace-scoped Role/RoleBinding instead of ClusterRole/ClusterRoleBinding. Default: `false` |

## Storage

| Value | Type | Description |
|-------|------|-------------|
| `storage.filesystem.enabled` | bool | Enable shared volume for filesystem storage. Default: `false` |
| `storage.filesystem.mountPath` | string | Mount path inside containers. Default: `/data/modules` |
| `storage.filesystem.hostPath` | string | Use a hostPath volume (for local dev with kind). Default: `""` |
| `storage.filesystem.storageClassName` | string | StorageClass for PVC (requires `ReadWriteMany`). Default: `""` |
| `storage.filesystem.size` | string | PVC storage size. Default: `10Gi` |

See [Storage Backends](storage/index.md) for S3, Azure, and GCS configuration, which are set via environment variables rather than Helm values.

## UI Configuration

The `ui` section deploys the Registry Explorer frontend. See [Registry Explorer UI](guides/registry-explorer/index.md) for setup, OIDC login, and public visibility configuration.

| Value | Type | Description |
|-------|------|-------------|
| `ui.enabled` | bool | When true, deploys the Registry Explorer UI and NGINX proxy. Also suppresses `server-ingress.yaml` — migrate traffic to `ui.ingress` before enabling. Default: `false` |
| `ui.replicaCount` | int | Number of UI pod replicas. Default: `1` |
| `ui.image.repository` | string | UI container image repository. Default: `gcr.io/opendepot-495604/opendepot/ui` |
| `ui.image.tag` | string | Image tag. Defaults to `global.image.tag`, then the chart `appVersion`. |
| `ui.serverHost` | string | Upstream `host:port` that NGINX proxies registry requests to. Defaults to `server.<namespace>.svc.cluster.local:80` when blank. |
| `ui.serverCACertPath` | string | Optional PEM CA path from the `opendepot-tls` Secret that Next.js trusts when connecting to a privately signed TLS server. Default: `""` |
| `ui.sessionPasswordSecretName` | string | Name of a Kubernetes Secret with a `sessionPassword` key (min 32 chars). Required when `ui.enabled: true`. |
| `ui.oidc.enabled` | bool | Enables OIDC authorization code login in the UI. Default: `false` |
| `ui.oidc.issuerUrl` | string | Public HTTPS OIDC issuer URL. Discovered endpoints must share this origin. HTTP is accepted only with `global.developmentMode: true`. |
| `ui.oidc.clientId` | string | OIDC client ID for the UI. Default: `"opendepot-ui"`. When `ui.oidc.enabled: true` and non-empty, the chart also passes `--oidc-ui-client-id` to the server so UI-issued tokens are accepted on browse and stats endpoints. See [Registry Explorer UI OIDC](authentication/oidc.md#registry-explorer-ui-oidc). |
| `ui.oidc.clientSecretName` | string | Name of a Kubernetes Secret with a `clientSecret` key for the OIDC confidential client. |
| `ui.oidc.scopes` | string | Space-separated OIDC scopes. Default: `"openid profile email groups"` |
| `ui.oidc.callbackPath` | string | OIDC redirect URI path registered with the identity provider. Default: `"/auth/callback"` |
| `ui.auth.devTokenInput.enabled` | bool | When true, shows a developer bearer-token input in the UI. **Must be `false` in production.** Default: `false` |
| `ui.ingress.enabled` | bool | Creates a Kubernetes Ingress for the UI with split-path routing rules. Default: `false` |
| `ui.ingress.className` | string | Ingress class name. |
| `ui.ingress.annotations` | map | Annotations applied to the Ingress resource. |
| `ui.ingress.hosts` | list | Host and path rules. |
| `ui.ingress.tls` | list | TLS configuration for the Ingress. |

## Prometheus Monitoring

The chart can optionally install a minimal [kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack). Grafana, Alertmanager, and node exporters are disabled by default. The OpenDepot `server` Service exposes `/metrics` on the named `metrics` port (`9090`). The chart's `ServiceMonitor` selects that Service by the `app: server` label in `global.namespace`.

The ServiceMonitor is enabled by default and scrapes every 15 seconds with a 10-second timeout. It requires a Prometheus Operator. If an existing operator selects ServiceMonitors by label, set `server.metrics.serviceMonitor.additionalLabels` to the labels it expects. Set `server.metrics.serviceMonitor.enabled: false` when the external monitoring system discovers the endpoint by another method.

The Stats page queries Prometheus through its HTTP API. For an existing Prometheus deployment, set `server.stats.prometheusURL` to its absolute HTTP API base URL:

```yaml
server:
  stats:
    prometheusURL: https://prometheus.example.com
```

This value takes precedence over the chart's in-cluster fallback. When it is empty and `monitoring.enabled: true`, the server falls back to `http://<release>-monitoring-prometheus.<release-namespace>.svc.cluster.local:9090`, which requires `monitoring.bundled.enabled: true`. The bundled stack is disabled by default because the Prometheus Operator requires cluster-wide RBAC.

| Value | Type | Description |
|-------|------|-------------|
| `monitoring.enabled` | bool | Enable OpenDepot monitoring resources, including the `ServiceMonitor` and the server's bundled-Prometheus fallback. Default: `true` |
| `monitoring.bundled.enabled` | bool | Install the bundled kube-prometheus-stack. Default: `false` |
| `monitoring.prometheus.prometheusSpec.retention` | string | Local Prometheus retention. Default: `90d` |
| `monitoring.prometheus.prometheusSpec.remoteWrite` | list | Optional remote-write targets such as Mimir for long-term storage. |
| `server.metrics.serviceMonitor.enabled` | bool | Create the OpenDepot `ServiceMonitor`. Default: `true` |
| `server.metrics.serviceMonitor.interval` | string | ServiceMonitor scrape interval. Default: `15s` |
| `server.metrics.serviceMonitor.scrapeTimeout` | string | ServiceMonitor scrape timeout. Default: `10s` |
| `server.metrics.serviceMonitor.additionalLabels` | map | Additional labels for matching an external Prometheus Operator's ServiceMonitor selector. Default: `{}` |
| `server.stats.prometheusURL` | string | Prometheus HTTP API URL. When blank, the server uses the bundled Prometheus service address only if `monitoring.enabled: true`; that service must be installed with `monitoring.bundled.enabled: true`. |
| `server.stats.lookback` | string | Stats page query window. Default: `90d` |
| `server.stats.queryTimeout` | duration | Prometheus query timeout. Default: `5s` |

The bundled Prometheus stack requires cluster-wide Prometheus Operator RBAC and
is disabled by default. Enable it only when that cluster-wide access is
acceptable:

```yaml
monitoring:
  bundled:
    enabled: true
```

The Stats page uses the configured lookback window, not an all-time total. For time series longer than local retention, configure `remoteWrite` to Mimir or another Prometheus-compatible backend and use Grafana to query the retained history.

### Source-built controller dependencies

The server and Version controller images build the pinned OpenTofu source commit
used for initialization and schema extraction. The scanning image also builds
Trivy from a pinned source commit. The chart release metadata is the source of
the default image tag; release `0.11.0` uses chart and application version
`0.11.0`.

See [Download Tracking](guides/registry-explorer/browse.md#download-tracking) for details on how stats are recorded and surfaced in the Registry Explorer UI.

## Scanning Values

The `scanning` section controls Trivy-based vulnerability scanning for modules and providers. See [Vulnerability Scanning](configuration/scanning.md) for full details.

| Value | Type | Description |
|-------|------|-------------|
| `scanning.enabled` | bool | Enable Trivy-based scanning. Switches the version-controller to the `-scanning` image variant and activates module IaC scanning. No PVC or CronJob is created at this level. Default: `false` |
| `scanning.providerScanning` | bool | Enable provider binary and source scanning. Requires `scanning.enabled: true`. Creates the Trivy DB PVC and `trivy-db-updater` CronJob and mounts the cache volume. Default: `false` |
| `scanning.cacheMountPath` | string | Mount path inside the version-controller container for the Trivy DB cache. Default: `/var/cache/trivy` |
| `scanning.offline` | bool | Pass `--offline-scan` to Trivy, preventing network calls during scans. Only applies to provider scanning. Default: `true` |
| `scanning.blockOnCritical` | bool | Halt reconciliation when CRITICAL findings are present (modules or providers). Default: `false` |
| `scanning.blockOnHigh` | bool | Halt reconciliation when HIGH findings are present (modules or providers). Default: `false` |
| `scanning.cache.storageClassName` | string | StorageClass for the Trivy cache PVC (must support ReadWriteMany for multi-node). Omitted from the PVC manifest when blank, allowing the cluster default to apply stably across upgrades. Default: `""` |
| `scanning.cache.accessMode` | string | Access mode for the Trivy cache PVC. Default: `ReadWriteMany` |
| `scanning.cache.size` | string | Size of the Trivy DB cache PVC. Default: `1Gi` |
| `scanning.dbUpdater.schedule` | string | Cron schedule for the Trivy DB update job. Default: `"0 2 * * *"` |
| `scanning.dbUpdater.image.repository` | string | Trivy image repository for the db-updater CronJob. Default: `aquasec/trivy` |
| `scanning.dbUpdater.image.tag` | string | Trivy image tag for the db-updater CronJob. Default: `"0.70.0"` |

!!! note
    `blockOnCritical` and `blockOnHigh` are cluster-wide floors. For per-finding exemptions without loosening enforcement everywhere, create a namespaced [`ScanPolicy`](reference/api.md#scanpolicy) resource instead of relaxing these flags. See [Policy Enforcement](configuration/scanning.md#policy-enforcement). The chart installs the `ScanPolicy` CRD and grants the version controller `get`/`list`/`watch` on `scanpolicies` automatically — no additional values are required to use it.

The optional policy-management API is intentionally separate from controller enforcement. Enabling `server.policyManagement.enabled` adds write verbs to the server ServiceAccount and exposes the UI CRUD surface; it does not grant users access. Configure Kubernetes `Role`/`RoleBinding` permissions for kubeconfig or bearer-token callers, or create a least-privilege [`SecurityGroupBinding`](guides/security-groupbinding.md) for OIDC callers. Keep the value `false` when policies are applied only through GitOps.

#### Tilt and monitoring scope

The repository's Tilt profile is development-only: it enables `global.developmentMode`, local TLS/Dex, Trivy scanning, the UI, and the bundled Prometheus stack for an end-to-end environment. It does **not** enable `server.policyManagement.enabled`; turn that on only in a disposable cluster when testing the write surface. Do not copy the Tilt values file into production: it uses local filesystem storage, test OIDC settings, and Dex password authentication.

The bundled monitoring configuration is intentionally narrow. It scrapes OpenDepot application metrics with a single Prometheus and 90-day retention; Grafana, Alertmanager, node exporters, Kubernetes component monitors, and the Prometheus Operator ServiceMonitor are disabled by default in the chart values. Policy audit evidence comes from server/Kubernetes logs, not from Prometheus. Configure `monitoring.prometheus.prometheusSpec.remoteWrite` and a durable audit-log sink separately when longer retention or production alerting is required.

