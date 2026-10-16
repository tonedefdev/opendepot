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
COPY pkg/storage/go.mod pkg/storage/go.sum pkg/storage/
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
RUN go work edit -dropuse=./test/integration

# Download dependencies in workspace mode so replace directives resolve local modules
RUN cd services/version && go mod download

# Copy source
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/storage/ pkg/storage/
COPY pkg/github/ pkg/github/
COPY pkg/utils/ pkg/utils/
COPY pkg/registry/ pkg/registry/
COPY pkg/hclschema/ pkg/hclschema/
COPY services/version/cmd/ services/version/cmd/
COPY services/version/internal/ services/version/internal/

# Build
RUN cd services/version && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o /workspace/version-controller cmd/main.go

# trivy-source: Trivy release binary, verified against the upstream release checksum; only copied into the final image when INCLUDE_TRIVY=true
FROM golang:1.27.2 AS trivy-source
ARG TARGETARCH
ARG TRIVY_VERSION=0.76.0
ARG TRIVY_SHA256_AMD64=e24b1c51293c1b15e4ba8d45bbc7891ba053f90278ec2698f3562c993e145fe7
ARG TRIVY_SHA256_ARM64=aab21e677a7b7071fc79f1711480840754c18a1b9d9cefa7ccd06af63a0e8f4c
RUN set -eux; \
  apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/*; \
  arch="${TARGETARCH:-amd64}"; \
  case "${arch}" in \
    amd64) asset=64bit; sha="${TRIVY_SHA256_AMD64}" ;; \
    arm64) asset=ARM64; sha="${TRIVY_SHA256_ARM64}" ;; \
    *) echo "unsupported architecture: ${arch}" >&2; exit 1 ;; \
  esac; \
  curl -fsSL -o /tmp/trivy.tar.gz "https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}/trivy_${TRIVY_VERSION}_Linux-${asset}.tar.gz"; \
  echo "${sha}  /tmp/trivy.tar.gz" | sha256sum -c -; \
  tar -xzf /tmp/trivy.tar.gz -C /usr/local/bin trivy


# OpenTofu release binary, verified against the upstream release checksum; only copied into the final image when INCLUDE_TOFU=true.
FROM golang:1.27.2 AS tofu-source
ARG TARGETARCH
ARG TOFU_VERSION=1.13.1
ARG TOFU_SHA256_AMD64=8ccbc6f8ee21d2827715f3c6e08a9b3e0209b1e62057c05067ef117e047c1a80
ARG TOFU_SHA256_ARM64=b9614df40575cc3fc10a8a25025b7245d961da279f715ea3efff4ddae8e6938a
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl unzip \
    && rm -rf /var/lib/apt/lists/*
RUN set -eux; \
    arch="${TARGETARCH:-amd64}"; \
    case "${arch}" in \
        amd64) sha="${TOFU_SHA256_AMD64}" ;; \
        arm64) sha="${TOFU_SHA256_ARM64}" ;; \
        *) echo "unsupported architecture: ${arch}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/tofu.zip "https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}/tofu_${TOFU_VERSION}_linux_${arch}.zip"; \
    echo "${sha}  /tmp/tofu.zip" | sha256sum -c -; \
    unzip -q /tmp/tofu.zip tofu -d /usr/local/bin; \
    chmod 0755 /usr/local/bin/tofu


# final image without Trivy or OpenTofu binaries
FROM gcr.io/distroless/static:nonroot AS final-false-false
WORKDIR /
COPY --from=builder /workspace/version-controller .
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# final image with OpenTofu binary bundled (assembly line)
FROM gcr.io/distroless/static:nonroot AS final-false-true
WORKDIR /
COPY --from=builder /workspace/version-controller .
COPY --from=tofu-source /usr/local/bin/tofu /usr/local/bin/tofu
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# final image with Trivy binary bundled
FROM gcr.io/distroless/static:nonroot AS final-true-false
WORKDIR /
COPY --from=builder /workspace/version-controller .
COPY --from=trivy-source /usr/local/bin/trivy /usr/local/bin/trivy
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# final image with Trivy and OpenTofu binaries bundled
FROM gcr.io/distroless/static:nonroot AS final-true-true
WORKDIR /
COPY --from=builder /workspace/version-controller .
COPY --from=trivy-source /usr/local/bin/trivy /usr/local/bin/trivy
COPY --from=tofu-source /usr/local/bin/tofu /usr/local/bin/tofu
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# select which final stage was built
FROM final-${INCLUDE_TRIVY}-${INCLUDE_TOFU}
