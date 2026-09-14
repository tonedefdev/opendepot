---
tags:
  - authentication
  - kubernetes
  - ci-cd
---

# Managed Cluster Tokens

Use a token issued by the managed Kubernetes provider when a CI job already
has access to the cluster API. OpenDepot forwards the token to Kubernetes; if
the API server accepts it, registry reads and module downloads work without Dex
credentials.

## Required Server Configuration

Use one of these modes:

- `server.useBearerToken: true` for pure bearer-token authentication.
- `server.oidc.allowServiceAccountFallback: true` when OIDC is primary but
  non-OIDC tokens should be forwarded to Kubernetes.

If OIDC is enabled without ServiceAccount fallback, managed-cluster tokens are
rejected because they are not Dex-issued JWTs.

## Registry Token Variable

OpenTofu and Terraform derive the environment variable name from the registry
hostname. Replace dots with underscores and convert the result to uppercase.

`opendepot.defdev.io` becomes `TF_TOKEN_OPENDEPOT_DEFDEV_IO`.

=== "Amazon EKS"

    ```bash
    export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(aws eks get-token \
      --cluster-name my-cluster \
      --region us-west-2 \
      --output json | jq -r '.status.token')

    tofu init
    tofu plan
    ```

=== "Azure AKS"

    ```bash
    export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(az account get-access-token \
      --resource 6dae42f8-4368-4678-94ff-3960e28e3630 \
      --query accessToken -o tsv)

    tofu init
    tofu plan
    ```

=== "Google GKE"

    ```bash
    export TF_TOKEN_OPENDEPOT_DEFDEV_IO=$(gcloud auth print-access-token)

    tofu init
    tofu plan
    ```

Tokens are short-lived and rotate automatically. This is the preferred option
for CI/CD jobs when the cluster accepts the provider-issued token.

## CI/CD Example

```yaml
name: Apply Infrastructure

on:
  push:
    branches: [main]

jobs:
  apply:
    runs-on: ubuntu-latest
    permissions:
      id-token: write
    steps:
      - uses: actions/checkout@v4

      - name: Configure AWS credentials
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::ACCOUNT_ID:role/github-actions-role
          aws-region: us-west-2

      - name: Setup OpenTofu
        uses: opentofu/setup-opentofu@v1

      - name: Set registry token
        run: |
          TOKEN=$(aws eks get-token --cluster-name my-cluster --region us-west-2 \
            --output json | jq -r '.status.token')
          echo "TF_TOKEN_OPENDEPOT_DEFDEV_IO=$TOKEN" >> $GITHUB_ENV

      - run: tofu init
      - run: tofu plan
```

For a pipeline-specific comparison with Dex Client Credentials and
ServiceAccount tokens, see [CI/CD Pipelines](../guides/cicd.md).
