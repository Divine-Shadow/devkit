FROM local/dev-agent:ouroboros-ide

ARG TERRAFORM_VERSION=1.9.8
ARG PACKER_VERSION=1.11.2

SHELL ["/bin/bash", "-lc"]
USER root

RUN set -euo pipefail; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl unzip; \
    arch="$(uname -m)"; \
    case "$arch" in \
      x86_64|amd64) zip_arch=amd64 ;; \
      aarch64|arm64) zip_arch=arm64 ;; \
      *) echo "unsupported arch for HashiCorp tools: $arch" >&2; exit 1 ;; \
    esac; \
    install_hashicorp_tool() { \
      local tool="$1"; \
      local version="$2"; \
      local url="https://releases.hashicorp.com/${tool}/${version}/${tool}_${version}_linux_${zip_arch}.zip"; \
      curl -fsSL "$url" -o "/tmp/${tool}.zip"; \
      unzip -qo "/tmp/${tool}.zip" "${tool}" -d /usr/local/bin; \
      chmod 0755 "/usr/local/bin/${tool}"; \
      rm -f "/tmp/${tool}.zip"; \
    }; \
    install_hashicorp_tool terraform "${TERRAFORM_VERSION}"; \
    install_hashicorp_tool packer "${PACKER_VERSION}"; \
    terraform version; \
    packer version; \
    rm -rf /var/lib/apt/lists/*

USER dev
WORKDIR /workspace
