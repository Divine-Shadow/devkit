FROM local/dev-agent:ouroboros-ide

SHELL ["/bin/bash", "-lc"]
USER root

RUN set -euo pipefail; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      binutils-arm-none-eabi \
      gcc-arm-none-eabi \
      libnewlib-arm-none-eabi; \
    arm-none-eabi-as --version | head -1; \
    arm-none-eabi-gcc --version | head -1; \
    rm -rf /var/lib/apt/lists/*

USER dev
WORKDIR /workspace
