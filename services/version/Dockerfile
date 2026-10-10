# select final stage via build args (defaults: no Trivy, no OpenTofu)
ARG INCLUDE_TRIVY=false
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
COPY pkg/signing/go.mod pkg/signing/go.sum pkg/signing/
COPY pkg/github/go.mod pkg/github/go.sum pkg/github/
COPY pkg/utils/go.mod pkg/utils/
COPY pkg/testutils/go.mod pkg/testutils/
COPY pkg/registry/go.mod pkg/registry/
COPY pkg/hclschema/go.mod pkg/hclschema/go.sum pkg/hclschema/
COPY services/version/go.mod services/version/go.sum services/version/
COPY services/depot/go.mod services/depot/go.sum services/depot/
COPY services/module/go.mod services/module/go.sum services/module/
COPY services/provider/go.mod services/provider/go.sum services/provider/
COPY services/server/go.mod services/server/go.sum services/server/

# Drop test/integration from the workspace — its terratest dependency causes a grpc ambiguous import conflict
RUN go work edit -dropuse=./test/integration -dropuse=./cli -dropuse=./services/agent

# Download dependencies in workspace mode so replace directives resolve local modules
RUN cd services/version && go mod download

# Copy source
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/storage/ pkg/storage/
COPY pkg/agentspec/ pkg/agentspec/
COPY pkg/archive/ pkg/archive/
COPY pkg/jev/ pkg/jev/
COPY pkg/signing/ pkg/signing/
COPY pkg/github/ pkg/github/
COPY pkg/utils/ pkg/utils/
COPY pkg/registry/ pkg/registry/
COPY pkg/hclschema/ pkg/hclschema/
COPY services/version/cmd/ services/version/cmd/
COPY services/version/internal/ services/version/internal/
COPY services/version/checks/ services/version/checks/

# Build
RUN cd services/version && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o /workspace/version-controller cmd/main.go

# trivy-release: upstream Trivy release binary, verified against the release checksum file; only copied into the final image when INCLUDE_TRIVY=true
FROM debian:bookworm-slim AS trivy-release
ARG TARGETARCH
ARG TRIVY_VERSION=0.75.0
RUN set -eux; \
  apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/*; \
  case "${TARGETARCH}" in amd64) trivy_arch=64bit ;; arm64) trivy_arch=ARM64 ;; *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; esac; \
  base="https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}"; \
  archive="trivy_${TRIVY_VERSION}_Linux-${trivy_arch}.tar.gz"; \
  cd /tmp; \
  curl -fsSL -o "${archive}" "${base}/${archive}"; \
  curl -fsSL -o checksums.txt "${base}/trivy_${TRIVY_VERSION}_checksums.txt"; \
  grep " ${archive}\$" checksums.txt | sha256sum -c -; \
  tar -xzf "${archive}" -C /usr/local/bin trivy; \
  chmod 0755 /usr/local/bin/trivy

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

# final image without Trivy or OpenTofu binaries
FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 AS final-false-false
WORKDIR /
COPY --from=builder /workspace/version-controller .
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# final image with OpenTofu binary bundled (assembly line)
FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 AS final-false-true
WORKDIR /
COPY --from=builder /workspace/version-controller .
COPY --from=tofu-release /usr/local/bin/tofu /usr/local/bin/tofu
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# final image with Trivy binary bundled
FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 AS final-true-false
WORKDIR /
COPY --from=builder /workspace/version-controller .
COPY --from=trivy-release /usr/local/bin/trivy /usr/local/bin/trivy
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# final image with Trivy and OpenTofu binaries bundled
FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 AS final-true-true
WORKDIR /
COPY --from=builder /workspace/version-controller .
COPY --from=trivy-release /usr/local/bin/trivy /usr/local/bin/trivy
COPY --from=tofu-release /usr/local/bin/tofu /usr/local/bin/tofu
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# select which final stage was built
FROM final-${INCLUDE_TRIVY}-${INCLUDE_TOFU}
