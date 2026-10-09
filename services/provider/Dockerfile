# Build the manager binary
FROM golang:1.26.6 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Copy go.work and all module manifests for workspace-aware builds
COPY go.work go.work.sum ./
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/agentspec/go.mod pkg/agentspec/go.sum pkg/agentspec/
COPY pkg/archive/go.mod pkg/archive/
COPY pkg/jev/go.mod pkg/jev/
COPY pkg/signing/go.mod pkg/signing/go.sum pkg/signing/
COPY pkg/storage/go.mod pkg/storage/go.sum pkg/storage/
COPY pkg/github/go.mod pkg/github/go.sum pkg/github/
COPY services/module/go.mod services/module/go.sum services/module/
COPY services/depot/go.mod services/depot/go.sum services/depot/
COPY services/server/go.mod services/server/go.sum services/server/
COPY services/version/go.mod services/version/go.sum services/version/
COPY services/provider/go.mod services/provider/go.sum services/provider/
COPY pkg/utils/go.mod pkg/utils/
COPY pkg/testutils/go.mod pkg/testutils/
COPY pkg/registry/go.mod pkg/registry/
COPY pkg/hclschema/go.mod pkg/hclschema/go.sum pkg/hclschema/

# Drop test/integration from the workspace — its terratest dependency causes a grpc ambiguous import conflict
RUN go work edit -dropuse=./test/integration -dropuse=./cli -dropuse=./services/agent

# Download dependencies in workspace mode so replace directives resolve local modules
RUN cd services/provider && go mod download

# Copy source
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/storage/ pkg/storage/
COPY pkg/github/ pkg/github/
COPY pkg/utils/ pkg/utils/
COPY pkg/testutils/ pkg/testutils/
COPY pkg/registry/ pkg/registry/
COPY services/provider/cmd/ services/provider/cmd/
COPY services/provider/internal/ services/provider/internal/

# Build
RUN cd services/provider && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o /workspace/provider-controller cmd/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
WORKDIR /
COPY --from=builder /workspace/provider-controller .
USER 65532:65532

ENTRYPOINT ["/provider-controller"]
