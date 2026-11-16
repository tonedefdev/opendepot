---
tags:
  - quickstart
  - kind
  - helm
  - kubernetes
search:
  boost: 2
---

# Quickstart

Run OpenDepot locally with the released Helm chart and public container images. This Quickstart creates a Kind cluster, installs the registry and UI, and uses anonymous access so you can explore without an identity provider.

The main step is one `Depot` resource. It discovers AWS modules and the AWS provider from upstream and creates the matching OpenDepot resources.

## Prerequisites

Install [Docker](https://docs.docker.com/get-docker/),
[Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation),
[`kubectl`](https://kubernetes.io/docs/tasks/tools/),
[Helm 3](https://helm.sh/docs/intro/install/), and
[mkcert](https://github.com/FiloSottile/mkcert#installation).

## Step 1: Create a cluster

Create a local Kind cluster:

```bash
kind create cluster --name opendepot
kubectl config use-context kind-opendepot
```

!!! note "Using an existing cluster"
    The same Helm installation works on an existing Kubernetes cluster. Skip
    the Kind commands and use the context for your cluster:

    ```bash
    kubectl config use-context <your-context>
    ```

Create the OpenDepot namespace:

```bash
kubectl create namespace opendepot-system
```

## Step 2: Install OpenDepot

Add the OpenDepot chart repository:

```bash
helm repo add opendepot https://opendepot.defdev.io
helm repo update
```

The UI encrypts its session cookies with a secret that must be at least 32
characters long. Create it before installing the chart:

```bash
kubectl create secret generic ui-session-secret \
  --from-literal=sessionPassword=$(openssl rand -base64 32) \
  -n opendepot-system
```

The server serves its API over HTTPS. Use [mkcert](https://github.com/FiloSottile/mkcert)
to create a local CA and a certificate for the Quickstart hostnames, then store
them in the `opendepot-tls` Secret. Run these commands in an empty directory:

```bash
mkcert -install
mkcert -cert-file tls.crt -key-file tls.key \
  opendepot.localtest.me server.opendepot-system.svc.cluster.local localhost

kubectl create secret generic opendepot-tls \
  --from-file=tls.crt=tls.crt \
  --from-file=tls.key=tls.key \
  --from-file=ca.crt="$(mkcert -CAROOT)/rootCA.pem" \
  -n opendepot-system
```

!!! note
    The UI loads the CA from the `opendepot-tls` Secret when it starts. If you
    regenerate the certificates after installing, restart the UI so it picks up
    the new CA:

    ```bash
    kubectl rollout restart deployment/ui -n opendepot-system
    ```

Install the released chart with the features used in this Quickstart:

```bash
helm install opendepot opendepot/opendepot \
  --namespace opendepot-system \
  --set server.anonymousAuth=true \
  --set server.tls.enabled=true \
  --set server.service.type=ClusterIP \
  --set provider.enabled=true \
  --set depot.enabled=true \
  --set ui.enabled=true \
  --set ui.sessionPasswordSecretName=ui-session-secret \
  --set ui.serverCACertPath=/etc/tls/ca.crt \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.hostPath=/var/local/opendepot-modules \
  --set scanning.enabled=true \
  --set scanning.providerScanning=true \
  --set scanning.cache.storageClassName=standard \
  --set scanning.cache.accessMode=ReadWriteOnce \
  --set monitoring.enabled=false \
  --wait
```

!!! note "Monitoring is disabled"
    By default, the chart creates a `ServiceMonitor`, which requires the
    Prometheus Operator CRDs. This Quickstart doesn't install Prometheus, so
    `monitoring.enabled=false` turns off the `ServiceMonitor` and the Stats
    page's Prometheus lookup. To enable monitoring later, see
    [Prometheus Monitoring](../helm-chart.md#prometheus-monitoring).

Wait for the workloads and CRDs to become ready:

```bash
kubectl get pods -n opendepot-system
kubectl get crds | grep opendepot
```

The installation pulls the released controller, server, and UI images. The version-controller image includes Trivy for module and provider scanning.

## Step 3: Launch OpenDepot Workshop

In a second terminal, forward local port 8080 to the UI and keep it running. If the port is in use, `kubectl` reports `unable to listen on any of the requested ports`.

```bash
kubectl port-forward -n opendepot-system svc/ui 8080:80
```

Open [http://localhost:8080](http://localhost:8080). Anonymous access is enabled,
so no login or OIDC configuration is required.

To reach the registry API directly, forward port 8081 to the server in another terminal:

```bash
kubectl port-forward -n opendepot-system svc/server 8081:80
```

The registry API is available at `https://opendepot.localtest.me:8081`. The
hostname resolves to `127.0.0.1`, and the mkcert certificate covers it.

## Step 4: Deploy the example Depot

Apply the [quickstart-depot.yaml](quickstart-depot.yaml) manifest below. Save it
locally first so you can inspect or edit the modules and provider before
deploying them:

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: ui-demo-depot
  namespace: opendepot-system
spec:
  global:
    moduleConfig:
      fileFormat: zip
      immutable: true
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
  moduleConfigs:
    - name: terraform-aws-acm
      provider: aws
      repoOwner: defdevio
      repoUrl: https://github.com/defdevio/terraform-aws-acm
      versionConstraints: ">= 0.0.0"
    - name: terraform-aws-api-gateway
      provider: aws
      repoOwner: defdevio
      repoUrl: https://github.com/defdevio/terraform-aws-api-gateway
      versionConstraints: ">= 0.0.0"
    - name: terraform-aws-cloudfront
      provider: aws
      repoOwner: defdevio
      repoUrl: https://github.com/defdevio/terraform-aws-cloudfront
      versionConstraints: ">= 0.0.0"
    - name: terraform-aws-ecr
      provider: aws
      repoOwner: defdevio
      repoUrl: https://github.com/defdevio/terraform-aws-ecr
      versionConstraints: ">= 0.0.0"
    - name: terraform-aws-iam
      provider: aws
      repoOwner: defdevio
      repoUrl: https://github.com/defdevio/terraform-aws-iam
      versionConstraints: ">= 0.0.0"
    - name: terraform-aws-lambda
      provider: aws
      repoOwner: defdevio
      repoUrl: https://github.com/defdevio/terraform-aws-lambda
      versionConstraints: ">= 0.0.0"
    - name: terraform-aws-s3
      provider: aws
      repoOwner: defdevio
      repoUrl: https://github.com/defdevio/terraform-aws-s3
      versionConstraints: ">= 0.0.0"
    - name: terraform-aws-secrets-manager
      provider: aws
      repoOwner: defdevio
      repoUrl: https://github.com/defdevio/terraform-aws-secrets-manager
      versionConstraints: ">= 0.0.0"
    - name: terraform-aws-ses
      provider: aws
      repoOwner: defdevio
      repoUrl: https://github.com/defdevio/terraform-aws-ses
      versionConstraints: ">= 0.0.0"
  pollingIntervalMinutes: 60
  providerConfigs:
    - name: aws
      architectures:
        - arm64
      operatingSystems:
        - darwin
        - linux
      versionConstraints: "~> 6.60.0"
```

```bash
kubectl apply -f quickstart-depot.yaml
```

The chart's `server.anonymousAuth=true` setting exposes the generated modules and providers through the anonymous browse API. No separate `Module` or `Provider` resources are needed. This Quickstart creates no `GroupBinding`. For OIDC deployments, create one scoped to your IdP group and the module and provider names users need.

Watch the controllers create and synchronize the generated resources:

```bash
kubectl get depots,modules,providers,versions -n opendepot-system
```

Open the **Depots** and **Modules** pages in OpenDepot Workshop to inspect relationships, versions, scan results, and provider metadata.

!!! note
    The first reconciliation downloads artifacts from GitHub and the upstream provider registry, so the catalog fills in over several minutes. The Depot polls every 60 minutes after the initial sync.

## Step 5: Configure OpenTofu for Modules and Providers

OpenDepot serves the module and provider artifacts locally, and the source addresses stay the same. Create a configuration that uses a module and the provider:

```bash
mkdir -p /tmp/opendepot-provider-test
cd /tmp/opendepot-provider-test

cat > main.tf <<'EOF'
terraform {
  required_providers {
    aws = {
      source  = "registry.opentofu.org/hashicorp/aws"
      version = "6.60.0"
    }
  }
}

module "s3" {
  source  = "opendepot.localtest.me:8081/opendepot-system/terraform-aws-s3/aws"
  version = ">= 0.0.0"
}
EOF

cat > .tofurc <<'EOF'
provider_installation {
  network_mirror {
    url     = "https://opendepot.localtest.me:8081/opendepot/providers/mirror/v1/opendepot-system/"
    include = ["registry.opentofu.org/*/*"]
  }

  direct {
    exclude = ["registry.opentofu.org/*/*"]
  }
}
EOF

TF_CLI_CONFIG_FILE=.tofurc tofu init
```

The module and provider archives now come from OpenDepot. The provider keeps its canonical `registry.opentofu.org/hashicorp/aws` identity, and the module source uses the OpenDepot host.

!!! note
    The mirror uses the mkcert certificate. `mkcert -install` adds its CA to your system trust store, so OpenTofu accepts the connection. In production, use a certificate from your organization's CA. See the [Providers guide](../guides/providers.md).

!!! note
    For Terraform, use the equivalent `.terraformrc` host configuration and run `terraform init`.

## Cleanup

Remove the Helm release and local namespace:

```bash
helm uninstall opendepot --namespace opendepot-system
kubectl delete namespace opendepot-system
```

Delete the Kind cluster when you are finished:

```bash
kind delete cluster --name opendepot
```
