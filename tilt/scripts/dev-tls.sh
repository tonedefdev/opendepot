#!/bin/sh
set -eu

if ! command -v mkcert >/dev/null 2>&1; then
  echo "mkcert is required to generate the Tilt development certificate" >&2
  exit 1
fi

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required to create the Tilt development TLS Secret" >&2
  exit 1
fi

namespace=opendepot-system
certificate_dir=$(mktemp -d -t opendepot-dev-tls)
trap 'rm -rf "$certificate_dir"' EXIT INT TERM

certificate_path="$certificate_dir/tls.crt"
private_key_path="$certificate_dir/tls.key"
ca_path="$(mkcert -CAROOT)/rootCA.pem"

mkcert \
  -cert-file "$certificate_path" \
  -key-file "$private_key_path" \
  opendepot.localtest.me \
  localhost \
  127.0.0.1 \
  ::1 \
  server \
  server.opendepot-system \
  server.opendepot-system.svc \
  server.opendepot-system.svc.cluster.local

kubectl create namespace "$namespace" --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret generic opendepot-tls \
  --namespace "$namespace" \
  --type kubernetes.io/tls \
  --from-file=tls.crt="$certificate_path" \
  --from-file=tls.key="$private_key_path" \
  --from-file=ca.crt="$ca_path" \
  --dry-run=client -o yaml | kubectl apply -f -