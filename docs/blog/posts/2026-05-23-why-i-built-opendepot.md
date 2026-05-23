---
date: 2026-05-23
authors:
  - tonedefdev
categories:
  - OpenDepot
  - OpenTofu
  - Kubernetes
  - Open Source
description: >
  I got tired of paying JFrog for a secure OpenTofu/Terraform registry,
  so I built my own — here's the full story behind OpenDepot.
---

# I got tired of paying JFrog for a secure OpenTofu / Terraform registry so I built my own

[:fontawesome-brands-github: View on GitHub](https://github.com/tonedefdev/opendepot){ .md-button .md-button--primary }

It's a tale as old as time — you want to implement a secure, centralized storage system to easily distribute these awesome IaC modules that your team has developed, but you quickly find that enterprise-grade comes with an enterprise price. You could stick with the good ol' GitHub refs, but you soon realize this doesn't scale well. Delivering critical security updates to developers becomes a tedious process. You then think to yourself "if only I could use OpenTofu version constraints!" Those constraints, like the pessimistic version constraint `~> v1.0.0` for modules, make delivering security patches at scale significantly less challenging, however, you only get access to them through the registry protocol.

So you spend late-nights scouring GitHub and Reddit looking for open-source registry projects hoping that you don't have to "pay the piper." Before you know it, you've spent months implementing several different open-source systems only to find each one either had a painful deployment process, no turn-key migration path, missing key features, or inconsistent authentication. You feel defeated — you have deadlines, after all, so you decide to "pony up" and "pay the man" just for peace of mind so you can mark your feature done.

I, for one, hate surrendering to the corporate SaaS overlords in this manner! From that painful journey I put my poor team through, and the lessons I learned along the way, I realized this was an opportunity to give back to the open-source community. That's when I first came up with the idea for OpenDepot!

<!-- more -->

## The Solution

OpenDepot is an enterprise-grade OpenTofu / Terraform module and provider registry built entirely to be Kubernetes native. OpenDepot uses first-class Kubernetes primitives like Custom Resource Definitions and operators to streamline and modernize the module and provider pipeline. Instead of "pushing and praying" like I had to do with other registries, especially enterprise-grade solutions like Artifactory, OpenDepot is entirely declarative and offers administrators complete control over their supply chain.

## :material-source-branch: The GitOps Way

The preferred method to deliver a new module version is by using GitOps with ArgoCD / Flux. This allows you to keep your registry manifest in the same repo as the module itself. When it's time to update or add new features to your module, the same pull request process you already use for module code is now tied-in with its release process:

```txt
terraform-aws-eks/
└── opendepot/
    └── terraform-aws-eks.yaml
```

=== "OpenDepot Module"

    ```yaml
    apiVersion: opendepot.defdev.io/v1alpha1
    kind: Module
    metadata:
      name: terraform-aws-eks
      namespace: opendepot-system
    spec:
      moduleConfig:
        name: terraform-aws-eks
        provider: aws
        repoOwner: my-org
        repoUrl: https://github.com/my-org/terraform-aws-eks
        fileFormat: zip
        immutable: false
        storageConfig:
          s3:
            bucket: my-org-opendepot-modules
            region: us-west-2
        githubClientConfig:
          useAuthenticatedClient: true
      versions:
        - version: "21.10.1"
        - version: "21.11.0"
        - version: "21.12.0"
        - version: "21.13.0"   # added in PR #42
    ```

=== "ArgoCD Application"

    ```yaml
    apiVersion: argoproj.io/v1alpha1
    kind: Application
    metadata:
      name: terraform-aws-eks
      namespace: argocd
    spec:
      project: default
      source:
        repoURL: https://github.com/my-org/terraform-aws-eks
        targetRevision: main
        path: opendepot
      destination:
        server: https://kubernetes.default.svc
        namespace: opendepot-system
      syncPolicy:
        automated:
          prune: false
          selfHeal: true
    ```

The release workflow then looks like this:

1. A developer opens a PR against their OpenTofu module repository with the code changes
2. The same PR includes an update to the OpenDepot Module manifest, adding the new version to `spec.versions`
3. The team reviews both the module code and the registry manifest in a single PR
4. On approval and merge, Argo CD detects the change and syncs the `Module` resource to the cluster
5. OpenDepot takes over — the Module controller creates a `Version` resource, and the Version controller fetches the archive from GitHub and uploads it to storage. Once completed, a SHA256 checksum of the archive is stored in the `status` field before marking the Version as synced.

!!! tip
    You can also use a centralized repo that hosts all your OpenDepot manifests, with a single ArgoCD application that gives you a visual overview of your entire registry.

## :material-cloud-upload: Storage Configuration

OpenDepot supports all three major cloud provider storage backends as well as local filesystem storage:

=== ":fontawesome-brands-aws: AWS S3"

    ```yaml
    storageConfig:
      s3:
        bucket: opendepot-modules
        region: us-west-2
    ```

=== ":material-microsoft-azure: Azure Blob"

    ```yaml
    storageConfig:
      azureBlob:
        accountName: opendepotmodules
        accountUrl: https://opendepotmodules.blob.core.windows.net
        subscriptionID: 00000000-0000-0000-0000-000000000000
        resourceGroup: opendepot-rg
    ```

=== ":material-google-cloud: Google Cloud"

    ```yaml
    storageConfig:
      gcs:
        bucket: opendepot-modules
    ```

=== ":material-harddisk: Filesystem"

    ```yaml
    storageConfig:
      filesystem:
        path: /data/opendepot
    ```

!!! tip "Extensible by Design"
    I designed OpenDepot to leverage a Go interface for storage. Adding and testing new providers is straightforward — provide a concrete implementation for the interface, update the API, regenerate new CRDs, and you're ready to start testing. See [CONTRIBUTING.md](https://github.com/tonedefdev/opendepot/blob/main/CONTRIBUTING.md) for more details.

**Pre-signed URLs** allow you to offload large egress costs (AWS providers can be ~700MB) by redirecting clients to pull directly from cloud storage instead of proxying through your infrastructure. Configure per-module, per-provider, or globally through the Depot:

```yaml
storageConfig:
  s3:
    bucket: opendepot-providers
    region: us-west-2
  presign:
    enabled: true
    ttl: "15m"
    fallbackToProxy: true
```

!!! info "Fallback Behaviour"
    When `fallbackToProxy` is `true`, if a pre-signed URL cannot be generated the server proxies the download itself. Set it to `false` to enforce that all downloads always use pre-signed URLs and never pass through your infrastructure.

**Filesystem storage** is backed by a Kubernetes Persistent Volume with any `StorageClass` that supports `ReadWriteMany`. The Version controller needs to write artifacts to the same volume the Server serves them from — hence the `ReadWriteMany` requirement.

!!! note "Init Container Privileges"
    On startup, an init container runs as root to `chown`/`chgrp` the directory mount so that user/group `65532` (the user the containers run as) can read/write to it. This is the only point where elevated privileges are required — otherwise, OpenDepot runs as non-root across the board.

## :material-refresh: The Depot (Pull-Based)

If you don't follow a GitOps process, no worries! The Depot resource allows you to pull down modules and providers using version constraints, creating a private mirror for public providers with a fully defined release process:

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: my-team-depot
  namespace: opendepot-system
spec:
  global:
    githubClientConfig:
      useAuthenticatedClient: true
    moduleConfig:
      fileFormat: zip
      immutable: false
    storageConfig:
      s3:
        bucket: opendepot-registry
        region: us-west-2
  moduleConfigs:
    - name: terraform-aws-eks
      provider: aws
      repoOwner: terraform-aws-modules
      versionConstraints: ">= 21.10.1, != 21.13.0"
    - name: terraform-azurerm-aks
      provider: azurerm
      repoOwner: azure
      versionConstraints: ">= 10.0.0"
  providerConfigs:
    - name: aws
      operatingSystems:
        - linux
        - darwin
      architectures:
        - amd64
        - arm64
      versionConstraints: ">= 5.80.0, < 6.0.0"
      storageConfig:
        s3:
          bucket: opendepot-registry
          region: us-west-2
  pollingIntervalMinutes: 60
```

This Depot will:

1. Query the `terraform-aws-modules/terraform-aws-eks` and `azure/terraform-azurerm-aks` GitHub repositories for releases
2. Filter releases matching the version constraints and create `Module` resources
3. Query the HashiCorp Releases API for the `aws` provider and create a `Provider` resource for matching versions
4. The Module and Provider controllers create `Version` resources for each discovered version and OS/architecture
5. The Version controller fetches archives from GitHub (modules) or HashiCorp (providers) and uploads them to the S3 bucket
6. Re-check for new releases every 60 minutes

Since your registry configuration is codified via the Depot, it now follows the same review process as other services in your stack!

!!! tip "Migrating from an Existing Registry"
    The Depot is a very handy migration tool. Point it at your GitHub repos with a version constraint that covers your existing versions, let it ingest everything, then delete the Depot. Removing the Depot resource does **not** delete any Modules or Providers — it's simply a centralized interface to ingest multiple artifacts.

## :material-sync: The CI/CD Workflow (Push-based)

You also have the option for an entirely push-based CI/CD workflow:

=== "OpenDepot Manifest"

    ```yaml
    apiVersion: opendepot.defdev.io/v1alpha1
    kind: Module
    metadata:
      name: terraform-aws-eks
      namespace: opendepot-system
    spec:
      moduleConfig:
        name: terraform-aws-eks
        provider: aws
        repoOwner: terraform-aws-modules
        repoUrl: https://github.com/terraform-aws-modules/terraform-aws-eks
        fileFormat: zip
        immutable: true
        storageConfig:
          s3:
            bucket: opendepot-modules
            region: us-west-2
        githubClientConfig:
          useAuthenticatedClient: true
      versions:
        - version: "21.10.1"
        - version: "21.11.0"
        - version: "21.12.0"
        - version: "21.13.0"  # added in PR #42
    ```

=== "GitHub Actions Workflow"

    ```yaml
    name: Publish Module Version

    on:
      release:
        types: [published]

    jobs:
      publish:
        runs-on: ubuntu-latest
        steps:
          - uses: actions/checkout@v4

          - name: Configure AWS credentials
            uses: aws-actions/configure-aws-credentials@v4
            with:
              role-to-assume: arn:aws:iam::<AWS_ACCOUNT_ID>:role/opendepot-github-actions-role
              aws-region: us-west-2

          - name: Setup kubeconfig
            run: aws eks update-kubeconfig --name my-cluster --region us-west-2

          - name: Publish module version
            run: |
              kubectl apply -f "opendepot/terraform-aws-eks.yaml"
    ```

## :material-shield-check: Security Features

### :material-check-decagram: Checksum Validation Every Reconcile

Kubernetes operators constantly reconcile resources and act on changes to any resources they manage. When a module version is being added, all previous versions are also reconciled. To ensure OpenDepot is not re-downloading providers or modules every reconciliation loop, the Version resource stores a `status.checksum` of each archive.

If the checksum metadata in storage doesn't match this field, the controller first attempts to pull it from source and revalidate the checksum. If the checksum from source is still not a match, the controller stops reconciling and emits errors.

!!! danger "Tamper Protection"
    The Server will not serve any modules whose checksums do not match. The Version controller continuously reconciles storage so that any tampered archive is re-fetched and restored to its known-good state.

### :material-key: GPG Keys

OpenDepot supports GPG signing for providers. When serving provider binaries, the registry protocol requires that each binary is accompanied by a SHA256 checksum file and a GPG signature so that OpenTofu and Terraform can verify the integrity of what they download. Configure OpenDepot with your GPG key via a Kubernetes Secret referenced by `server.gpg.secretName` in the Helm chart. Once set, the Server automatically signs provider checksum files on the fly with your private key. Clients that have your public key in their trust store can verify every provider binary they pull is untampered and came from your registry.

### :material-magnify-scan: Trivy Vulnerability Scans

OpenDepot has the option to perform security scans using a separate Version controller image that comes bundled with Trivy. Trivy will perform a configuration scan of modules and store findings in the `module.status` field. For providers, Trivy will scan the binary for each operating system and architecture, and OpenDepot will attempt to find and scan the source code, deduplicate findings, then store each in the `provider.status` field.

!!! warning "Blocking Policy"
    You can configure OpenDepot to block `CRITICAL` and `HIGH` vulnerabilities to ensure that only modules and providers with a good security posture can be reconciled and stored in your registry.

### :material-login-variant: Dex OIDC Integration

OpenDepot's Helm chart bundles [Dex](https://dexidp.io/) as a subchart to handle OIDC authentication with an upstream IdP like Entra ID, GitHub, Okta, and many more. This is the recommended authentication method since it doesn't require cluster access or expose endpoints used to modify resources.

With OIDC enabled you can leverage fine-grained access control through `GroupBinding` custom resources. Use the [Expr](https://expr-lang.org/) language to bind the `groups` claim in a user's JWT to specific modules or providers. The `moduleResources` field also supports glob patterns:

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: "01-aws-platform-team"
  namespace: opendepot-system
spec:
  expression: '"aws-platform-team" in groups'
  moduleResources:
    - "terraform-aws-*"
  providerResources:
    - "aws"
```

!!! tip "Native `tofu login` Support"
    OIDC with Dex is the **only** method that supports the native `tofu login opendepot.defdev.io` command.

### :material-shield-key: Other Authentication Methods

- **Kubernetes service account token** — Use Kubernetes RBAC permissions to control access per module or provider. Bypasses GroupBinding in favor of native Kubernetes RBAC.
- **Base64-encoded kubeconfig** — Convenient for local kind clusters.
- **Anonymous auth** — Enable via a single Helm chart flag to host a public registry. The Server's own Service Account is used for fetching, and clients don't need an access token.

!!! warning "Kubeconfig — Local Use Only"
    A base64-encoded Kubernetes kubeconfig should **never** be used in production. It is convenient for local `kind` cluster testing only.

## :material-download: Fetching Artifacts

The Server implements both the Module and Provider Registry protocols so that OpenTofu and Terraform can use OpenDepot as a drop-in registry. Crucially, the Server is **completely read-only** — it provides no endpoints that allow modifications. All changes to resources require strict Kubernetes access.

Reference your modules and providers in code, then run `tofu init`:

=== "Module"

    ```hcl
    module "eks" {
      source  = "opendepot.defdev.io/opendepot-system/terraform-aws-key-pair/aws"
      version = "~> 21.0.0"
    }
    ```

=== "Provider"

    ```hcl
    terraform {
      required_providers {
        aws = {
          source  = "opendepot.defdev.io/opendepot-system/aws"
          version = "~> 5.80"
        }
        azurerm = {
          source  = "opendepot.defdev.io/opendepot-system/azurerm"
          version = ">= 4.0.0"
        }
      }
    }
    ```

Configure your `.tofurc` to point at OpenDepot:

```hcl
host "opendepot.defdev.io" {
  services = {
    "modules.v1"   = "https://opendepot.defdev.io/opendepot/modules/v1/"
    "providers.v1" = "https://opendepot.defdev.io/opendepot/providers/v1/"
  }
}
```

With Dex configured, the full `tofu login` + `tofu init` flow looks like this:

```text
$ tofu login opendepot.defdev.io
$ tofu init

Initializing the backend...
Initializing modules...
Downloading opendepot.defdev.io/opendepot-system/terraform-aws-key-pair/aws 2.0.3 for key_pair...
- key_pair in .terraform/modules/key_pair

Initializing provider plugins...

OpenTofu has been successfully initialized!

You may now begin working with OpenTofu. Try running "tofu plan" to see
any changes that are required for your infrastructure. All OpenTofu commands
should now work.

If you ever set or change modules or backend configuration for OpenTofu,
rerun this command to reinitialize your working directory. If you forget, other
commands will detect it and remind you to do so if necessary.
```

That's all there is to it on the consuming side! It's simple and easy to get started with OpenDepot! I ask that you try it today and share your experiences. If you have any questions, see any issues, or just want to talk about Cloud Native tooling in general — feel free to reach out to me anytime!

---

<div class="grid cards" markdown>

- :material-book-open-variant: &nbsp;[__Full Documentation__](https://tonedefdev.github.io/opendepot/docs)

    ---

    Everything you need to get set up, configured, and running your own registry.

- :material-laptop: &nbsp;[__Local Quickstart__](../../getting-started/quickstart.md)

    ---

    Run a fully functional registry on your laptop with `kind` in minutes — no cloud account needed.

- :material-rocket-launch: &nbsp;[__Installation Guide__](../../getting-started/installation.md)

    ---

    Deploy OpenDepot to your cluster with Helm.

</div>
