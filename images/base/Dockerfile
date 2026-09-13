# syntax=docker/dockerfile:1
ARG DOCKER_VERSION=28.4.0
FROM docker:${DOCKER_VERSION}-cli AS docker
FROM --platform=$BUILDPLATFORM golang:1.25 AS idle
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd/dx-idle ./cmd/dx-idle
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /dx-idle ./cmd/dx-idle

FROM debian:bookworm-slim
LABEL org.opencontainers.image.source="https://github.com/joshualawson/dx" \
      org.opencontainers.image.title="dx-base"
RUN apt-get update && apt-get install -y --no-install-recommends \
      git curl ca-certificates bash openssh-client make less procps xz-utils \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 dx && useradd --uid 1000 --gid 1000 --create-home --shell /bin/bash dx
COPY --from=docker /usr/local/bin/docker /usr/local/bin/docker
COPY --from=docker /usr/local/libexec/docker/cli-plugins/docker-compose /usr/local/libexec/docker/cli-plugins/docker-compose
COPY --from=idle /dx-idle /usr/local/bin/dx-idle
CMD ["bash"]
