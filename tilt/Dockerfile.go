FROM aquasec/trivy:0.74.0 AS trivy-source

FROM debian:bookworm-slim AS tofu-source
ARG TARGETARCH
ARG TOFU_VERSION=1.12.6
ARG TOFU_SHA256_AMD64=5dc43da4f750f33873dc25e94587128709e819e544b7be9016b255316153c3a8
ARG TOFU_SHA256_ARM64=e573979ba68a17fe7b881752051a694a7efcd970e39521f6a25775197861ed4d
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl unzip \
  && rm -rf /var/lib/apt/lists/*
RUN set -eux; \
  arch="${TARGETARCH:-amd64}"; \
  case "$arch" in \
    amd64) sha="$TOFU_SHA256_AMD64" ;; \
    arm64) sha="$TOFU_SHA256_ARM64" ;; \
    *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
  esac; \
  curl -fsSL -o /tmp/tofu.zip \
    "https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}/tofu_${TOFU_VERSION}_linux_${arch}.zip"; \
  echo "${sha}  /tmp/tofu.zip" | sha256sum -c -; \
  unzip -o /tmp/tofu.zip tofu -d /usr/local/bin; \
  chmod 0755 /usr/local/bin/tofu

FROM golang:1.26.6 AS server-runtime
WORKDIR /workspace
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/hclschema/go.mod pkg/hclschema/go.sum pkg/hclschema/
COPY pkg/storage/go.mod pkg/storage/go.sum pkg/storage/
COPY pkg/utils/go.mod pkg/utils/go.sum pkg/utils/
COPY pkg/signing/go.mod pkg/signing/go.sum pkg/signing/
COPY services/server/go.mod services/server/go.sum services/server/
RUN go work init ./api/v1alpha1 ./pkg/hclschema ./pkg/storage ./pkg/utils ./pkg/signing ./services/server \
  && cd services/server && go mod download
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/hclschema/ pkg/hclschema/
COPY pkg/storage/ pkg/storage/
COPY pkg/utils/ pkg/utils/
COPY pkg/signing/ pkg/signing/
COPY services/server/ services/server/
RUN cd services/server && CGO_ENABLED=0 go build -o /workspace/bin/server . \
  && chown -R 65532:65532 /workspace
COPY --from=tofu-source /usr/local/bin/tofu /usr/local/bin/tofu
ENV GOCACHE=/workspace/.cache/go-build
USER 65532:65532

FROM golang:1.26.6 AS depot-runtime
WORKDIR /workspace
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/github/go.mod pkg/github/go.sum pkg/github/
COPY pkg/registry/go.mod pkg/registry/
COPY services/depot/go.mod services/depot/go.sum services/depot/
RUN go work init ./api/v1alpha1 ./pkg/github ./pkg/registry ./services/depot \
  && cd services/depot && go mod download
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/github/ pkg/github/
COPY pkg/registry/ pkg/registry/
COPY services/depot/ services/depot/
RUN cd services/depot && CGO_ENABLED=0 go build -o /workspace/bin/depot-controller ./cmd \
  && chown -R 65532:65532 /workspace
ENV GOCACHE=/workspace/.cache/go-build
USER 65532:65532

FROM golang:1.26.6 AS module-runtime
WORKDIR /workspace
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/utils/go.mod pkg/utils/go.sum pkg/utils/
COPY services/module/go.mod services/module/go.sum services/module/
RUN go work init ./api/v1alpha1 ./pkg/utils ./services/module \
  && cd services/module && go mod download
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/utils/ pkg/utils/
COPY services/module/ services/module/
RUN cd services/module && CGO_ENABLED=0 go build -o /workspace/bin/module-controller ./cmd \
  && chown -R 65532:65532 /workspace
ENV GOCACHE=/workspace/.cache/go-build
USER 65532:65532

FROM golang:1.26.6 AS agent-runtime
WORKDIR /workspace
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/utils/go.mod pkg/utils/go.sum pkg/utils/
COPY services/agent/go.mod services/agent/go.sum services/agent/
RUN go work init ./api/v1alpha1 ./pkg/utils ./services/agent \
  && cd services/agent && go mod download
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/utils/ pkg/utils/
COPY services/agent/ services/agent/
RUN cd services/agent && CGO_ENABLED=0 go build -o /workspace/bin/agent-controller ./cmd \
  && chown -R 65532:65532 /workspace
ENV GOCACHE=/workspace/.cache/go-build
USER 65532:65532

FROM golang:1.26.6 AS provider-runtime
WORKDIR /workspace
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/utils/go.mod pkg/utils/go.sum pkg/utils/
COPY services/provider/go.mod services/provider/go.sum services/provider/
RUN go work init ./api/v1alpha1 ./pkg/utils ./services/provider \
  && cd services/provider && go mod download
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/utils/ pkg/utils/
COPY services/provider/ services/provider/
RUN cd services/provider && CGO_ENABLED=0 go build -o /workspace/bin/provider-controller ./cmd \
  && chown -R 65532:65532 /workspace
ENV GOCACHE=/workspace/.cache/go-build
USER 65532:65532

FROM golang:1.26.6 AS version-runtime
WORKDIR /workspace
COPY api/v1alpha1/go.mod api/v1alpha1/go.sum api/v1alpha1/
COPY pkg/github/go.mod pkg/github/go.sum pkg/github/
COPY pkg/registry/go.mod pkg/registry/
COPY pkg/storage/go.mod pkg/storage/go.sum pkg/storage/
COPY pkg/hclschema/go.mod pkg/hclschema/go.sum pkg/hclschema/
COPY pkg/utils/go.mod pkg/utils/go.sum pkg/utils/
COPY pkg/agentspec/go.mod pkg/agentspec/go.sum pkg/agentspec/
COPY pkg/archive/go.mod pkg/archive/
COPY pkg/jev/go.mod pkg/jev/
COPY pkg/signing/go.mod pkg/signing/go.sum pkg/signing/
COPY services/version/go.mod services/version/go.sum services/version/
RUN go work init ./api/v1alpha1 ./pkg/github ./pkg/registry ./pkg/storage ./pkg/hclschema ./pkg/utils ./pkg/agentspec ./pkg/archive ./pkg/jev ./pkg/signing ./services/version \
  && cd services/version && go mod download
COPY api/v1alpha1/ api/v1alpha1/
COPY pkg/github/ pkg/github/
COPY pkg/registry/ pkg/registry/
COPY pkg/storage/ pkg/storage/
COPY pkg/hclschema/ pkg/hclschema/
COPY pkg/utils/ pkg/utils/
COPY pkg/agentspec/ pkg/agentspec/
COPY pkg/archive/ pkg/archive/
COPY pkg/jev/ pkg/jev/
COPY pkg/signing/ pkg/signing/
COPY services/version/ services/version/
RUN cd services/version && CGO_ENABLED=0 go build -o /workspace/bin/version-controller ./cmd \
  && chown -R 65532:65532 /workspace
COPY --from=tofu-source /usr/local/bin/tofu /usr/local/bin/tofu
ENV GOCACHE=/workspace/.cache/go-build
USER 65532:65532

FROM version-runtime AS version-dev
USER 0
COPY --from=trivy-source /usr/local/bin/trivy /usr/local/bin/trivy
USER 65532:65532