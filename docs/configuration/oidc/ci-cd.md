---
tags:
  - configuration
  - oidc
  - ci
  - rbac
---

# OIDC CI/CD Authentication

OpenDepot supports two CI/CD paths when OIDC is enabled:

- **ServiceAccount fallback** uses Kubernetes RBAC for pipelines that already
  authenticate to the cluster.
- **Client credentials** uses short-lived Dex tokens and GroupBinding without
  distributing kubeconfigs or ServiceAccount tokens.

## ServiceAccount Fallback

By default, every token must be a valid Dex JWT. Set
`server.oidc.allowServiceAccountFallback: true` to allow Kubernetes
ServiceAccount tokens as a second authentication path:

```yaml
server:
  oidc:
    enabled: true
    allowServiceAccountFallback: true
```

Tokens from a different issuer are forwarded to the Kubernetes API, where the
ServiceAccount's RBAC controls access. GroupBinding is not evaluated for these
tokens. Invalid or expired Dex tokens are still rejected with `401 Unauthorized`.

### Required RBAC

The ServiceAccount needs `get` and `list` access to the resources it downloads:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: opendepot-registry-reader
  namespace: opendepot-system
rules:
- apiGroups: ["opendepot.defdev.io"]
  resources: ["modules", "versions", "providers"]
  verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: opendepot-registry-reader-binding
  namespace: opendepot-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: opendepot-registry-reader
subjects:
- kind: ServiceAccount
  name: my-ci-sa
  namespace: my-ci-namespace
```

See [Registry Reads: SA Fallback with OIDC](../../guides/cicd.md#registry-reads-sa-fallback-with-oidc)
for a pipeline example.

## Client Credentials

A service can obtain a short-lived Dex access token through the OAuth2 client
credentials grant and present it as a bearer token. The token's `sub` claim is
mapped to the virtual group `client:<sub>`, which GroupBinding expressions can
match.

!!! note
    Client credentials tokens use `aud=<client-id>`, not the primary OpenDepot
    client ID. OpenDepot uses a secondary verifier that still validates the Dex
    signature and expiry.

### Step 1: Register a Client in Dex

```yaml
dex:
  config:
    oauth2:
      grantTypes:
        - authorization_code
        - client_credentials
    staticClients:
      - id: ci-pipeline
        name: CI Pipeline
        secretEnv: OPENDEPOT_CC_CLIENT_SECRET
        grantTypes:
          - client_credentials
  envFrom:
    - secretRef:
        name: opendepot-cc-client-secret
```

!!! warning
    Manage the client secret like any other credential. Use an external secret
    operator or a secrets manager rather than committing it to Helm values.

### Step 2: Enable Client Credentials

```yaml
server:
  oidc:
    enabled: true
    allowClientCredentials: true
```

### Step 3: Create a GroupBinding

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: ci-pipeline-binding
  namespace: opendepot-system
spec:
  expression: '"client:ci-pipeline" in groups'
  moduleResources:
    - "*"
  providerResources:
    - "*"
```

!!! warning "Use specific client expressions"
    Match the exact `client:<sub>` value. A broad expression that accepts every
    `client:` group would grant access to future machine clients as well.

### Step 4: Obtain and Use a Token

```bash
TOKEN=$(curl -s -X POST https://opendepot.example.com/dex/token \
  -d grant_type=client_credentials \
  -d client_id=ci-pipeline \
  -d client_secret=<secret> \
  -d scope=openid \
  | jq -r '.access_token')
```

The example assumes the server-proxied Dex setup. With shared external Dex, use
that Dex deployment's token endpoint instead.

Use the token in `.tofurc`:

```hcl
credentials "opendepot.example.com" {
  token = "<access_token>"
}
host "opendepot.example.com" {
  services = {
    "modules.v1"   = "https://opendepot.example.com/opendepot/modules/v1/"
    "providers.v1" = "https://opendepot.example.com/opendepot/providers/v1/"
  }
}
```

No `tofu login` is required.

## Choosing a CI/CD Method

| | ServiceAccount Fallback | Client Credentials |
|---|---|---|
| Token source | `kubectl create token` | Dex `client_credentials` grant |
| Requires cluster API access | Yes | No |
| Access control | Kubernetes RBAC | GroupBinding |
| Works without `kubectl` | No | Yes |
| Short-lived tokens | Yes, configurable | Yes, Dex-controlled |

See [Client Credentials in CI/CD](../../guides/cicd.md#registry-reads-with-dex-client-credentials)
for pipeline examples.
