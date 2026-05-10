# -----------------------------
# Fixed values for your project
# -----------------------------
PROJECT_ID="opendepot-495604"
PROJECT_NUMBER="789629963060"
POOL_ID="github-oidc-pool"
PROVIDER_ID="github-provider"
REPO="tonedefdev/opendepot"
WORKFLOW_PATH=".github/workflows/storage-integration.yaml"
SA_EMAIL="opendepot-test@opendepot-495604.iam.gserviceaccount.com"

# Optional sanity check: pool exists
gcloud iam workload-identity-pools list \
  --project="${PROJECT_ID}" \
  --location="global" \
  --format="table(name,displayName,state)"

# Create provider (use POOL ID only, not full resource path)
gcloud iam workload-identity-pools providers create-oidc "${PROVIDER_ID}" \
  --project="${PROJECT_ID}" \
  --location="global" \
  --workload-identity-pool="${POOL_ID}" \
  --display-name="GitHub Provider" \
  --issuer-uri="https://token.actions.githubusercontent.com" \
  --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository,attribute.event_name=assertion.event_name,attribute.base_ref=assertion.base_ref,attribute.workflow_ref=assertion.workflow_ref,attribute.aud=assertion.aud" \
  --attribute-condition="assertion.repository=='${REPO}' && assertion.event_name=='pull_request' && assertion.base_ref=='main' && assertion.workflow_ref.startsWith('${REPO}/${WORKFLOW_PATH}@')"

# If it already exists, run update instead
gcloud iam workload-identity-pools providers update-oidc "${PROVIDER_ID}" \
  --project="${PROJECT_ID}" \
  --location="global" \
  --workload-identity-pool="${POOL_ID}" \
  --attribute-mapping="google.subject=assertion.sub,attribute.repository=assertion.repository,attribute.event_name=assertion.event_name,attribute.base_ref=assertion.base_ref,attribute.workflow_ref=assertion.workflow_ref,attribute.aud=assertion.aud" \
  --attribute-condition="assertion.repository=='${REPO}' && assertion.event_name=='pull_request' && assertion.base_ref=='main' && assertion.workflow_ref.startsWith('${REPO}/${WORKFLOW_PATH}@')"

# Allow identities from this repo (provider condition above does the stricter filtering)
gcloud iam service-accounts add-iam-policy-binding "${SA_EMAIL}" \
  --project="${PROJECT_ID}" \
  --role="roles/iam.workloadIdentityUser" \
  --member="principalSet://iam.googleapis.com/projects/${PROJECT_NUMBER}/locations/global/workloadIdentityPools/${POOL_ID}/attribute.repository/${REPO}"

# Print the provider resource name to place in GitHub workflow
echo "projects/${PROJECT_NUMBER}/locations/global/workloadIdentityPools/${POOL_ID}/providers/${PROVIDER_ID}"
