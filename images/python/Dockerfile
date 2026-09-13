# syntax=docker/dockerfile:1
ARG BASE=ghcr.io/joshualawson/dx-base:latest
ARG UV_VERSION=0.8.17
FROM ghcr.io/astral-sh/uv:${UV_VERSION} AS uv
FROM ${BASE}
ARG PYTHON_VERSION=3.13
ARG RUFF_VERSION=0.12.12
LABEL org.opencontainers.image.source="https://github.com/joshualawson/dx" \
      org.opencontainers.image.title="dx-python"
COPY --from=uv /uv /uvx /usr/local/bin/
ENV UV_CACHE_DIR=/cache/python/uv \
    PIP_CACHE_DIR=/cache/python/pip \
    RUFF_CACHE_DIR=/cache/python/ruff \
    UV_PYTHON_INSTALL_DIR=/usr/local/share/uv/python \
    UV_TOOL_DIR=/cache/python/tools \
    UV_TOOL_BIN_DIR=/cache/python/bin \
    PATH=/cache/python/bin:$PATH
RUN set -eux; \
    UV_CACHE_DIR=/tmp/uv UV_PYTHON_BIN_DIR=/usr/local/bin uv python install --default "${PYTHON_VERSION}"; \
    UV_CACHE_DIR=/tmp/uv UV_TOOL_DIR=/usr/local/share/uv/tools UV_TOOL_BIN_DIR=/usr/local/bin uv tool install --python "${PYTHON_VERSION}" "ruff==${RUFF_VERSION}"; \
    chmod -R a+rX /usr/local/share/uv; \
    mkdir -p /cache/python; chmod 1777 /cache/python; \
    rm -rf /tmp/uv
ENV UV_LINK_MODE=copy
CMD ["bash"]
