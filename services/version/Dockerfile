# select final stage via build args (defaults: no Trivy, no OpenTofu)
ARG INCLUDE_TRIVY=false
ARG INCLUDE_TOFU=false

# Build the manager binary
FROM golang:1.26.6 AS builder
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

# trivy-source: built from an immutable upstream commit; only copied into the final image when INCLUDE_TRIVY=true
FROM golang:1.27.1 AS trivy-source
ARG TRIVY_COMMIT=8f815546c7b57dc11229b0098463dff0cfc9ed2c
RUN set -eux; \
  apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/*; \
  curl -fsSL -o /tmp/trivy.tar.gz "https://github.com/aquasecurity/trivy/archive/${TRIVY_COMMIT}.tar.gz"; \
  mkdir /tmp/trivy; \
  tar --strip-components=1 -xzf /tmp/trivy.tar.gz -C /tmp/trivy; \
  cd /tmp/trivy; \
  CGO_ENABLED=0 go build -trimpath -o /usr/local/bin/trivy ./cmd/trivy

# tofu-source: supplies the statically linked patched tofu binary used to extract provider schemas; only copied into the final image when INCLUDE_TOFU=true.
FROM golang:1.27.1 AS tofu-source
ARG TOFU_COMMIT=9ffbb1c211bfb36775c5923809a18289396ee0c1
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
RUN set -eux; \
    curl -fsSL -o /tmp/opentofu.tar.gz "https://github.com/opentofu/opentofu/archive/${TOFU_COMMIT}.tar.gz"; \
    mkdir /tmp/opentofu; \
    tar --strip-components=1 -xzf /tmp/opentofu.tar.gz -C /tmp/opentofu; \
    cd /tmp/opentofu; \
    sed -i -E 's/(golang.org\/x\/mod[[:space:]]+)v[0-9.]+/\1v0.40.0/; s/(google.golang.org\/grpc[[:space:]]+)v[0-9.]+/\1v1.83.2/' go.mod; \
    GOWORK=off go mod tidy; \
    GOWORK=off CGO_ENABLED=0 go build -trimpath -o /usr/local/bin/tofu ./cmd/tofu

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
