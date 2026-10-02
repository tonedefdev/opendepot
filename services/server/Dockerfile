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
COPY services/server/go.mod services/server/go.sum services/server/
COPY services/depot/go.mod services/depot/go.sum services/depot/
COPY services/module/go.mod services/module/go.sum services/module/
COPY services/version/go.mod services/version/go.sum services/version/
COPY services/provider/go.mod services/provider/go.sum services/provider/

# Drop test/integration from the workspace — its terratest dependency causes a grpc ambiguous import conflict
RUN go work edit -dropuse=./test/integration

# Download dependencies in workspace mode so replace directives resolve local modules
RUN cd services/server && go mod download

# Copy source
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/storage/ pkg/storage/
COPY pkg/github/ pkg/github/
COPY pkg/utils/ pkg/utils/
COPY pkg/registry/ pkg/registry/
COPY pkg/hclschema/ pkg/hclschema/
COPY services/server/ services/server/

# Build
RUN cd services/server && CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o /workspace/server .

# Supply the same patched OpenTofu binary used by the version controller.
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

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/server .
COPY --from=tofu-source /usr/local/bin/tofu /usr/local/bin/tofu
USER 65532:65532

ENTRYPOINT ["/server"]
