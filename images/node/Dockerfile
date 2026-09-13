# syntax=docker/dockerfile:1
ARG BASE=ghcr.io/joshualawson/dx-base:latest
FROM ${BASE}
ARG TARGETARCH
ARG NODE_VERSION=22.19.0
ARG COREPACK_VERSION=0.34.0
ARG PNPM_VERSION=10.17.0
ARG TYPESCRIPT_VERSION=5.9.2
ARG TSX_VERSION=4.20.5
ARG ESLINT_VERSION=9.35.0
ARG PRETTIER_VERSION=3.6.2
LABEL org.opencontainers.image.source="https://github.com/joshualawson/dx" \
      org.opencontainers.image.title="dx-node"
ENV npm_config_cache=/cache/node/npm \
    COREPACK_HOME=/cache/node/corepack \
    COREPACK_DEFAULT_TO_LATEST=0 \
    COREPACK_ENABLE_AUTO_PIN=0 \
    PNPM_HOME=/cache/node/pnpm/bin \
    PATH=/cache/node/pnpm/bin:$PATH
RUN set -eux; \
    case "${TARGETARCH}" in amd64) arch=x64 ;; arm64) arch=arm64 ;; *) exit 1 ;; esac; \
    archive="node-v${NODE_VERSION}-linux-${arch}.tar.xz"; \
    curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/${archive}" -o "/tmp/${archive}"; \
    curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/SHASUMS256.txt" -o /tmp/checksums; \
    cd /tmp; grep " ${archive}\$" checksums | sha256sum -c -; \
    tar -xJf "${archive}" -C /usr/local --strip-components=1; \
    rm "${archive}" checksums; \
    npm_config_cache=/tmp/npm npm install --global "corepack@${COREPACK_VERSION}" "typescript@${TYPESCRIPT_VERSION}" "tsx@${TSX_VERSION}" "eslint@${ESLINT_VERSION}" "prettier@${PRETTIER_VERSION}"; \
    corepack enable; \
    COREPACK_HOME=/usr/local/share/corepack corepack prepare "pnpm@${PNPM_VERSION}" --activate; \
    mkdir -p /cache/node/corepack /usr/local/etc; \
    cp -a /usr/local/share/corepack/. /cache/node/corepack/; \
    printf 'store-dir=/cache/node/pnpm/store\n' > /usr/local/etc/npmrc; \
    chmod -R a+rwX /cache/node/corepack; chmod 1777 /cache/node /cache/node/corepack; \
    rm -rf /tmp/npm
CMD ["bash"]
