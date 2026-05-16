REGISTRY ?= ghcr.io/tonedefdev/opendepot
PLATFORM ?= linux/arm64
KIND_CLUSTER ?= kind
TAG ?= dev

SERVICES := server depot-controller module-controller version-controller

# Map service names to their build context directories
server_PATH := services/server
depot-controller_PATH := services/depot
module-controller_PATH := services/module
version-controller_PATH := services/version

# Map service names to their Docker build context (repo root for services that use local packages)
server_CONTEXT := .
depot-controller_CONTEXT := .
module-controller_CONTEXT := .
version-controller_CONTEXT := .

.PHONY: build load deploy clean $(addprefix build-,$(SERVICES)) $(addprefix load-,$(SERVICES))

## Build all images for the target platform
build: $(addprefix build-,$(SERVICES))

## Load all images into the kind cluster
load: $(addprefix load-,$(SERVICES))

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
	helm upgrade --install opendepot $(CHART_PATH) -n opendepot-system --create-namespace --wait
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
		-t $(REGISTRY)/$(1):$(TAG) \
		-f $($(1)_PATH)/Dockerfile \
		$($(1)_CONTEXT)

load-$(1):
	kind load docker-image $(REGISTRY)/$(1):$(TAG) --name $(KIND_CLUSTER)
endef

$(foreach svc,$(SERVICES),$(eval $(call SERVICE_RULES,$(svc))))

## Build and load a single service: make service NAME=server
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
OIDC_DEX_PORT     ?= 5556
OIDC_RELEASE_NAME    ?= opendepot
OIDC_NAMESPACE       ?= opendepot-system
# Group assigned to the test user in Dex and matched by the test GroupBinding expression.
OIDC_GROUP           ?= local-test-group
# Hostname used as the registry address in module sources. Must contain at least one dot
# (OpenTofu requirement). Resolves to 127.0.0.1 via /etc/hosts — see oidc-hosts target.
OIDC_REGISTRY_HOST   ?= registry.local
# In-cluster Dex URL — used by the server pod for OIDC discovery and JWKS.
# Must match dex.config.issuer so that go-oidc accepts the discovery document.
# This is the correct value for local Kind testing where there is no ingress.
# In production with bundled Dex, set dex.config.issuer (and server.oidc.issuerUrl)
# to a real hostname fronted by an ingress, e.g. https://auth.company.com/dex.
OIDC_DEX_INCLUSTER_URL = http://$(OIDC_RELEASE_NAME)-dex.$(OIDC_NAMESPACE).svc.cluster.local:5556/dex
# Localhost URLs advertised to clients (tofu login / browser) via login.v1.
# These override the authz/token URLs from the Dex discovery document so that
# the browser is directed to the port-forwarded address rather than the
# unreachable in-cluster hostname. Not needed when using a real ingress.
OIDC_DEX_AUTHZ_URL = http://localhost:$(OIDC_DEX_PORT)/dex/auth
OIDC_DEX_TOKEN_URL = http://localhost:$(OIDC_DEX_PORT)/dex/token

.PHONY: oidc-hash oidc-hosts oidc-login oidc-tls oidc-deploy oidc-forward oidc-stop oidc-verify oidc-setup oidc-test-resources oidc-test-clean oidc-verify-module

## Install a locally-trusted TLS cert for localhost into the Kind cluster.
## Creates the opendepot-tls Kubernetes Secret used by the server when TLS is enabled.
## Requires mkcert (brew install mkcert). Run once per cluster before oidc-deploy.
## Add $(OIDC_REGISTRY_HOST) → 127.0.0.1 to /etc/hosts (one-time, requires sudo).
## This lets OpenTofu resolve a dotted hostname to the local port-forward.
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
	tmpfile=$$(mktemp /tmp/opendepot-oidc-XXXXXX.yaml); \
	printf '%s\n' \
	  'dex:' \
	  '  enabled: true' \
	  '  image:' \
	  '    tag: v2.45.0' \
	  '  config:' \
	  "    issuer: \"$(OIDC_DEX_INCLUSTER_URL)\"" \
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
	  "    authzUrl: \"$(OIDC_DEX_AUTHZ_URL)\"" \
	  "    tokenUrl: \"$(OIDC_DEX_TOKEN_URL)\"" \
	  '    clientSecret: "$(OIDC_SECRET)"' \
	  '  tls:' \
	  '    enabled: true' \
	  'storage:' \
	  '  filesystem:' \
	  '    enabled: true' \
	  '    hostPath: /tmp/opendepot-modules' \
	  > "$$tmpfile"; \
	echo "=== Deploying with OIDC values (dex issuer: $(OIDC_DEX_INCLUSTER_URL)) ==="; \
	helm upgrade --install opendepot $(CHART_PATH) \
	  -n opendepot-system --create-namespace \
	  --set global.image.tag=$(TAG) \
	  -f "$$tmpfile" --wait; \
	rm -f "$$tmpfile"

## Start kubectl port-forwards for local OIDC testing.
## server → localhost:$(OIDC_SERVER_PORT)  |  Dex → localhost:$(OIDC_DEX_PORT)
## When TLS is enabled (oidc-tls was run) the server listens on :8443 inside the
## container; we port-forward directly to the pod to reach that port.
oidc-forward:
	@echo "Starting port-forward: server → localhost:$(OIDC_SERVER_PORT)"
	@kubectl port-forward -n opendepot-system svc/server $(OIDC_SERVER_PORT):80 &
	@echo "Starting port-forward: dex → localhost:$(OIDC_DEX_PORT)"
	@kubectl port-forward -n opendepot-system svc/opendepot-dex $(OIDC_DEX_PORT):5556 &
	@echo "Port-forwards running. Stop with: make oidc-stop"

## Stop OIDC test port-forwards
oidc-stop:
	@pkill -f "kubectl port-forward.*svc/server.*:80" 2>/dev/null \
	  && echo "Stopped server port-forward" || echo "Server port-forward was not running"
	@pkill -f "kubectl port-forward.*svc/opendepot-dex" 2>/dev/null \
	  && echo "Stopped Dex port-forward" || echo "Dex port-forward was not running"

## Authenticate tofu with the local registry. Run after oidc-forward.
oidc-login:
	tofu login $(OIDC_REGISTRY_HOST):$(OIDC_SERVER_PORT)

## Verify the OIDC service discovery response (requires port-forwards to be running)
oidc-verify:
	@curl -sf --cacert "$$(mkcert -CAROOT)/rootCA.pem" \
	  https://$(OIDC_REGISTRY_HOST):$(OIDC_SERVER_PORT)/.well-known/terraform.json | python3 -m json.tool

## Deploy and start port-forwards in one step (builds and loads images first).
## Usage: make oidc-setup PASS=yourpassword
oidc-setup: deploy oidc-hosts oidc-tls oidc-deploy oidc-forward

## Apply a sample Module (terraform-aws-key-pair) and a GroupBinding that grants
## access to OIDC users in $(OIDC_GROUP). Run after oidc-deploy to set up e2e test resources.
## The module controller will sync from GitHub (public, no auth required).
## Use oidc-verify-module to confirm the auth flow after running tofu login.
oidc-test-resources:
	@echo "=== Creating test Module and GroupBinding ==="
	@tmpfile=$$(mktemp /tmp/opendepot-test-XXXXXX.yaml); \
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

## Verify that the stored tofu token grants access to the test module.
## Requires: tofu login localhost:$(OIDC_SERVER_PORT) has been run and port-forwards are active.
oidc-verify-module:
	@TOKEN=$$(python3 -c "\
	import json, sys; \
	d = json.load(open('$(HOME)/.terraform.d/credentials.tfrc.json')); \
	creds = d.get('credentials', {}); \
	key = next((k for k in creds if 'localhost' in k), None); \
	print(creds[key]['token'] if key else '')" 2>/dev/null); \
	if [ -z "$$TOKEN" ]; then \
	  echo "No token found. Run: tofu login localhost:$(OIDC_SERVER_PORT)" >&2; exit 1; \
	fi; \
	echo "=== Testing authenticated access to module versions ==="; \
	curl -sf --cacert "$$(mkcert -CAROOT)/rootCA.pem" \
	  -H "Authorization: Bearer $$TOKEN" \
	  "https://$(OIDC_REGISTRY_HOST):$(OIDC_SERVER_PORT)/opendepot/modules/v1/$(OIDC_NAMESPACE)/terraform-aws-key-pair/aws/versions" \
	  | python3 -m json.tool \
	  && echo "=== Access granted — GroupBinding is working ==="

## ─────────────────────────────────────────────────────────────────────────────

## Tag shared packages, update all go.mod files, and push. Usage: make tag-modules MODULE_VERSION=vX.Y.Z
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
