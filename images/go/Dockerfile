# syntax=docker/dockerfile:1
ARG BASE=ghcr.io/joshualawson/dx-base:latest
FROM ${BASE}
ARG TARGETARCH
ARG GO_VERSION=1.25.14
ARG GO_SHA256_AMD64=a21ae5633a269bcd7e90cf767e48225633795e99d831742cbf3397064fee7712
ARG GO_SHA256_ARM64=9bf234ea70ffec9347fdf6b22ce4add51717d3386a38a441e8c8743fceb5eaee
ARG GOPLS_VERSION=v0.20.0
ARG GOLANGCI_LINT_VERSION=v2.4.0
ARG DLV_VERSION=v1.25.0
LABEL org.opencontainers.image.source="https://github.com/joshualawson/dx" \
      org.opencontainers.image.title="dx-go"
ENV PATH=/usr/local/go/bin:$PATH \
    GOPATH=/cache/go \
    GOMODCACHE=/cache/go/pkg/mod \
    GOCACHE=/cache/go/build \
    GOLANGCI_LINT_CACHE=/cache/go/golangci-lint
RUN set -eux; \
    archive="go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"; \
    curl -fsSL "https://go.dev/dl/${archive}" -o /tmp/go.tar.gz; \
    case "${TARGETARCH}" in amd64) checksum="${GO_SHA256_AMD64}" ;; arm64) checksum="${GO_SHA256_ARM64}" ;; *) exit 1 ;; esac; \
    echo "${checksum}  /tmp/go.tar.gz" | sha256sum -c -; \
    tar -C /usr/local -xzf /tmp/go.tar.gz; \
    rm /tmp/go.tar.gz; \
    GOBIN=/usr/local/bin GOPATH=/tmp/go GOMODCACHE=/tmp/go/pkg/mod GOCACHE=/tmp/go/build go install "golang.org/x/tools/gopls@${GOPLS_VERSION}"; \
    GOBIN=/usr/local/bin GOPATH=/tmp/go GOMODCACHE=/tmp/go/pkg/mod GOCACHE=/tmp/go/build go install "github.com/go-delve/delve/cmd/dlv@${DLV_VERSION}"; \
    version="${GOLANGCI_LINT_VERSION#v}"; \
    archive="golangci-lint-${version}-linux-${TARGETARCH}.tar.gz"; \
    curl -fsSL "https://github.com/golangci/golangci-lint/releases/download/${GOLANGCI_LINT_VERSION}/${archive}" -o "/tmp/${archive}"; \
    curl -fsSL "https://github.com/golangci/golangci-lint/releases/download/${GOLANGCI_LINT_VERSION}/golangci-lint-${version}-checksums.txt" -o /tmp/checksums; \
    cd /tmp; grep " ${archive}\$" checksums | sha256sum -c -; \
    tar -xzf "${archive}"; \
    install "golangci-lint-${version}-linux-${TARGETARCH}/golangci-lint" /usr/local/bin/golangci-lint; \
    rm -rf /tmp/go /tmp/golangci-lint-* /tmp/checksums; \
    mkdir -p /cache/go; chmod 1777 /cache/go
ENV PATH=/cache/go/bin:$PATH \
    GOPLSCACHE=/cache/go/gopls
CMD ["bash"]
