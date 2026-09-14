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

Run OpenDepot locally with the released Helm chart and its publicly available
container images. This Quickstart creates a Kind cluster, installs the complete
registry experience, and uses anonymous access so you can start exploring
without configuring an identity provider.

The main event is one `Depot` resource. It discovers a useful set of AWS
modules and the AWS provider from upstream, then creates and reconciles the
corresponding OpenDepot resources automatically.

## Prerequisites

Install [Docker](https://docs.docker.com/get-docker/),
[Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation),
[`kubectl`](https://kubernetes.io/docs/tasks/tools/), and
[Helm 3](https://helm.sh/docs/intro/install/).

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

Install the released chart with the features used in this Quickstart:

```bash
helm install opendepot opendepot/opendepot \
  --namespace opendepot-system \
  --set server.anonymousAuth=true \
  --set server.service.type=ClusterIP \
  --set provider.enabled=true \
  --set depot.enabled=true \
  --set ui.enabled=true \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.hostPath=/var/local/opendepot-modules \
  --set scanning.enabled=true \
  --set scanning.providerScanning=true \
  --set scanning.cache.storageClassName=standard \
  --set scanning.cache.accessMode=ReadWriteOnce \
  --wait
```

Wait for the workloads and CRDs to become ready:

```bash
kubectl get pods -n opendepot-system
kubectl get crds | grep opendepot
```

The installation pulls the released controller, server, UI, and scanning
images.

## Step 3: Open the Registry Explorer

In a second terminal, forward the UI service:

```bash
kubectl port-forward -n opendepot-system svc/ui 8080:80
```

Open [http://localhost:8080](http://localhost:8080). Anonymous access is
enabled for this Quickstart, so no login or OIDC configuration is required.

To access the registry API directly, forward the server service in another
terminal:

```bash
kubectl port-forward -n opendepot-system svc/server 8081:80
```

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
---
apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: quickstart-public-access
  namespace: opendepot-system
spec:
  expression: "true"
  moduleResources:
    - "*"
  providerResources:
    - "*"
```

```bash
kubectl apply -f quickstart-depot.yaml
```

The `GroupBinding` makes the generated modules and providers visible through
the anonymous browse API. No separate `Module` or `Provider` resources are
needed.

Watch the controllers create and synchronize the generated resources:

```bash
kubectl get depots,modules,providers,versions -n opendepot-system
```

Open the **Depots** and **Modules** pages in the Registry Explorer to inspect
the relationships, versions, scan results, and provider metadata as they become
available.

!!! note
    The first reconciliation downloads artifacts from GitHub and the upstream
    provider registry, so the catalog fills in over several minutes. The Depot
    continues polling every 60 minutes after the initial sync.

## Step 5: Configure OpenTofu for Modules and Providers

The Depot keeps the module and provider source addresses familiar to OpenTofu
while OpenDepot serves both kinds of artifact locally. Add a module reference
to `main.tf`:

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

  module "s3" {
    source  = "opendepot.localtest.me:8081/opendepot-system/terraform-aws-s3/aws"
    version = ">= 0.0.0"
  }
}
EOF

cat > .tofurc <<'EOF'
host "opendepot.localtest.me:8081" {
  services = {
    "modules.v1" = "http://opendepot.localtest.me:8081/opendepot/modules/v1/"
  }
}

provider_installation {
  network_mirror {
    url     = "http://opendepot.localtest.me:8081/opendepot/providers/mirror/v1/opendepot-system/"
    include = ["registry.opentofu.org/*/*"]
  }
  direct {
    exclude = ["registry.opentofu.org/*/*"]
  }
}
EOF

TF_CLI_CONFIG_FILE=.tofurc tofu init
```

The module and provider archives now come from OpenDepot. The provider retains
its canonical `registry.opentofu.org/hashicorp/aws` identity, while the module
uses the local OpenDepot host in its source address.

!!! note
    For Terraform, use the equivalent `.terraformrc` host configuration and
    run `terraform init`.

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
