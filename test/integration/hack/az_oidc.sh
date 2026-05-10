# ---------- Required inputs ----------
SUBSCRIPTION_ID="7d069d72-e281-4e33-911b-aedadcb4f773"
APP_NAME="opendepot-github-oidc"
REPO="tonedefdev/opendepot"

# Scope where tests need access (pick one):
# 1) Subscription-wide:
RESOURCE_SCOPE="/subscriptions/${SUBSCRIPTION_ID}"
# 2) Or tighter scope, for example a resource group:
# RESOURCE_SCOPE="/subscriptions/${SUBSCRIPTION_ID}/resourceGroups/YOUR_RG"

# ---------- Login / context ----------
az login
az account set --subscription "${SUBSCRIPTION_ID}"

TENANT_ID="$(az account show --query tenantId -o tsv)"

# ---------- Create Entra app + service principal ----------
APP_CLIENT_ID="$(az ad app create --display-name "${APP_NAME}" --query appId -o tsv)"
APP_OBJECT_ID="$(az ad app show --id "${APP_CLIENT_ID}" --query id -o tsv)"
SP_OBJECT_ID="$(az ad sp create --id "${APP_CLIENT_ID}" --query id -o tsv)"

# ---------- Federated credential for pull_request ----------
cat > fic-pr.json <<EOF
{
  "name": "github-pr",
  "issuer": "https://token.actions.githubusercontent.com",
  "subject": "repo:${REPO}:pull_request",
  "description": "GitHub Actions PR OIDC",
  "audiences": ["api://AzureADTokenExchange"]
}
EOF

az ad app federated-credential create \
  --id "${APP_OBJECT_ID}" \
  --parameters fic-pr.json

# ---------- Optional: allow workflow_dispatch from main ----------
cat > fic-main.json <<EOF
{
  "name": "github-main-ref",
  "issuer": "https://token.actions.githubusercontent.com",
  "subject": "repo:${REPO}:ref:refs/heads/main",
  "description": "GitHub Actions main-branch OIDC (workflow_dispatch/push style ref token)",
  "audiences": ["api://AzureADTokenExchange"]
}
EOF

az ad app federated-credential create \
  --id "${APP_OBJECT_ID}" \
  --parameters fic-main.json

# ---------- RBAC (least privilege: choose roles you actually need) ----------
# Example commonly needed for storage tests:
az role assignment create \
  --assignee-object-id "${SP_OBJECT_ID}" \
  --assignee-principal-type ServicePrincipal \
  --role "Storage Blob Data Contributor" \
  --scope "${RESOURCE_SCOPE}"

# Often also needed for control-plane reads:
az role assignment create \
  --assignee-object-id "${SP_OBJECT_ID}" \
  --assignee-principal-type ServicePrincipal \
  --role "Reader" \
  --scope "${RESOURCE_SCOPE}"

# ---------- Values to put in GitHub workflow/vars ----------
echo "AZURE_CLIENT_ID=${APP_CLIENT_ID}"
echo "AZURE_TENANT_ID=${TENANT_ID}"
echo "AZURE_SUBSCRIPTION_ID=${SUBSCRIPTION_ID}"
