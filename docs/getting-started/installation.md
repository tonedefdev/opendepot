---
tags:
  - installation
  - helm
  - kubernetes
search:
  boost: 2
---

# Installation

## Prerequisites

- Kubernetes v1.16+
- Helm 3.0+
- `kubectl` configured to access your cluster
- A supported storage backend (S3 bucket, Azure Storage Account, GCS bucket, or local filesystem)
- *(Optional)* A GitHub App for authenticated API access

### Optional OIDC Prerequisites (Recommended)

If you plan to use OIDC authentication and `tofu login`, also prepare:

- An OIDC issuer (Dex via Helm subchart or an external OIDC provider)
- A registered OIDC client for OpenDepot (client ID/secret)
- Externally reachable OIDC auth/token endpoints advertised in service discovery (`login.v1`)
- *(Optional)* `GroupBinding` resources for fine-grained module/provider access control

See [OIDC Configuration](../configuration/oidc/index.md) for full setup details and examples.

## Install with Helm

!!! tip "Minimal install"
    For a quick start, the one-liner below is all you need. OpenDepot will use in-cluster defaults. Customise with `--set` flags or a values file once you're ready.

```bash
helm repo add opendepot https://opendepot.defdev.io
helm repo update
helm install opendepot opendepot/opendepot \
  -n opendepot-system \
  --create-namespace
```

!!! warning "Upgrading an existing installation"
    Helm does not update CRDs during `helm upgrade`. If you are upgrading from a previous version, apply the latest CRDs manually first:
    ```bash
    helm show crds opendepot/opendepot | kubectl apply --server-side -f -
    ```

To customize values:

```bash
helm install opendepot opendepot/opendepot \
  -n opendepot-system \
  --create-namespace \
  --set global.image.tag=v0.1.0 \
  --set server.service.type=ClusterIP \
  --set depot.enabled=false
```

Or use a values file:

```bash
helm install opendepot opendepot/opendepot \
  -n opendepot-system \
  --create-namespace \
  -f my-values.yaml
```

See [Helm Chart](../helm-chart.md) for the full Helm values reference — Global, Server, OIDC, Dex, UI, Prometheus monitoring, Controllers, GPG, Service Account & RBAC, Storage, and Scanning values.

## Build from Source (Alternative)

If you prefer to build container images yourself:

```bash
# Build all services for linux/arm64
make build

# Load into a kind cluster
make load

# Or build and load in one step
make deploy
```

**Additional Makefile targets:**

| Target | Description |
|--------|-------------|
| `make build` | Build all container images |
| `make load` | Load all images into the kind cluster |
| `make deploy` | Build and load all images |
| `make build-server-assembly` | Build the `server` image with OpenTofu (`INCLUDE_TOFU=true`), tagged `$(TAG)-assembly` |
| `make load-server-assembly` | Load the `-assembly` server image into the kind cluster |
| `make build-version-controller-assembly` | Build the `version-controller` image with Trivy and OpenTofu (`INCLUDE_TRIVY=true INCLUDE_TOFU=true`), tagged `$(TAG)-assembly` |
| `make load-version-controller-assembly` | Load the `-assembly` version-controller image into the kind cluster |
| `make build-version-controller-scanning` | Build the `version-controller` image with Trivy (`INCLUDE_TRIVY=true`) |
| `make service NAME=server` | Build and load a single service |
| `make restart` | Restart all deployments in `opendepot-system` |
| `make redeploy` | Build, load, and restart all services |
| `make kind-restart` | Full cluster recreation with Istio, TLS, gateway, and Helm deploy (for production-like local setup) |

**Configurable variables:**

| Variable | Default | Description |
|----------|---------|-------------|
| `PLATFORM` | `linux/arm64` | Target platform for container builds |
| `KIND_CLUSTER` | `kind` | Name of the kind cluster |
| `TAG` | Current Unix timestamp (`date +%s`) | Image tag for all services. Set `TAG` explicitly for a stable tag |
| `REGISTRY` | `ghcr.io/tonedefdev/opendepot` | Container registry prefix |

`make build` produces the default images, which do not include OpenTofu. `make build-server-assembly` and `make build-version-controller-assembly` build the OpenTofu-enabled `-assembly` variants. Point `server.image.tag` and `version.image.tag` at the `-assembly` tag when you enable Assembly Line. See [Assembly Line Configuration](../configuration/assembly-line.md) for the Helm values.

!!! note
    `make ui-deploy` and `make ui-setup-oidc` set `server.image.tag` and `version.image.tag` to `$(TAG)-assembly` automatically. `make ui-setup-oidc` also builds and loads the `-assembly` images.
