#!/bin/sh
set -eu

umask 077
secrets_dir=$(mktemp -d -t opendepot-secrets)
trap 'rm -rf "$secrets_dir"' EXIT

namespace=opendepot-system
anonymous_auth=${OPENDEPOT_DEV_ANONYMOUS_AUTH:-false}

if [ -z "${OPENDEPOT_DEV_PASSWORD:-}" ]; then
  echo "OPENDEPOT_DEV_PASSWORD is required for the local Dex user" >&2
  exit 1
fi

case "$anonymous_auth" in
  true|false) ;;
  *)
    echo "OPENDEPOT_DEV_ANONYMOUS_AUTH must be true or false" >&2
    exit 1
    ;;
esac

if command -v htpasswd >/dev/null 2>&1; then
  password_hash=$(htpasswd -bnBC 10 "" "$OPENDEPOT_DEV_PASSWORD" | tr -d ':\n')
else
  password_hash=$(python3 -c 'import bcrypt, os; print(bcrypt.hashpw(os.environ["OPENDEPOT_DEV_PASSWORD"].encode(), bcrypt.gensalt(10)).decode())')
fi

kubectl create namespace "$namespace" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

if ! kubectl get secret ui-session-secret --namespace "$namespace" >/dev/null 2>&1; then
  session_password=$(openssl rand -base64 48)
  printf '%s' "$session_password" > "$secrets_dir/sessionPassword"
  kubectl create secret generic ui-session-secret \
    --from-file=sessionPassword="$secrets_dir/sessionPassword" \
    --namespace "$namespace" >/dev/null
fi

if ! kubectl get secret ui-oidc-secret --namespace "$namespace" \
  -o jsonpath='{.data.clientSecret}{" "}{.data.OPENDEPOT_UI_CLIENT_SECRET}' 2>/dev/null \
  | grep -Eq '^[^ ]+ [^ ]+$'; then
  ui_client_secret=$(openssl rand -base64 48)
  printf '%s' "$ui_client_secret" > "$secrets_dir/ui_client_secret"
  kubectl create secret generic ui-oidc-secret \
    --from-file=clientSecret="$secrets_dir/ui_client_secret" \
    --from-file=OPENDEPOT_UI_CLIENT_SECRET="$secrets_dir/ui_client_secret" \
    --namespace "$namespace" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
fi

if ! kubectl get secret opendepot-provider-gpg --namespace "$namespace" >/dev/null 2>&1; then
  gpg_home="$secrets_dir/gpg"
  mkdir -m 700 "$gpg_home"

  cat > "$gpg_home/keygen.conf" <<'EOF'
%no-protection
Key-Type: RSA
Key-Length: 2048
Name-Real: OpenDepot Local
Name-Email: opendepot@local.test
Expire-Date: 0
%commit
EOF

  GNUPGHOME="$gpg_home" gpg --batch --gen-key "$gpg_home/keygen.conf" >/dev/null 2>&1
  key_id=$(GNUPGHOME="$gpg_home" gpg --list-keys --with-colons | awk -F: '/^fpr/{print $10; exit}')
  ascii_armor=$(GNUPGHOME="$gpg_home" gpg --armor --export "$key_id")
  private_key=$(GNUPGHOME="$gpg_home" gpg --armor --export-secret-keys "$key_id" | base64 | tr -d '\n')

  printf '%s' "$key_id" > "$secrets_dir/OPENDEPOT_PROVIDER_GPG_KEY_ID"
  printf '%s' "$ascii_armor" > "$secrets_dir/OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR"
  printf '%s' "$private_key" > "$secrets_dir/OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64"
  kubectl create secret generic opendepot-provider-gpg \
    --from-file=OPENDEPOT_PROVIDER_GPG_KEY_ID="$secrets_dir/OPENDEPOT_PROVIDER_GPG_KEY_ID" \
    --from-file=OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR="$secrets_dir/OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR" \
    --from-file=OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64="$secrets_dir/OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64" \
    --namespace "$namespace"
fi

jev_enabled=false
if [ -n "${OPENDEPOT_JEV_API_KEY:-}" ]; then
  printf '%s' "$OPENDEPOT_JEV_API_KEY" > "$secrets_dir/jevToken"
  kubectl create secret generic opendepot-jev \
    --from-file=jevToken="$secrets_dir/jevToken" \
    --namespace "$namespace" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null
  jev_enabled=true
fi

mkdir -p tilt/.generated
cat > tilt/.generated/values.yaml <<EOF
server:
  anonymousAuth: $anonymous_auth

scanning:
  jev:
    enabled: $jev_enabled

dex:
  config:
    staticPasswords:
      - email: dev@example.com
        hash: '$password_hash'
        username: devuser
        userID: local-test-user
        groups:
          - local-test-group
EOF