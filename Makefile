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

## Tag shared packages, update all go.mod files, and push. Usage: make tag-modules MODULE_VERSION=vX.Y.Z
## Only importable packages are tagged (api/v1alpha1, pkg/*) — services are excluded.
## Steps: update go.mod files → work-tidy → commit → tag → push commit + tags.
MODULE_VERSION ?= $(error MODULE_VERSION is required. Usage: make tag-modules MODULE_VERSION=vX.Y.Z)
MODULE_PACKAGES := api/v1alpha1 pkg/github pkg/storage pkg/testutils pkg/utils
.PHONY: tag-modules
tag-modules:
	@if ! git diff --cached --quiet || ! git diff --quiet; then \
	  echo "ERROR: uncommitted changes detected. Commit or stash before tagging." && exit 1; \
	fi
	@echo "=== Creating tags ==="
	@for pkg in $(MODULE_PACKAGES); do \
	  git tag "$${pkg}/$(MODULE_VERSION)" 2>/dev/null && echo "  tagged $${pkg}/$(MODULE_VERSION)" || echo "  tag $${pkg}/$(MODULE_VERSION) already exists, skipping"; \
	done
	@echo "=== Pushing tags ==="
	@git push origin $(foreach pkg,$(MODULE_PACKAGES),$(pkg)/$(MODULE_VERSION)) || true
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
