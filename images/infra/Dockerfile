# syntax=docker/dockerfile:1
ARG BASE=ghcr.io/joshualawson/dx-base:latest
FROM ${BASE}
ARG TARGETARCH
ARG TERRAFORM_VERSION=1.13.2
ARG KUBECTL_VERSION=1.34.1
ARG HELM_VERSION=3.19.0
ARG AWS_CLI_VERSION=2.30.0
ARG GCLOUD_VERSION=539.0.0
ARG AZURE_CLI_VERSION=2.77.0
LABEL org.opencontainers.image.source="https://github.com/joshualawson/dx" \
      org.opencontainers.image.title="dx-infra"
ENV TF_PLUGIN_CACHE_DIR=/cache/infra/terraform/plugins \
    CHECKPOINT_DISABLE=1 \
    HELM_CACHE_HOME=/cache/infra/helm/cache \
    AWS_PAGER="" \
    AWS_CLI_AUTO_PROMPT=off \
    CLOUDSDK_CORE_DISABLE_PROMPTS=1 \
    CLOUDSDK_CORE_DISABLE_USAGE_REPORTING=true \
    CLOUDSDK_COMPONENT_MANAGER_DISABLE_UPDATE_CHECK=true \
    AZURE_CORE_COLLECT_TELEMETRY=false
RUN set -eux; \
    apt-get update; apt-get install -y --no-install-recommends unzip gnupg python3; \
    curl -fsSL https://packages.microsoft.com/keys/microsoft.asc | gpg --dearmor -o /usr/share/keyrings/microsoft.gpg; \
    echo "deb [arch=${TARGETARCH} signed-by=/usr/share/keyrings/microsoft.gpg] https://packages.microsoft.com/repos/azure-cli/ bookworm main" > /etc/apt/sources.list.d/azure-cli.list; \
    apt-get update; apt-get install -y --no-install-recommends "azure-cli=${AZURE_CLI_VERSION}-1~bookworm"; \
    rm -rf /var/lib/apt/lists/*; \
    mkdir -p /tmp/infra; cd /tmp/infra; \
    archive="terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip"; \
    curl -fsSLO "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/${archive}"; \
    curl -fsSL "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_SHA256SUMS" -o checksums; \
    grep " ${archive}\$" checksums | sha256sum -c -; \
    unzip "${archive}" terraform; install terraform /usr/local/bin/terraform; \
    curl -fsSL "https://dl.k8s.io/release/v${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl" -o kubectl; \
    checksum="$(curl -fsSL "https://dl.k8s.io/release/v${KUBECTL_VERSION}/bin/linux/${TARGETARCH}/kubectl.sha256")"; \
    echo "${checksum}  kubectl" | sha256sum -c -; install kubectl /usr/local/bin/kubectl; \
    archive="helm-v${HELM_VERSION}-linux-${TARGETARCH}.tar.gz"; \
    curl -fsSLO "https://get.helm.sh/${archive}"; \
    curl -fsSL "https://get.helm.sh/${archive}.sha256sum" -o checksums; \
    sha256sum -c checksums; tar -xzf "${archive}"; install "linux-${TARGETARCH}/helm" /usr/local/bin/helm; \
    case "${TARGETARCH}" in amd64) aws_arch=x86_64; gcloud_arch=x86_64 ;; arm64) aws_arch=aarch64; gcloud_arch=arm ;; *) exit 1 ;; esac; \
    curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-${aws_arch}-${AWS_CLI_VERSION}.zip" -o aws.zip; \
    unzip -q aws.zip; ./aws/install --bin-dir /usr/local/bin; \
    curl -fsSL "https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/google-cloud-cli-${GCLOUD_VERSION}-linux-${gcloud_arch}.tar.gz" -o gcloud.tar.gz; \
    tar -xzf gcloud.tar.gz -C /opt; \
    CLOUDSDK_CONFIG=/tmp/gcloud /opt/google-cloud-sdk/install.sh --quiet --usage-reporting=false --path-update=false --bash-completion=false; \
    mkdir -p /cache/infra/terraform/plugins; \
    chmod 1777 /cache/infra /cache/infra/terraform /cache/infra/terraform/plugins; \
    ln -s /opt/google-cloud-sdk/bin/gcloud /usr/local/bin/gcloud; \
    chmod -R a+rX /opt/google-cloud-sdk; \
    rm -rf /tmp/infra /tmp/gcloud
CMD ["bash"]
