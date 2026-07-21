REGISTRY ?= ghcr.io/tonedefdev/opendepot
PLATFORM ?= linux/arm64
KIND_CLUSTER ?= opendepot
TAG ?= dev

SERVICES := server depot-controller module-controller provider-controller version-controller ui

# Map service names to their build context directories
server_PATH := services/server
depot-controller_PATH := services/depot
module-controller_PATH := services/module
provider-controller_PATH := services/provider
version-controller_PATH := services/version
ui_PATH := services/ui

# Map service names to their Docker build context (repo root for services that use local packages)
server_CONTEXT := .
depot-controller_CONTEXT := .
module-controller_CONTEXT := .
provider-controller_CONTEXT := .
version-controller_CONTEXT := .
ui_CONTEXT := services/ui

.PHONY: build load deploy clean $(addprefix build-,$(SERVICES)) $(addprefix load-,$(SERVICES)) build-version-controller-scanning load-version-controller-scanning

## Build all images for the target platform
build: $(addprefix build-,$(SERVICES))

## Load all images into the kind cluster
load: $(addprefix load-,$(SERVICES))

## Build the version-controller image with Trivy bundled (required for scanning.enabled=true)
build-version-controller-scanning:
	docker build --platform $(PLATFORM) \
		--no-cache \
		-t $(REGISTRY)/version-controller:$(TAG)-scanning \
		--build-arg INCLUDE_TRIVY=true \
		-f services/version/Dockerfile \
		.

## Load the scanning variant of the version-controller into the kind cluster
load-version-controller-scanning:
	kind load docker-image $(REGISTRY)/version-controller:$(TAG)-scanning --name $(KIND_CLUSTER)

## Build and load all images into the kind cluster
deploy: build load

CHART_PATH ?= chart/opendepot
ISTIO_VERSION ?= 1.28.3

## Recreate kind cluster with Istio, TLS, gateway, Helm chart, and cloud-provider-kind
kind-restart:
	@echo "=== Deleting existing kind cluster ==="
	kind delete cluster --name $(KIND_CLUSTER)
	@echo "=== Creating kind cluster ==="
	kind create cluster --name $(KIND_CLUSTER)
	@echo "=== Loading images into kind ==="
	@$(MAKE) load
	@echo "=== Installing Istio ==="
	helm install istio-base istio/base -n istio-system --create-namespace --wait
	helm install istiod istio/istiod -n istio-system --wait
	@echo "=== Creating istio-ingress namespace ==="
	kubectl create namespace istio-ingress || true
	@echo "=== Installing Istio ingress gateway ==="
	helm install istio-ingress istio/gateway -n istio-ingress --wait || true
	@echo "=== Applying TLS secret ==="
	kubectl apply -f $(CHART_PATH)/tls-secret.yaml
	@echo "=== Applying Istio gateway ==="
	kubectl apply -f $(CHART_PATH)/gateway.yaml
	@echo "=== Deploying Helm chart ==="
	helm upgrade --install opendepot $(CHART_PATH) -n opendepot-system --create-namespace --wait --force-conflicts
	@echo "=== Starting cloud-provider-kind ==="
	@pkill -f cloud-provider-kind 2>/dev/null || true
	@sleep 1
	sudo cloud-provider-kind &>/tmp/cloud-provider-kind.log &
	@echo "Waiting for cloud-provider-kind to start..."
	@sleep 10
	@echo "=== Port mappings ==="
	@docker port $$(docker ps -q --filter name=kindccm) 2>/dev/null || echo "No proxy container found yet — check: docker ps --filter name=kindccm"

define SERVICE_RULES
.PHONY: build-$(1) load-$(1)

build-$(1):
	docker build --platform $(PLATFORM) \
		--no-cache \
		-t $(REGISTRY)/$(1):$(TAG) \
		-f $($(1)_PATH)/Dockerfile \
		$($(1)_CONTEXT)

load-$(1):
	kind load docker-image $(REGISTRY)/$(1):$(TAG) --name $(KIND_CLUSTER)
endef

$(foreach svc,$(SERVICES),$(eval $(call SERVICE_RULES,$(svc))))

## Build and load a single service: make service NAME=provider-controller
service:
	@$(MAKE) build-$(NAME) load-$(NAME)

## Restart deployments in opendepot-system after loading new images
restart:
	kubectl rollout restart deployment -n opendepot-system

## Build, load, and restart all
redeploy: deploy restart

clean:
	@for svc in $(SERVICES); do \
		docker rmi $(REGISTRY)/$$svc:$(TAG) 2>/dev/null || true; \
	done

## Tidy all workspace modules and sync go.work; run after adding any new go.mod
.PHONY: work-tidy
work-tidy:
	@go work edit -json | python3 -c \
	  "import sys,json,subprocess,os; \
	   [subprocess.run(['go','mod','tidy'],cwd=os.path.join('$(CURDIR)',d['DiskPath']),check=True) \
	   for d in json.load(sys.stdin)['Use']]"
	go work sync

E2E_SERVICES := depot module provider server version

## Run e2e tests for all services sequentially
.PHONY: test-e2e
test-e2e:
	@for svc in $(E2E_SERVICES); do \
	  echo "=== Running e2e tests for $$svc ==="; \
	  $(MAKE) -C services/$$svc test-e2e || exit 1; \
	done

## ─── OIDC Local Testing (Kind) ───────────────────────────────────────────────
# Configurable defaults — override any of these on the command line.
OIDC_EMAIL        ?= dev@example.com
OIDC_USER         ?= devuser
OIDC_SECRET       ?= local-test-secret
OIDC_CLIENT_ID    ?= opendepot
OIDC_SERVER_PORT  ?= 8080
OIDC_RELEASE_NAME    ?= opendepot
OIDC_NAMESPACE       ?= opendepot-system
# Group assigned to the test user in Dex and matched by the test GroupBinding expression.
OIDC_GROUP           ?= local-test-group
# Hostname used as the registry address in module sources. Must contain at least one dot
# (OpenTofu requirement). opendepot.localtest.me resolves to 127.0.0.1 via public DNS —
# no /etc/hosts editing required. Override to use a different local hostname.
OIDC_REGISTRY_HOST   ?= opendepot.localtest.me
# In-cluster Dex URL — used by the UI backend (ui.oidc.issuerUrl) for its own
# server-to-Dex OIDC discovery and token exchange, which happens entirely
# in-cluster and never touches the browser.
OIDC_DEX_INCLUSTER_URL = http://$(OIDC_RELEASE_NAME)-dex.$(OIDC_NAMESPACE).svc.cluster.local:5556/dex
# External, browser-reachable Dex URL for the server (CLI/tofu login) path. Dex
# is reverse-proxied through the server itself (server.oidc.dexProxy.enabled),
# so this is reachable via the single server port-forward — no separate Dex
# port-forward needed. Kind has no ingress controller, so this stands in for a
# real ingress hostname (see docs/configuration/oidc.md#recommended-proxy-dex-through-the-server).
OIDC_DEX_EXTERNAL_URL = http://$(OIDC_REGISTRY_HOST):$(OIDC_SERVER_PORT)/dex
# External, browser-reachable Dex URL for the UI's own login flow. Reverse-proxied
# through the UI's nginx -> server -> Dex, reachable via the single UI
# port-forward. Only overrides ui.oidc.authzUrl for the browser redirect; the UI
# backend itself still talks to Dex in-cluster via OIDC_DEX_INCLUSTER_URL above.
UI_DEX_EXTERNAL_URL = http://$(OIDC_REGISTRY_HOST):$(UI_PORT)/dex

.PHONY: oidc-hash oidc-hosts oidc-login oidc-tls oidc-deploy oidc-forward oidc-stop oidc-verify oidc-setup oidc-test-resources oidc-test-clean

## Add $(OIDC_REGISTRY_HOST) → 127.0.0.1 to /etc/hosts (requires sudo).
## Only needed when OIDC_REGISTRY_HOST is overridden to a custom hostname that
## does not resolve via public DNS. Not required for the default opendepot.localtest.me.
oidc-hosts:
	@if grep -q "$(OIDC_REGISTRY_HOST)" /etc/hosts; then \
	  echo "$(OIDC_REGISTRY_HOST) already in /etc/hosts — skipping"; \
	else \
	  echo "Adding 127.0.0.1 $(OIDC_REGISTRY_HOST) to /etc/hosts (sudo required)..."; \
	  sudo sh -c 'echo "127.0.0.1 $(OIDC_REGISTRY_HOST)" >> /etc/hosts'; \
	  echo "Done."; \
	fi

oidc-tls:
	@echo "Installing mkcert CA into system trust store..."
	mkcert -install
	@echo "Generating certificate for $(OIDC_REGISTRY_HOST) and localhost..."
	mkcert -cert-file /tmp/opendepot-tls.crt -key-file /tmp/opendepot-tls.key $(OIDC_REGISTRY_HOST) localhost 127.0.0.1 ::1
	@echo "Creating opendepot-tls secret..."
	kubectl create namespace $(OIDC_NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	kubectl create secret tls opendepot-tls \
	  --cert=/tmp/opendepot-tls.crt \
	  --key=/tmp/opendepot-tls.key \
	  -n $(OIDC_NAMESPACE) \
	  --dry-run=client -o yaml | kubectl apply -f -
	@rm -f /tmp/opendepot-tls.crt /tmp/opendepot-tls.key
	@echo "TLS secret ready. Run: make oidc-deploy PASS=yourpassword"

## Generate a bcrypt password hash for Dex staticPasswords.
## Usage: make oidc-hash PASS=yourpassword
oidc-hash:
ifndef PASS
	$(error PASS is required. Usage: make oidc-hash PASS=yourpassword)
endif
	@if command -v htpasswd >/dev/null 2>&1; then \
	  htpasswd -bnBC 10 "" "$(PASS)" | tr -d ':\n' && echo; \
	else \
	  python3 -c "import bcrypt; print(bcrypt.hashpw(b'$(PASS)', bcrypt.gensalt(10)).decode())" \
	    || (echo "ERROR: neither htpasswd nor the python3 bcrypt package is available." \
	        "Install Apache tools (brew install httpd) or run: pip3 install bcrypt" >&2 && exit 1); \
	fi

## Deploy OpenDepot with Dex and OIDC enabled for local Kind testing.
## The bcrypt hash is generated from PASS entirely within the shell recipe so that
## the '$' characters in the hash are never subject to Make variable expansion.
## Usage: make oidc-deploy PASS=yourpassword
oidc-deploy:
ifndef PASS
	$(error PASS is required. Usage: make oidc-deploy PASS=yourpassword)
endif
	@if command -v htpasswd >/dev/null 2>&1; then \
	  _hash=$$(htpasswd -bnBC 10 "" "$(PASS)" | tr -d ':\n'); \
	else \
	  _hash=$$(python3 -c "import bcrypt; print(bcrypt.hashpw(b'$(PASS)', bcrypt.gensalt(10)).decode())") \
	    || { echo "ERROR: neither htpasswd nor the python3 bcrypt package is available." \
	           "Install Apache tools (brew install httpd) or run: pip3 install bcrypt" >&2; exit 1; }; \
	fi; \
	rm -f /tmp/opendepot-oidc-XXXXXX.yaml; \
	tmpfile=$$(mktemp -t opendepot-oidc) || { echo "ERROR: mktemp failed" >&2; exit 1; }; \
	printf '%s\n' \
	  'dex:' \
	  '  enabled: true' \
	  '  image:' \
	  '    tag: v2.45.0' \
	  '  config:' \
	  "    issuer: \"$(OIDC_DEX_EXTERNAL_URL)\"" \
	  '    storage:' \
	  '      type: memory' \
	  '    enablePasswordDB: true' \
	  '    oauth2:' \
	  '      responseTypes:' \
	  '        - code' \
	  '      grantTypes:' \
	  '        - authorization_code' \
	  '        - "urn:ietf:params:oauth:grant-type:device_code"' \
	  '      skipApprovalScreen: true' \
	  '    staticPasswords:' \
	  '      - email: "$(OIDC_EMAIL)"' \
	  "        hash: \"$$_hash\"" \
	  '        username: "$(OIDC_USER)"' \
	  '        userID: "local-test-user"' \
	  '        groups:' \
	  '          - "$(OIDC_GROUP)"' \
	  '    connectors: []' \
	  '    staticClients:' \
	  '      - id: "$(OIDC_CLIENT_ID)"' \
	  '        name: OpenDepot' \
	  '        public: true' \
	  '        redirectURIs:' \
	  '          - http://localhost:10000/login' \
	  '          - http://localhost:10001/login' \
	  '          - http://localhost:10002/login' \
	  '          - http://localhost:10003/login' \
	  '          - http://localhost:10004/login' \
	  '          - http://localhost:10005/login' \
	  '          - http://localhost:10006/login' \
	  '          - http://localhost:10007/login' \
	  '          - http://localhost:10008/login' \
	  '          - http://localhost:10009/login' \
	  '          - http://localhost:10010/login' \
	  'server:' \
	  '  oidc:' \
	  '    enabled: true' \
	  "    issuerUrl: \"$(OIDC_DEX_EXTERNAL_URL)\"" \
	  '    dexProxy:' \
	  '      enabled: true' \
	  '    clientSecret: "$(OIDC_SECRET)"' \
	  'storage:' \
	  '  filesystem:' \
	  '    enabled: true' \
	  '    hostPath: /tmp/opendepot-modules' \
	  > "$$tmpfile"; \
	echo "=== Deploying with OIDC values (dex issuer: $(OIDC_DEX_EXTERNAL_URL)) ==="; \
	helm upgrade --install opendepot $(CHART_PATH) \
	  -n opendepot-system --create-namespace \
	  --set global.image.tag=$(TAG) \
	  -f "$$tmpfile" --wait --force-conflicts; \
	rm -f "$$tmpfile"

## Start the kubectl port-forward for local OIDC testing. Dex is reverse-proxied
## through the server (server.oidc.dexProxy.enabled), so a single port-forward
## covers both the registry API and Dex login/token exchange — server → localhost:$(OIDC_SERVER_PORT).
## When TLS is enabled (oidc-tls was run) the server listens on :8443 inside the
## container; we port-forward directly to the pod to reach that port.
oidc-forward:
	@echo "Starting port-forward: server → localhost:$(OIDC_SERVER_PORT)"
	@kubectl port-forward -n opendepot-system svc/server $(OIDC_SERVER_PORT):80 &
	@echo "Port-forward running. Stop with: make oidc-stop"

## Stop the OIDC test port-forward
oidc-stop:
	@pkill -f "kubectl port-forward.*svc/server.*:80" 2>/dev/null \
	  && echo "Stopped server port-forward" || echo "Server port-forward was not running"

## Authenticate tofu with the local registry. Run after oidc-forward.
oidc-login:
	tofu login $(OIDC_REGISTRY_HOST):$(OIDC_SERVER_PORT)

## Verify the OIDC service discovery response (requires port-forwards to be running)
oidc-verify:
	@curl -sf --cacert "$$(mkcert -CAROOT)/rootCA.pem" \
	  https://$(OIDC_REGISTRY_HOST):$(OIDC_SERVER_PORT)/.well-known/terraform.json | python3 -m json.tool

## Deploy and start port-forwards in one step (builds and loads images first).
## Usage: make oidc-setup PASS=yourpassword
oidc-setup: deploy oidc-tls oidc-deploy oidc-forward

## Apply a sample Module (terraform-aws-key-pair) and a GroupBinding that grants
## access to OIDC users in $(OIDC_GROUP). Run after oidc-deploy to set up e2e test resources.
## The module controller will sync from GitHub (public, no auth required).
oidc-test-resources:
	@echo "=== Creating test Module and GroupBinding ==="
	@tmpfile=$$(mktemp -t opendepot-test) || { echo "ERROR: mktemp failed" >&2; exit 1; }; \
	printf '%s\n' \
	  'apiVersion: opendepot.defdev.io/v1alpha1' \
	  'kind: Module' \
	  'metadata:' \
	  '  name: terraform-aws-key-pair' \
	  '  namespace: $(OIDC_NAMESPACE)' \
	  'spec:' \
	  '  moduleConfig:' \
	  '    fileFormat: zip' \
	  '    githubClientConfig:' \
	  '      useAuthenticatedClient: false' \
	  '    provider: aws' \
	  '    repoOwner: terraform-aws-modules' \
	  '    repoUrl: https://github.com/terraform-aws-modules/terraform-aws-key-pair' \
	  '    storageConfig:' \
	  '      fileSystem:' \
	  '        directoryPath: /data/modules' \
	  '  versions:' \
	  '  - version: v2.0.3' \
	  '---' \
	  'apiVersion: opendepot.defdev.io/v1alpha1' \
	  'kind: GroupBinding' \
	  'metadata:' \
	  '  name: local-test-access' \
	  '  namespace: $(OIDC_NAMESPACE)' \
	  'spec:' \
	  '  expression: "\"$(OIDC_GROUP)\" in groups"' \
	  '  moduleResources:' \
	  '  - terraform-aws-key-pair' \
	  > "$$tmpfile"; \
	kubectl apply -f "$$tmpfile"; \
	rm -f "$$tmpfile"; \
	echo "=== Done. Monitor sync: kubectl get module terraform-aws-key-pair -n $(OIDC_NAMESPACE) -w ==="

## Remove the test Module and GroupBinding created by oidc-test-resources.
oidc-test-clean:
	kubectl delete module terraform-aws-key-pair -n $(OIDC_NAMESPACE) --ignore-not-found
	kubectl delete groupbinding local-test-access -n $(OIDC_NAMESPACE) --ignore-not-found

## ─── UI Local Testing (Kind) ─────────────────────────────────────────────────
# Port that the UI NGINX container is port-forwarded to on the host.
UI_PORT           ?= 8080
# Port used for local server API port-forward consumed by Next.js dev server.
UI_API_PORT       ?= 18080
# Port used by the local Next.js dev server.
UI_DEV_PORT       ?= 3000
# OIDC client ID registered in Dex for the UI (separate from the tofu login client).
UI_OIDC_CLIENT_ID ?= opendepot-ui
# Static client secret used for the UI Dex client in local Kind testing only.
UI_OIDC_SECRET    ?= ui-local-test-secret

.PHONY: ui-session-secret ui-gpg-secret ui-deploy-anon ui-deploy ui-forward ui-stop ui-tofurc ui-setup ui-setup-oidc ui-dev ui-dev-stop chart-deps

## Download and cache Helm chart dependencies (Dex, Valkey). Run once after cloning.
## make ui-setup and make ui-setup-oidc call this automatically.
chart-deps:
	helm dependency update $(CHART_PATH)

## Generate a throwaway GPG keypair and create the provider signing secret for local testing.
## Idempotent — skips if the secret already exists.
ui-gpg-secret:
	@kubectl create namespace $(OIDC_NAMESPACE) --dry-run=client -o yaml | kubectl apply -f - >/dev/null; \
	if kubectl get secret opendepot-provider-gpg -n $(OIDC_NAMESPACE) >/dev/null 2>&1; then \
	  echo "opendepot-provider-gpg already exists — skipping"; \
	else \
	  tmpdir=$$(mktemp -d -t opendepot-gpg); \
	  printf '%%no-protection\nKey-Type: RSA\nKey-Length: 2048\nName-Real: OpenDepot Local\nName-Email: opendepot@local.test\nExpire-Date: 0\n%%commit\n' \
	    > "$$tmpdir/keygen.conf"; \
	  GNUPGHOME="$$tmpdir" gpg --batch --gen-key "$$tmpdir/keygen.conf" 2>/dev/null; \
	  KEY_ID=$$(GNUPGHOME="$$tmpdir" gpg --list-keys --with-colons 2>/dev/null | awk -F: '/^fpr/{print $$10; exit}'); \
	  ASCII_ARMOR=$$(GNUPGHOME="$$tmpdir" gpg --armor --export "$$KEY_ID" 2>/dev/null); \
	  PRIV_B64=$$(GNUPGHOME="$$tmpdir" gpg --armor --export-secret-keys "$$KEY_ID" 2>/dev/null | base64 | tr -d '\n'); \
	  kubectl create secret generic opendepot-provider-gpg \
	    --from-literal=OPENDEPOT_PROVIDER_GPG_KEY_ID="$$KEY_ID" \
	    --from-literal=OPENDEPOT_PROVIDER_GPG_ASCII_ARMOR="$$ASCII_ARMOR" \
	    --from-literal=OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64="$$PRIV_B64" \
	    -n $(OIDC_NAMESPACE); \
	  rm -rf "$$tmpdir"; \
	  echo "Created opendepot-provider-gpg (key: $$KEY_ID)"; \
	fi

## Create the session-cookie encryption secret for the UI. Idempotent — skips if it already exists.
ui-session-secret:
	@kubectl create namespace $(OIDC_NAMESPACE) --dry-run=client -o yaml | kubectl apply -f - >/dev/null
	@if kubectl get secret ui-session-secret -n $(OIDC_NAMESPACE) >/dev/null 2>&1; then \
	  echo "ui-session-secret already exists — skipping"; \
	else \
	  _pass=$$(openssl rand -base64 32); \
	  kubectl create secret generic ui-session-secret \
	    --from-literal=sessionPassword="$$_pass" \
	    -n $(OIDC_NAMESPACE); \
	  echo "Created ui-session-secret in $(OIDC_NAMESPACE)"; \
	fi

## Deploy the UI in anonymous-auth mode. No OIDC required — all resources are visible to everyone.
## Usage: make ui-deploy-anon
ui-deploy-anon: ui-session-secret
	@helm upgrade --install $(OIDC_RELEASE_NAME) $(CHART_PATH) \
	  -n $(OIDC_NAMESPACE) --create-namespace \
	  --set global.image.tag=$(TAG) \
	  --set server.enabled=true \
	  --set server.image.repository=$(REGISTRY)/server \
	  --set server.image.tag=$(TAG) \
	  --set server.anonymousAuth=true \
	  --set server.useBearerToken=false \
	  --set ui.enabled=true \
	  --set ui.image.repository=$(REGISTRY)/ui \
	  --set ui.image.tag=$(TAG) \
	  --set ui.sessionPasswordSecretName=ui-session-secret \
	  --set ui.nginx.preserveHostPort=true \
	  --set storage.filesystem.enabled=true \
	  --set storage.filesystem.hostPath=/tmp/opendepot-modules \
	  --set provider.enabled=true \
	  --set scanning.enabled=true \
	  --set scanning.providerScanning=true \
	  --set scanning.cache.storageClassName="standard" \
	  --set scanning.cache.accessMode=ReadWriteOnce \
	  --set version.zapLogLevel=5 \
	  --wait --force-conflicts \
	kubectl create job trivy-cache-db from=cronjob/trivy-db-updater -n $(OIDC_NAMESPACE) -w

## Full e2e deployment: UI + OIDC login + module/provider scanning + tofu login support.
## This is the single target to validate the entire system end-to-end.
## Deploys: server (OIDC), module, version, provider, depot, scanning (w/ provider scanning),
## UI (OIDC), and Dex. Configures a test user ($(OIDC_EMAIL)) who can log in via the UI
## and use `tofu login` through the UI proxy at http://opendepot.localtest.me:$(UI_PORT).
## Usage: make ui-deploy PASS=yourpassword
ui-deploy: ui-session-secret ui-gpg-secret
ifndef PASS
	$(error PASS is required. Usage: make ui-deploy PASS=yourpassword)
endif
	@if command -v htpasswd >/dev/null 2>&1; then \
	  _hash=$$(htpasswd -bnBC 10 "" "$(PASS)" | tr -d ':\n'); \
	else \
	  _hash=$$(python3 -c "import bcrypt; print(bcrypt.hashpw(b'$(PASS)', bcrypt.gensalt(10)).decode())") \
	    || { echo "ERROR: neither htpasswd nor the python3 bcrypt package is available." \
	           "Install Apache tools (brew install httpd) or run: pip3 install bcrypt" >&2; exit 1; }; \
	fi; \
	kubectl create secret generic ui-oidc-secret \
	  --from-literal=clientSecret="$(UI_OIDC_SECRET)" \
	  -n $(OIDC_NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -; \
	rm -f /tmp/opendepot-ui-XXXXXX.yaml; \
	tmpfile=$$(mktemp -t opendepot-ui) || { echo "ERROR: mktemp failed" >&2; exit 1; }; \
	printf '%s\n' \
	  'dex:' \
	  '  enabled: true' \
	  '  image:' \
	  '    tag: v2.45.0' \
	  '  config:' \
	  "    issuer: \"$(UI_DEX_EXTERNAL_URL)\"" \
	  '    storage:' \
	  '      type: memory' \
	  '    enablePasswordDB: true' \
	  '    oauth2:' \
	  '      responseTypes:' \
	  '        - code' \
	  '      grantTypes:' \
	  '        - authorization_code' \
	  '        - "urn:ietf:params:oauth:grant-type:device_code"' \
	  '      skipApprovalScreen: true' \
	  '    staticPasswords:' \
	  '      - email: "$(OIDC_EMAIL)"' \
	  "        hash: \"$$_hash\"" \
	  '        username: "$(OIDC_USER)"' \
	  '        userID: "local-test-user"' \
	  '        groups:' \
	  '          - "$(OIDC_GROUP)"' \
	  '    connectors: []' \
	  '    staticClients:' \
	  '      - id: "$(UI_OIDC_CLIENT_ID)"' \
	  '        name: OpenDepot UI' \
	  '        secret: "$(UI_OIDC_SECRET)"' \
	  '        trustedPeers:' \
	  '          - "$(OIDC_CLIENT_ID)"' \
	  '        redirectURIs:' \
	  "          - \"http://opendepot.localtest.me:$(UI_PORT)/auth/callback\"" \
	  '      - id: "$(OIDC_CLIENT_ID)"' \
	  '        name: OpenDepot' \
	  '        public: true' \
	  '        redirectURIs:' \
	  '          - http://localhost:10000/login' \
	  '          - http://localhost:10001/login' \
	  '          - http://localhost:10002/login' \
	  '          - http://localhost:10003/login' \
	  '          - http://localhost:10004/login' \
	  '          - http://localhost:10005/login' \
	  '          - http://localhost:10006/login' \
	  '          - http://localhost:10007/login' \
	  '          - http://localhost:10008/login' \
	  '          - http://localhost:10009/login' \
	  '          - http://localhost:10010/login' \
	  'server:' \
	  '  image:' \
	  '    repository: $(REGISTRY)/server' \
	  "    tag: \"$(TAG)\"" \
	  '  gpg:' \
	  '    secretName: opendepot-provider-gpg' \
	  '  oidc:' \
	  '    enabled: true' \
	  "    issuerUrl: \"$(UI_DEX_EXTERNAL_URL)\"" \
	  '    dexProxy:' \
	  '      enabled: true' \
	  '    clientId: "$(OIDC_CLIENT_ID)"' \
	  '    clientSecret: "$(OIDC_SECRET)"' \
	  '    groupsClaim: "groups"' \
	  'provider:' \
	  '  enabled: true' \
	  '  image:' \
	  '    repository: $(REGISTRY)/provider-controller' \
	  "    tag: \"$(TAG)\"" \
	  'scanning:' \
	  '  enabled: true' \
	  '  providerScanning: true' \
	  '  cache:' \
	  '    storageClassName: standard' \
	  '    accessMode: ReadWriteOnce' \
	  'version:' \
	  '  zapLogLevel: 5' \
	  '  image:' \
	  '    repository: $(REGISTRY)/version-controller' \
	  'storage:' \
	  '  filesystem:' \
	  '    enabled: true' \
	  '    hostPath: /tmp/opendepot-modules' \
	  'ui:' \
	  '  enabled: true' \
	  '  image:' \
	  '    repository: $(REGISTRY)/ui' \
	  "    tag: \"$(TAG)\"" \
	  "  baseUrl: \"http://opendepot.localtest.me:$(UI_PORT)\"" \
	  '  sessionPasswordSecretName: ui-session-secret' \
	  '  oidc:' \
	  '    enabled: true' \
	  "    issuerUrl: \"$(OIDC_DEX_INCLUSTER_URL)\"" \
	  "    authzUrl: \"$(UI_DEX_EXTERNAL_URL)/auth\"" \
	  '    clientId: "$(UI_OIDC_CLIENT_ID)"' \
	  '    clientSecretName: ui-oidc-secret' \
	  '  nginx:' \
	  '    preserveHostPort: true' \
	  > "$$tmpfile"; \
	echo "=== Deploying full e2e stack: UI + OIDC + scanning (dex issuer: $(UI_DEX_EXTERNAL_URL)) ==="; \
	helm upgrade --install $(OIDC_RELEASE_NAME) $(CHART_PATH) \
	  -n $(OIDC_NAMESPACE) --create-namespace \
	  --set global.image.tag=$(TAG) \
	  -f "$$tmpfile" --wait --force-conflicts; \
	rm -f "$$tmpfile"; \
	echo "=== Seeding Trivy vulnerability DB cache ==="; \
	kubectl create job trivy-cache-db --from=cronjob/trivy-db-updater -n $(OIDC_NAMESPACE) --dry-run=client -o yaml \
	  | kubectl apply -f -; \
	kubectl wait --for=condition=complete job/trivy-cache-db -n $(OIDC_NAMESPACE) --timeout=5m \
	  || echo "WARNING: Trivy cache seed job did not complete in 5m — scans will run online"; \
	echo "=== Deployment complete ==="; \
	echo "  UI:        http://opendepot.localtest.me:$(UI_PORT)  (run: make ui-forward)"; \
	echo "  tofu login: tofu login opendepot.localtest.me:$(UI_PORT)"; \
	echo "  Login:     $(OIDC_EMAIL) / <your PASS>"

## Start the port-forward for the UI. Dex is reverse-proxied through the UI's
## nginx -> server -> Dex (server.oidc.dexProxy.enabled), so a single port-forward
## covers the UI, the registry API, and Dex login/token exchange.
## After running, open http://opendepot.localtest.me:$(UI_PORT) in your browser.
## Usage: make ui-forward
ui-forward:
	@echo "Starting port-forward: ui → localhost:$(UI_PORT)"
	@kubectl port-forward -n $(OIDC_NAMESPACE) svc/ui $(UI_PORT):80 &
	@echo "UI available at: http://opendepot.localtest.me:$(UI_PORT)"

## Stop the UI port-forward started by ui-forward.
ui-stop:
	@pkill -f "kubectl port-forward.*svc/ui.*:80" 2>/dev/null \
	  && echo "Stopped UI port-forward" || echo "UI port-forward was not running"

## Write ~/.tofurc so that `tofu login opendepot.localtest.me:$(UI_PORT)` works
## over plain HTTP. This is still required even with the single-port-forward
## dexProxy setup: OpenTofu's service discovery only works over HTTPS, and the
## local registry here is plain HTTP, so a CLI config host block is needed to
## define modules.v1/providers.v1 explicitly. A host block replaces discovery
## entirely for every service under that host, so login.v1 must be defined
## manually too — pointed at Dex through the single UI port-forward
## ($(UI_DEX_EXTERNAL_URL)), no separate Dex port-forward needed.
## Usage: make ui-tofurc
ui-tofurc:
	@printf '%s\n' \
	  'host "opendepot.localtest.me:$(UI_PORT)" {' \
	  '  services = {' \
	  '    "modules.v1"   = "http://opendepot.localtest.me:$(UI_PORT)/opendepot/modules/v1/"' \
	  '    "providers.v1" = "http://opendepot.localtest.me:$(UI_PORT)/opendepot/providers/v1/"' \
	  '    "login.v1" = {' \
	  '      client      = "$(OIDC_CLIENT_ID)"' \
	  '      grant_types = ["authz_code"]' \
	  "      authz       = \"$(UI_DEX_EXTERNAL_URL)/auth\"" \
	  "      token       = \"$(UI_DEX_EXTERNAL_URL)/token\"" \
	  '      scopes      = ["openid", "email", "profile", "groups", "offline_access"]' \
	  '      ports       = [10000, 10010]' \
	  '    }' \
	  '  }' \
	  '}' \
	  > ~/.tofurc; \
	echo "Wrote ~/.tofurc for opendepot.localtest.me:$(UI_PORT) (Dex via the single UI port-forward at $(UI_DEX_EXTERNAL_URL))"

## Build all images, deploy the UI in anonymous-auth mode, and start the port-forward.
## Usage: make ui-setup
ui-setup: chart-deps deploy build-version-controller-scanning load-version-controller-scanning ui-deploy-anon restart ui-forward

## Build all images, deploy the full e2e stack (UI + OIDC + scanning), start
## the port-forward, and write ~/.tofurc so `tofu login opendepot.localtest.me:$(UI_PORT)`
## works immediately. ~/.tofurc is still required because this local registry is
## plain HTTP (no mkcert TLS) — OpenTofu's service discovery only works over
## HTTPS. Dex itself needs no separate port-forward: server.oidc.dexProxy.enabled
## reverse-proxies it through the same single UI port-forward.
## Usage: make ui-setup-oidc PASS=yourpassword
ui-setup-oidc: chart-deps deploy build-version-controller-scanning load-version-controller-scanning ui-deploy restart ui-forward ui-tofurc

## One-shot local UI development against a running kind cluster server.
## - Starts server API port-forward: localhost:$(UI_API_PORT) -> svc/server:80
## - Runs Next.js dev server on localhost:$(UI_DEV_PORT)
## Usage: make ui-dev
ui-dev:
	@pkill -f "kubectl port-forward.*svc/server.*$(UI_API_PORT):80" 2>/dev/null || true
	@pkill -f "next dev --port $(UI_DEV_PORT)" 2>/dev/null || true
	@rm -rf services/ui/.next
	@echo "Starting port-forward: server -> localhost:$(UI_API_PORT)"
	@kubectl port-forward -n $(OIDC_NAMESPACE) svc/server $(UI_API_PORT):80 >/tmp/opendepot-ui-api-forward.log 2>&1 &
	@echo "UI API forward log: /tmp/opendepot-ui-api-forward.log"
	@echo "Starting Next.js dev server on localhost:$(UI_DEV_PORT)"
	@cd services/ui && NEXT_DISABLE_DEVTOOLS=1 OPENDEPOT_SERVER_URL=http://127.0.0.1:$(UI_API_PORT) yarn dev --port $(UI_DEV_PORT)

## Stop local UI development background port-forwards started by ui-dev.
## Usage: make ui-dev-stop
ui-dev-stop:
	@pkill -f "kubectl port-forward.*svc/server.*$(UI_API_PORT):80" 2>/dev/null \
	  && echo "Stopped server API port-forward" || echo "Server API port-forward was not running"


## Only importable packages are tagged (api/v1alpha1, pkg/*) — services are excluded.
## Steps: create tags → push tags → update go.mod files → work-tidy → commit.
MODULE_VERSION ?= $(error MODULE_VERSION is required. Usage: make tag-modules MODULE_VERSION=vX.Y.Z)
MODULE_PACKAGES := api/v1alpha1 pkg/github pkg/storage pkg/testutils pkg/utils
.PHONY: tag-modules
tag-modules:
	@if ! git diff --cached --quiet || ! git diff --quiet; then \
	  echo "ERROR: uncommitted changes detected. Commit or stash before tagging." && exit 1; \
	fi
	@echo "=== Creating tags ==="
	@for pkg in $(MODULE_PACKAGES); do \
	  if git tag -l "$${pkg}/$(MODULE_VERSION)" | grep -q .; then \
	    echo "  tag $${pkg}/$(MODULE_VERSION) already exists, skipping"; \
	  else \
	    git tag "$${pkg}/$(MODULE_VERSION)" && echo "  tagged $${pkg}/$(MODULE_VERSION)"; \
	  fi; \
	done
	@echo "=== Pushing tags ==="
	@git push origin $(foreach pkg,$(MODULE_PACKAGES),$(pkg)/$(MODULE_VERSION))
	@echo "=== Updating all go.mod files to $(MODULE_VERSION) ==="
	@find . -name go.mod -not -path "*/vendor/*" | while read gomod; do \
	  for pkg in $(MODULE_PACKAGES); do \
	    sed -i '' "s|github.com/tonedefdev/opendepot/$${pkg} v[^[:space:]]*|github.com/tonedefdev/opendepot/$${pkg} $(MODULE_VERSION)|g" "$$gomod"; \
	  done; \
	done
	@echo "=== Running work-tidy ==="
	@$(MAKE) --no-print-directory work-tidy
	@echo "=== Committing go.mod and go.sum changes ==="
	@git add -u
	@git commit -m "chore: bump internal module dependencies to $(MODULE_VERSION)"
	@git push origin HEAD
	@echo "=== Done: all packages tagged at $(MODULE_VERSION) ==="
