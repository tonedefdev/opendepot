# Build the manager binary
FROM golang:1.26.11-bookworm@sha256:dc40a13b61304647ed7e5d581d246b86413dfc29d63c6d018310feb93cd98fc0 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Copy go.work and all module manifests for workspace-aware builds
COPY go.work go.work.sum ./
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/agentspec/go.mod pkg/agentspec/go.sum pkg/agentspec/
COPY pkg/archive/go.mod pkg/archive/
COPY pkg/jev/go.mod pkg/jev/
COPY pkg/agentassembly/go.mod pkg/agentassembly/
COPY pkg/mcp/go.mod pkg/mcp/go.sum pkg/mcp/
COPY pkg/oci/go.mod pkg/oci/go.sum pkg/oci/
COPY pkg/ocibuild/go.mod pkg/ocibuild/go.sum pkg/ocibuild/
COPY pkg/schematic/go.mod pkg/schematic/
COPY pkg/signing/go.mod pkg/signing/go.sum pkg/signing/
COPY pkg/storage/go.mod pkg/storage/go.sum pkg/storage/
COPY pkg/github/go.mod pkg/github/go.sum pkg/github/
COPY pkg/utils/go.mod pkg/utils/
COPY pkg/testutils/go.mod pkg/testutils/
COPY pkg/registry/go.mod pkg/registry/
COPY pkg/hclschema/go.mod pkg/hclschema/go.sum pkg/hclschema/
COPY services/depot/go.mod services/depot/go.sum services/depot/
COPY services/server/go.mod services/server/go.sum services/server/
COPY services/version/go.mod services/version/go.sum services/version/

# Drop test/integration from the workspace — its terratest dependency causes a grpc ambiguous import conflict
RUN go work edit -dropuse=./test/integration -dropuse=./cli -dropuse=./services/agent -dropuse=./services/iac -dropuse=./services/oci -dropuse=./pkg/agentassembly -dropuse=./pkg/mcp -dropuse=./pkg/oci -dropuse=./pkg/schematic

# Download dependencies in workspace mode so replace directives resolve local modules
WORKDIR /workspace/services/depot
RUN go mod download

WORKDIR /workspace

# Copy source
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/storage/ pkg/storage/
COPY pkg/github/ pkg/github/
COPY pkg/registry/ pkg/registry/
COPY services/depot/cmd/ services/depot/cmd/
COPY services/depot/internal/ services/depot/internal/

# Build (COVER=1 adds coverage counters and the coverage build tag)
ARG COVER=0
WORKDIR /workspace/services/depot
RUN COVER_FLAGS="" && \
    if [ "${COVER}" = "1" ]; then COVER_FLAGS="-cover -covermode=atomic -tags coverage"; fi && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build ${COVER_FLAGS} -a -o /workspace/depot-controller ./cmd

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
WORKDIR /
COPY --from=builder /workspace/depot-controller .
USER 65532:65532

ENTRYPOINT ["/depot-controller"]
