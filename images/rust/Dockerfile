# syntax=docker/dockerfile:1
ARG BASE=ghcr.io/joshualawson/dx-base:latest
FROM ${BASE}
ARG TARGETARCH
ARG RUST_VERSION=stable
ARG RUSTUP_VERSION=1.28.2
LABEL org.opencontainers.image.source="https://github.com/joshualawson/dx" \
      org.opencontainers.image.title="dx-rust"
ENV RUSTUP_HOME=/usr/local/rustup \
    CARGO_HOME=/usr/local/cargo \
    CARGO_INSTALL_ROOT=/cache/rust/install \
    PATH=/cache/rust/install/bin:/usr/local/cargo/bin:$PATH
RUN set -eux; \
    apt-get update; apt-get install -y --no-install-recommends build-essential pkg-config libssl-dev; \
    rm -rf /var/lib/apt/lists/*; \
    case "${TARGETARCH}" in amd64) arch=x86_64 ;; arm64) arch=aarch64 ;; *) exit 1 ;; esac; \
    url="https://static.rust-lang.org/rustup/archive/${RUSTUP_VERSION}/${arch}-unknown-linux-gnu/rustup-init"; \
    curl -fsSL "${url}" -o /tmp/rustup-init; \
    curl -fsSL "${url}.sha256" -o /tmp/rustup-init.sha256; \
    cd /tmp; sha256sum -c rustup-init.sha256; \
    chmod +x rustup-init; \
    ./rustup-init -y --no-modify-path --profile minimal --default-toolchain "${RUST_VERSION}" --component rustfmt --component clippy --component rust-analyzer; \
    rm rustup-init rustup-init.sha256; \
    mkdir -p /cache/rust/registry /cache/rust/git /cache/rust/install; \
    ln -s /cache/rust/registry "${CARGO_HOME}/registry"; \
    ln -s /cache/rust/git "${CARGO_HOME}/git"; \
    chmod -R a+rX "${RUSTUP_HOME}" "${CARGO_HOME}"; \
    chmod 1777 "${CARGO_HOME}" /cache/rust /cache/rust/registry /cache/rust/git /cache/rust/install
CMD ["bash"]
