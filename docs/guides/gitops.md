---
tags:
  - gitops
  - argocd
  - guides
---

# GitOps with Argo CD

!!! tip "Recommended for OIDC organizations"
    If your organization has OIDC enabled, this is the **recommended approach** for publishing modules. Developers push to Git; no pipeline ever holds cluster credentials, no ServiceAccount token bypasses GroupBinding, and every module version published maps to an approved pull request. This is the least-privilege publishing model and the most defensible posture in regulated or security-sensitive environments.

For teams that manage infrastructure declaratively through Git, OpenDepot fits naturally into a GitOps workflow with [Argo CD](https://argo-cd.readthedocs.io/). Instead of running `kubectl apply` from a CI pipeline, you check your `Module` manifests into a Git repository and let Argo CD sync them to the cluster.

**How it works:**

1. A developer opens a PR against their OpenTofu module repository with the code changes
2. The same PR includes an update to the OpenDepot `Module` manifest, adding the new version to `spec.versions`
3. The team reviews both the module code and the registry manifest in a single PR
4. On approval and merge, Argo CD detects the change and syncs the `Module` resource to the cluster
5. OpenDepot takes over — the Module controller creates a `Version` resource, and the Version controller fetches the archive from GitHub and uploads it to storage

This gives you a complete audit trail: every module version published to your registry maps to an approved, merged pull request.

**Example repository structure:**

```
opendepot-manifests/
├── modules/
│   └── terraform-aws-eks.yaml
└── kustomization.yaml
```

**Module manifest (`modules/terraform-aws-eks.yaml`):**

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
    immutable: true
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

**Argo CD Application:**

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-org-opendepot-modules
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/my-org/opendepot-manifests
    targetRevision: main
    path: modules
  destination:
    server: https://kubernetes.default.svc
    namespace: opendepot-system
  syncPolicy:
    automated:
      prune: false
      selfHeal: true
```

!!! tip
    Set `prune: false` so that Argo CD does not delete `Module` resources removed from Git — this prevents accidental module deletion. Use `selfHeal: true` so that any manual drift on the cluster is corrected back to the Git-declared state.

**Why this works well with OpenDepot:**

- **Single PR, full visibility** — module code and registry manifest are reviewed together
- **No cluster credentials in CI** — Argo CD handles authentication to the cluster; developers only push to Git
- **Immutable audit trail** — Git history records exactly who added each version and when
- **Declarative all the way down** — Git declares the desired state, Argo CD syncs it, and OpenDepot reconciles it to storage
- **GroupBinding fully enforced** — all human access still flows through OIDC and GroupBinding; no bypass path is introduced
- **Blast radius is Git, not the cluster** — a compromised pipeline runner can push a PR at worst; it cannot directly modify Kubernetes resources or reach the cluster API

## Least Privilege in Regulated Environments

When OIDC is enabled, the alternative to GitOps is enabling [ServiceAccount fallback](cicd.md#registry-reads-sa-fallback-with-oidc) so pipelines can authenticate alongside human users. That approach has a material trade-off: **SA tokens bypass GroupBinding entirely**. The SA's Kubernetes RBAC governs its access instead of the centralized, identity-aware GroupBinding model. In practice this means:

- You operate two separate access control systems — OIDC + GroupBinding for humans, RBAC for pipelines — which increases audit surface and operational overhead.
- A compromised pipeline token grants direct cluster API access, not just registry access.
- Access granted to an SA does not appear in GroupBinding audit logs.

For organizations subject to SOC 2, PCI DSS, FedRAMP, or similar frameworks where principle of least privilege and separation of duties are mandatory controls, GitOps removes these concerns entirely. The pipeline has no cluster credentials to compromise.
