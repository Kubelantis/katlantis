FROM golang:1.27.1@sha256:f44f6e88636cfb311f9ebace870ded69d943f227bb3cb27d32ffd84ea18c43ea

RUN apt-get update && apt-get --no-install-recommends -y install unzip \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# Install Terraform
# renovate: datasource=github-releases depName=hashicorp/terraform versioning=hashicorp
ENV TERRAFORM_VERSION=1.16.3
RUN case "$(uname -m)" in x86_64|amd64) ARCH="amd64" ;; aarch64|arm64|armv7l) ARCH="arm64" ;; *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;; esac && \
    TERRAFORM_ARCHIVE="terraform_${TERRAFORM_VERSION}_linux_${ARCH}.zip" && \
    wget -nv "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/${TERRAFORM_ARCHIVE}" && \
    wget -nv -O terraform_SHA256SUMS "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_SHA256SUMS" && \
    grep " ${TERRAFORM_ARCHIVE}$" terraform_SHA256SUMS | sha256sum -c - && \
    mkdir -p /usr/local/bin/tf/versions/${TERRAFORM_VERSION} && \
    unzip "${TERRAFORM_ARCHIVE}" -d /usr/local/bin/tf/versions/${TERRAFORM_VERSION} && \
    ln -s /usr/local/bin/tf/versions/${TERRAFORM_VERSION}/terraform /usr/local/bin/terraform && \
    rm "${TERRAFORM_ARCHIVE}" terraform_SHA256SUMS

# Install conftest
# renovate: datasource=github-releases depName=open-policy-agent/conftest
ENV CONFTEST_VERSION=0.69.0
RUN case $(uname -m) in x86_64|amd64) ARCH="x86_64" ;; aarch64|arm64|armv7l) ARCH="arm64" ;; esac && \
    curl -LOs https://github.com/open-policy-agent/conftest/releases/download/v${CONFTEST_VERSION}/conftest_${CONFTEST_VERSION}_Linux_${ARCH}.tar.gz && \
    curl -LOs https://github.com/open-policy-agent/conftest/releases/download/v${CONFTEST_VERSION}/checksums.txt && \
    sed -n "/conftest_${CONFTEST_VERSION}_Linux_${ARCH}.tar.gz/p" checksums.txt | sha256sum -c && \
    mkdir -p /usr/local/bin/cft/versions/${CONFTEST_VERSION} && \
    tar -C  /usr/local/bin/cft/versions/${CONFTEST_VERSION} -xzf conftest_${CONFTEST_VERSION}_Linux_${ARCH}.tar.gz && \
    # Generally Atlantis requires `conftest$version` command. But we use `conftest` command in test.
    # `conftest$version` command blocks upgrading conftest operation cause e2e test use this image.
    ln -s /usr/local/bin/cft/versions/${CONFTEST_VERSION}/conftest /usr/local/bin/conftest && \
    rm conftest_${CONFTEST_VERSION}_Linux_${ARCH}.tar.gz && \
    rm checksums.txt

# Install terragrunt
# renovate: datasource=github-releases depName=gruntwork-io/terragrunt
ENV TERRAGRUNT_VERSION=1.1.6
RUN case "$(uname -m)" in x86_64|amd64) ARCH="amd64" ;; aarch64|arm64) ARCH="arm64" ;; *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;; esac && \
    curl -LOs "https://github.com/gruntwork-io/terragrunt/releases/download/v${TERRAGRUNT_VERSION}/terragrunt_linux_${ARCH}" && \
    curl -LOs "https://github.com/gruntwork-io/terragrunt/releases/download/v${TERRAGRUNT_VERSION}/SHA256SUMS" && \
    grep " terragrunt_linux_${ARCH}$" SHA256SUMS | sha256sum -c && \
    install -m 0755 "terragrunt_linux_${ARCH}" /usr/local/bin/terragrunt && \
    rm "terragrunt_linux_${ARCH}" SHA256SUMS

RUN useradd -u 1001 -m atlantis

USER atlantis
