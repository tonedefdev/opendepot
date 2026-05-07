# Storage Integration Tests

End-to-end tests for the `pkg/storage` backends. Each test provisions real cloud
infrastructure using [OpenTofu](https://opentofu.org/) (managed by
[Terratest](https://terratest.gruntwork.io/)), runs a full CRUD + presign
exercise against the live backend, then destroys the infrastructure — all within
a single `go test` invocation.

## Prerequisites

| Tool | Version | Install |
|---|---|---|
| Go | 1.25+ | <https://go.dev/dl/> |
| OpenTofu | 1.11+ | `brew install opentofu` |
| AWS CLI / credentials | any | see [AWS](#aws-s3) section |
| gcloud CLI | any | `brew install --cask google-cloud-sdk` |

Tests are gated behind the `integration` build tag and never run during normal
`go test ./...`.

---

## Running the tests

All commands are run from the `test/integration/` directory with `GOWORK=off` to
avoid a transitive dependency conflict in the Go workspace module graph.

### AWS S3

**Credentials:** Any credential source that the AWS SDK default chain
recognizes works — environment variables, `~/.aws/credentials`, SSO, or an
instance profile.

```bash
export AWS_REGION=us-east-1        # required
export AWS_PROFILE=my-profile      # optional, if not using default

cd test/integration
GOWORK=off go test -tags=integration -v -count=1 -timeout 15m . -run TestS3StorageIntegration
```

**Required IAM permissions** on the principal running the tests:

```
s3:CreateBucket
s3:DeleteBucket
s3:PutObject
s3:GetObject
s3:DeleteObject
s3:ListBucket
s3:GetBucketLocation
s3:PutBucketPublicAccessBlock
s3:GetBucketPublicAccessBlock
s3:PutEncryptionConfiguration
s3:GetEncryptionConfiguration
s3:PutBucketTagging
s3:GetBucketTagging
```

Scope these to `arn:aws:s3:::opendepot-integration-*` and
`arn:aws:s3:::opendepot-integration-*/*`.

---

### GCS (Google Cloud Storage)

**Credentials:** Application Default Credentials (ADC) are used. Log in with:

```bash
gcloud auth application-default login
```

```bash
export GCP_PROJECT=my-gcp-project  # required
export GCP_LOCATION=US             # optional, defaults to US

cd test/integration
GOWORK=off go test -tags=integration -v -count=1 -timeout 15m . -run TestGCSStorageIntegration
```

**Required IAM permissions** on the principal running the tests:

```
storage.buckets.create
storage.buckets.delete
storage.buckets.get
storage.buckets.list
storage.objects.create
storage.objects.delete
storage.objects.get
storage.objects.list
```

The built-in role `roles/storage.admin` covers all of the above. Scope it to
the project or restrict it to buckets matching `opendepot-integration-*`.

#### PresignObject and the `authorized_user` credential limitation

`PresignObject` generates a V4 signed URL, which requires permissions to sign
requests. By default, plain user OAuth2 credentials (`authorized_user`) obtained
via `gcloud auth application-default login` do not have signing permissions —
this sub-test is automatically skipped when such credentials are detected.

To run `PresignObject` locally, grant your user account the necessary IAM
permissions:

```bash
export GCP_PROJECT=my-gcp-project
export SA_EMAIL=opendepot-test@$GCP_PROJECT.iam.gserviceaccount.com
export USER_EMAIL=your-email@example.com

# Allow your user to impersonate the service account
gcloud iam service-accounts add-iam-policy-binding $SA_EMAIL \
  --member=user:$USER_EMAIL \
  --role=roles/iam.serviceAccountUser \
  --project=$GCP_PROJECT

# Allow your user to generate tokens (for signing operations)
gcloud iam service-accounts add-iam-policy-binding $SA_EMAIL \
  --member=user:$USER_EMAIL \
  --role=roles/iam.serviceAccountTokenCreator \
  --project=$GCP_PROJECT
```

Then authenticate normally:

```bash
gcloud auth application-default login
```

Re-run the tests. The `credsSupportSigning()` check will now detect that your
user has signing permissions and the `PresignObject` sub-test will execute.

---

### Azure Blob Storage

**Credentials:** `DefaultAzureCredential` is used. Log in with the Azure CLI:

```bash
az login
az account set --subscription SUBSCRIPTION_ID
```

```bash
export AZURE_SUBSCRIPTION_ID=00000000-0000-0000-0000-000000000000  # required
export AZURE_LOCATION="West US 2"                                  # optional, defaults to "West US 2"

cd test/integration
GOWORK=off go test -tags=integration -v -count=1 -timeout 15m . -run TestAzureBlobStorageIntegration
```

**Required RBAC roles** on the storage account or subscription:

| Role | Purpose |
|---|---|
| `Storage Blob Data Contributor` | PutObject, GetObject, DeleteObject |
| `Storage Blob Delegator` | PresignObject (User Delegation SAS) |
| `Contributor` (resource group scope) | OpenTofu: create/destroy resource group and storage account |

---

## Running all backends

```bash
cd test/integration
GOWORK=off go test -tags=integration -v -count=1 -timeout 30m .
```

---

## CI

Tests run automatically on pull requests that touch `pkg/storage/**` or
`test/**`, and can be triggered manually via **Actions → Storage Integration
Tests → Run workflow**.

The CI workflow lives at
[.github/workflows/storage-integration.yaml](../.github/workflows/storage-integration.yaml).

### Required GitHub repository secrets

#### S3

| Secret | Description |
|---|---|
| `AWS_ROLE_ARN` | ARN of the IAM role to assume via OIDC |
| `AWS_REGION` | AWS region for the test bucket (e.g. `us-east-1`) |

The IAM role trust policy must allow:
```json
{
  "StringLike": {
    "token.actions.githubusercontent.com:sub": "repo:tonedefdev/opendepot:*"
  }
}
```

#### GCS

| Secret | Description |
|---|---|
| `GCP_WORKLOAD_IDENTITY_PROVIDER` | Full resource name of the Workload Identity Provider |
| `GCP_SERVICE_ACCOUNT` | Email of the service account to impersonate |
| `GCP_PROJECT` | GCP project ID (e.g. `opendepot-495604`) |
| `GCP_LOCATION` | GCS bucket location (e.g. `US`). Defaults to `US` if omitted |

The Workload Identity Pool binding must allow:
```
attribute.repository/tonedefdev/opendepot
```

The service account needs `roles/storage.admin` on the project (or scoped to
`opendepot-integration-*` buckets) and `roles/iam.serviceAccountTokenCreator`
on itself to generate signed URLs.

#### Azure

| Secret | Description |
|---|---|
| `AZURE_CLIENT_ID` | Client ID of the app registration with federated credential |
| `AZURE_TENANT_ID` | Azure AD tenant ID |
| `AZURE_SUBSCRIPTION_ID` | Azure subscription ID |
| `AZURE_LOCATION` | Azure region (e.g. `West US 2`). Defaults to `West US 2` if omitted |

The app registration must have a federated credential with subject
`repo:tonedefdev/opendepot:*`. Grant the service principal `Contributor` on
the target subscription (or resource group) plus `Storage Blob Data Contributor`
and `Storage Blob Delegator` on the storage account once it is created.

---

## Infrastructure

OpenTofu modules under `test/infra/` are managed entirely by Terratest — you do
not need to run `tofu` commands manually.

| Directory | Cloud | Resources created |
|---|---|---|
| `test/infra/s3/` | AWS | `aws_s3_bucket` — SSE-S3 encryption, public access blocked |
| `test/infra/gcp/` | GCP | `google_storage_bucket` — uniform bucket-level access |
| `test/infra/azure/` | Azure | `azurerm_resource_group` + `azurerm_storage_account` — Standard LRS |

Each bucket is named `opendepot-integration-<nanosecond-timestamp>` and has
`force_destroy = true` so cleanup always succeeds even if objects remain.
