# select final stage via build args (defaults: no Trivy, no OpenTofu, no MCP runtimes)
ARG INCLUDE_TRIVY=false
ARG INCLUDE_TOFU=false
ARG INCLUDE_MCP_RUNTIMES=false

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
COPY pkg/storage/go.mod pkg/storage/go.sum pkg/storage/
COPY pkg/signing/go.mod pkg/signing/go.sum pkg/signing/
COPY pkg/github/go.mod pkg/github/go.sum pkg/github/
COPY pkg/utils/go.mod pkg/utils/
COPY pkg/testutils/go.mod pkg/testutils/
COPY pkg/registry/go.mod pkg/registry/
COPY pkg/hclschema/go.mod pkg/hclschema/go.sum pkg/hclschema/
COPY pkg/mcp/go.mod pkg/mcp/go.sum pkg/mcp/
COPY pkg/oci/go.mod pkg/oci/go.sum pkg/oci/
COPY pkg/agentassembly/go.mod pkg/agentassembly/
COPY pkg/schematic/go.mod pkg/schematic/
COPY services/version/go.mod services/version/go.sum services/version/
COPY services/depot/go.mod services/depot/go.sum services/depot/
COPY services/server/go.mod services/server/go.sum services/server/

# Drop test/integration from the workspace — its terratest dependency causes a grpc ambiguous import conflict
RUN go work edit -dropuse=./test/integration -dropuse=./cli -dropuse=./services/agent -dropuse=./services/iac -dropuse=./services/oci -dropuse=./pkg/agentassembly -dropuse=./pkg/mcp -dropuse=./pkg/oci -dropuse=./pkg/schematic

# Download dependencies in workspace mode so replace directives resolve local modules
WORKDIR /workspace/services/version
RUN go mod download

WORKDIR /workspace

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

# Build (COVER=1 adds coverage counters and the coverage build tag)
ARG COVER=0
WORKDIR /workspace/services/version
RUN COVER_FLAGS="" && \
    if [ "${COVER}" = "1" ]; then COVER_FLAGS="-cover -covermode=atomic -tags coverage"; fi && \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build ${COVER_FLAGS} -a -o /workspace/version-controller ./cmd

# trivy-release: upstream Trivy release binary, verified against the release checksum file; only copied into the final image when INCLUDE_TRIVY=true
FROM debian:bookworm-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc AS trivy-release
ARG TARGETARCH
ARG TRIVY_VERSION=0.76.2
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
FROM debian:bookworm-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc AS tofu-release
ARG TARGETARCH
ARG TOFU_VERSION=1.13.10
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

# runtime-false: distroless runtime for the default image (no Node.js or Python)
FROM gcr.io/distroless/static:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329 AS runtime-false

# node-release: upstream Node.js release, verified against the release checksum file; only copied into runtime-true when INCLUDE_MCP_RUNTIMES=true
FROM debian:bookworm-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc AS node-release
ARG TARGETARCH
ARG NODE_VERSION=26.13.1
RUN set -eux; \
  apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/*; \
  case "${TARGETARCH}" in amd64) node_arch=x64 ;; arm64) node_arch=arm64 ;; *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; esac; \
  base="https://nodejs.org/dist/v${NODE_VERSION}"; \
  archive="node-v${NODE_VERSION}-linux-${node_arch}.tar.gz"; \
  cd /tmp; \
  curl -fsSL -o "${archive}" "${base}/${archive}"; \
  curl -fsSL -o SHASUMS256.txt "${base}/SHASUMS256.txt"; \
  grep " ${archive}\$" SHASUMS256.txt | sha256sum -c -; \
  mkdir -p /opt/node; \
  tar -xzf "${archive}" -C /opt/node --strip-components=1; \
  rm -f "${archive}" SHASUMS256.txt

# runtime-true: Node.js and Python with pip3 for MCP vendoring. Node.js is copied from the verified node-release stage, so curl is not installed here.
FROM python:3.13-slim-bookworm@sha256:f0fa218d5b06f9b9235024fa061cde2cb141f726de52134471053d2d9b5324a3 AS runtime-true
COPY --from=node-release /opt/node/ /usr/local/
RUN set -eux; \
	apt-get update \
	&& apt-get install -y --no-install-recommends libatomic1 \
	&& rm -rf /var/lib/apt/lists/*; \
	npm install -g npm@12.2.3; \
	npm cache clean --force; \
	pip3 install --no-cache-dir --upgrade pip==26.3 setuptools==84.2.1; \
	node --version; \
	npm --version; \
	python3 --version; \
	pip3 --version

# final image without Trivy or OpenTofu binaries
FROM runtime-${INCLUDE_MCP_RUNTIMES} AS final-false-false
WORKDIR /
COPY --from=builder /workspace/version-controller .
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# final image with OpenTofu binary bundled (assembly line)
FROM runtime-${INCLUDE_MCP_RUNTIMES} AS final-false-true
WORKDIR /
COPY --from=builder /workspace/version-controller .
COPY --from=tofu-release /usr/local/bin/tofu /usr/local/bin/tofu
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# final image with Trivy binary bundled
FROM runtime-${INCLUDE_MCP_RUNTIMES} AS final-true-false
WORKDIR /
COPY --from=builder /workspace/version-controller .
COPY --from=trivy-release /usr/local/bin/trivy /usr/local/bin/trivy
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# final image with Trivy and OpenTofu binaries bundled
FROM runtime-${INCLUDE_MCP_RUNTIMES} AS final-true-true
WORKDIR /
COPY --from=builder /workspace/version-controller .
COPY --from=trivy-release /usr/local/bin/trivy /usr/local/bin/trivy
COPY --from=tofu-release /usr/local/bin/tofu /usr/local/bin/tofu
USER 65532:65532
ENTRYPOINT ["/version-controller"]

# select which final stage was built
FROM final-${INCLUDE_TRIVY}-${INCLUDE_TOFU}
