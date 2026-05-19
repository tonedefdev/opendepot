---
tags:
  - contributing
---

# Contributing

Thank you for your interest in contributing to OpenDepot! Please read our [CONTRIBUTING.md](https://github.com/tonedefdev/opendepot/blob/main/CONTRIBUTING.md) on GitHub for guidelines on opening issues, submitting pull requests, and the development workflow.

Pull requests also run the e2e workflow, which builds the service images and scans them with Trivy before the controller tests execute. A PR fails if the scanner reports critical or high severity vulnerabilities in a built image.

## Local OIDC E2E Testing

The repository ships a set of `make` targets for end-to-end OIDC testing against a local Kind cluster. They wire together Kind, Helm, Dex v2.45.0, filesystem storage, mkcert TLS, and a static test user so that you can run `tofu login` without any cloud infrastructure.

!!! note
  The `make` targets use `opendepot.localtest.me` as the default registry hostname. This hostname resolves to `127.0.0.1` via public DNS — no `/etc/hosts` editing required. You can override it with `OIDC_REGISTRY_HOST=<hostname>` if you need a custom local name, in which case `make oidc-hosts` will add the entry to `/etc/hosts`.

| Target | Purpose |
|--------|---------|
| `make oidc-hosts` | Adds `$(OIDC_REGISTRY_HOST) → 127.0.0.1` to `/etc/hosts` (requires `sudo`). Only needed when using a custom `OIDC_REGISTRY_HOST` that does not resolve via public DNS |
| `make oidc-tls` | Generates a locally-trusted mkcert TLS cert for `opendepot.localtest.me`, `localhost`, and `127.0.0.1` and creates the `opendepot-tls` Kubernetes Secret |
| `make oidc-deploy PASS=<password>` | Deploys OpenDepot + Dex v2.45.0 with filesystem storage and a static OIDC test user |
| `make oidc-forward` | Port-forwards the server (`localhost:8080`) and Dex (`localhost:5556`) |
| `make oidc-login` | Runs `tofu login opendepot.localtest.me:8080` |
| `make oidc-test-resources` | Creates a sample Module and a GroupBinding for the test user's group |
| `make oidc-verify-module` | Runs an authenticated request against the test module to verify the full auth flow |
| `make oidc-test-clean` | Removes the test Module and GroupBinding |
| `make oidc-stop` | Stops all port-forwards |
| `make oidc-setup PASS=<password>` | Runs all setup steps (`deploy`, `oidc-tls`, `oidc-deploy`, `oidc-forward`) in sequence |

**Prerequisites:** [kind](https://kind.sigs.k8s.io/), [kubectl](https://kubernetes.io/docs/tasks/tools/), [Helm 3](https://helm.sh/docs/intro/install/), [mkcert](https://github.com/FiloSottile/mkcert), [OpenTofu](https://opentofu.org/docs/intro/install/), and either `htpasswd` (from `httpd`) or the Python `bcrypt` package for hashing the test user password.

**Full setup from a freshly created Kind cluster:**

```bash
# One-time: install mkcert CA into the system trust store
mkcert -install

# Create a kind cluster (if you don't have one already)
kind create cluster --name opendepot

# Build and load images, generate TLS cert, deploy, and port-forward
make oidc-setup PASS=mysecretpassword
```

**Login and test:**

```bash
make oidc-login           # opens tofu login - authenticate in the browser
make oidc-test-resources  # create test Module and GroupBinding
make oidc-verify-module   # verify authenticated access
```

The `oidc-deploy` target configures `authzUrl` and `tokenUrl` to point at the local port-forward (`http://localhost:5556/dex/auth` and `/token`). This is the split-network pattern described in [Split-Network OIDC](configuration/oidc.md#split-network-oidc-authzurl-tokenurl) - the server pod reaches Dex via the in-cluster service URL for token validation, while `tofu login` redirects the browser through the port-forward.
