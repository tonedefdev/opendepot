# select final stage via build arg (default: no OpenTofu)
ARG INCLUDE_TOFU=false

# Build the manager binary
FROM golang:1.26.9 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Copy go.work and all module manifests for workspace-aware builds
COPY go.work go.work.sum ./
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/agentspec/go.mod pkg/agentspec/go.sum pkg/agentspec/
COPY pkg/archive/go.mod pkg/archive/
COPY pkg/jev/go.mod pkg/jev/
COPY pkg/storage/go.mod pkg/storage/go.sum pkg/storage/
COPY pkg/github/go.mod pkg/github/go.sum pkg/github/
COPY pkg/utils/go.mod pkg/utils/
COPY pkg/testutils/go.mod pkg/testutils/
COPY pkg/registry/go.mod pkg/registry/
COPY pkg/hclschema/go.mod pkg/hclschema/go.sum pkg/hclschema/
COPY pkg/signing/go.mod pkg/signing/go.sum pkg/signing/
COPY services/server/go.mod services/server/go.sum services/server/
COPY services/depot/go.mod services/depot/go.sum services/depot/
COPY services/module/go.mod services/module/go.sum services/module/
COPY services/version/go.mod services/version/go.sum services/version/
COPY services/provider/go.mod services/provider/go.sum services/provider/

# Drop test/integration from the workspace — its terratest dependency causes a grpc ambiguous import conflict
RUN go work edit -dropuse=./test/integration -dropuse=./cli -dropuse=./services/agent

# Download dependencies in workspace mode so replace directives resolve local modules
RUN cd services/server && go mod download

# Copy source
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/storage/ pkg/storage/
COPY pkg/github/ pkg/github/
COPY pkg/utils/ pkg/utils/
COPY pkg/registry/ pkg/registry/
COPY pkg/hclschema/ pkg/hclschema/
COPY pkg/signing/ pkg/signing/
COPY services/server/ services/server/

# Build
RUN cd services/server && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o /workspace/server .

# tofu-release: upstream OpenTofu release binary, verified against the release checksum file; only copied into the final image when INCLUDE_TOFU=true
FROM debian:bookworm-slim AS tofu-release
ARG TARGETARCH
ARG TOFU_VERSION=1.13.1
RUN set -eux; \
	apt-get update \
	&& apt-get install -y --no-install-recommends ca-certificates curl unzip \
	&& rm -rf /var/lib/apt/lists/*; \
	base="https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}"; \
	archive="tofu_${TOFU_VERSION}_linux_${TARGETARCH}.zip"; \
	cd /tmp; \
	curl -fsSL -o "${archive}" "${base}/${archive}"; \
	curl -fsSL -o SHA256SUMS "${base}/tofu_${TOFU_VERSION}_SHA256SUMS"; \
	grep " ${archive}\$" SHA256SUMS | sha256sum -c -; \
	unzip -o "${archive}" tofu -d /usr/local/bin; \
	chmod 0755 /usr/local/bin/tofu

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 AS final-false
WORKDIR /
COPY --from=builder /workspace/server .
USER 65532:65532

ENTRYPOINT ["/server"]

# final image with OpenTofu binary bundled (assembly line)
FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 AS final-true
WORKDIR /
COPY --from=builder /workspace/server .
COPY --from=tofu-release /usr/local/bin/tofu /usr/local/bin/tofu
USER 65532:65532

ENTRYPOINT ["/server"]

# select which final stage was built (default: no OpenTofu)
FROM final-${INCLUDE_TOFU}
