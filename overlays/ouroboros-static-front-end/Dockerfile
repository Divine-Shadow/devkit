FROM node:20-bullseye

ARG INSTALL_CODEX=true
ARG CODEX_VERSION="rust-v0.130.0"
ARG CODEX_ASSET="codex-x86_64-unknown-linux-musl.tar.gz"
ARG CODEX_SHA256=""
ARG SPAGO_VERSION="0.93.45"
ARG NETLIFY_CLI_VERSION="26.0.1"

ENV DEBIAN_FRONTEND=noninteractive
SHELL ["/bin/bash","-lc"]

# Base tools needed for git/ssh and fetching binaries, plus Python 3
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      git openssh-client ca-certificates netcat-openbsd \
      curl jq ripgrep tmux vim bubblewrap \
      python3 python3-venv python3-pip \
 && rm -rf /var/lib/apt/lists/*

# Ensure npm is up to date before global installs
RUN npm install -g npm@latest

# Install Spago (PureScript build tool)
RUN npm install -g "spago@${SPAGO_VERSION}" \
 && spago --version

# Install Netlify CLI for static front-end deploy workflows.
RUN npm install -g "netlify-cli@${NETLIFY_CLI_VERSION}" \
 && netlify --version

# Install uv (provides `uv` and `uvx`) to /usr/local/bin
RUN curl -fsSL https://astral.sh/uv/install.sh -o /tmp/install-uv.sh \
 && env UV_INSTALL_DIR=/usr/local/bin sh /tmp/install-uv.sh \
 && rm -f /tmp/install-uv.sh \
 && /usr/local/bin/uv --version && /usr/local/bin/uvx --version

# Install Codex CLI (pinned binary) as root into /usr/local/bin
RUN if [ "$INSTALL_CODEX" = "true" ]; then \
      set -euo pipefail; \
      URL="https://github.com/openai/codex/releases/download/${CODEX_VERSION}/${CODEX_ASSET}"; \
      echo "Fetching Codex CLI from: ${URL}"; \
      curl -fsSL -o /tmp/codex.tgz "$URL"; \
      if [ -n "$CODEX_SHA256" ]; then echo "$CODEX_SHA256  /tmp/codex.tgz" | sha256sum -c -; fi; \
      tar -xzf /tmp/codex.tgz -C /usr/local/bin; \
      COD_BIN=$(ls /usr/local/bin/codex* | head -n1); \
      mv "$COD_BIN" /usr/local/bin/codex; \
      chmod +x /usr/local/bin/codex; \
      rm -f /tmp/codex.tgz; \
      /usr/local/bin/codex --version || true; \
    fi

# Add codex wrapper that enforces credentials presence (no best-effort)
RUN mkdir -p /usr/local/lib/codex \
  && if [ -x /usr/local/bin/codex ]; then mv /usr/local/bin/codex /usr/local/lib/codex/codex-bin; fi \
  && printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'DEV_HOME="${HOME:-/workspace/.devhome}"' \
  'if [[ -z "$DEV_HOME" || "$DEV_HOME" == "/" || "$DEV_HOME" == "/home/node" ]]; then DEV_HOME="/workspace/.devhome"; fi' \
  'mkdir -p "$DEV_HOME/.codex/rollouts" "$DEV_HOME/.cache" "$DEV_HOME/.config" "$DEV_HOME/.local"' \
  'if [[ ! -r "$DEV_HOME/.codex/auth.json" ]]; then' \
  '  echo "ERROR: Missing Codex credentials at $DEV_HOME/.codex/auth.json. Mount HOST_CODEX_DIR (~/.codex) or HOST_CODEX_FILE and restart." >&2' \
  '  exit 2' \
  'fi' \
  'chmod 600 "$DEV_HOME/.codex/auth.json" || true' \
  'export HOME="$DEV_HOME"' \
  'export CODEX_ROLLOUT_DIR="$DEV_HOME/.codex/rollouts"' \
  'exec /usr/local/lib/codex/codex-bin "$@"' \
  > /usr/local/bin/codex \
  && chmod +x /usr/local/bin/codex \
  && ln -sf /usr/local/bin/codex /usr/local/bin/codexw \
  && chmod +x /usr/local/bin/codexw \
  && echo 'alias codex=codexw' >> /etc/bash.bashrc

# Strict entrypoint: ensure shared Codex volume is seeded, set HOME, then exec command
RUN printf '%s\n' \
  '#!/usr/bin/env bash' \
  'set -euo pipefail' \
  'DEV_HOME="${HOME:-/workspace/.devhome}"' \
  'if [[ -z "$DEV_HOME" || "$DEV_HOME" == "/" || "$DEV_HOME" == "/home/node" ]]; then DEV_HOME="/workspace/.devhome"; fi' \
  'AUTH_ROOT="/var/host-codex"' \
  'mkdir -p "$DEV_HOME"' \
  'mkdir -p "$AUTH_ROOT" >/dev/null 2>&1 || true' \
  'if [[ ! -w "$AUTH_ROOT" ]]; then' \
  '  echo "WARN: /var/host-codex not writable; using $DEV_HOME/.codex instead" >&2' \
  '  AUTH_ROOT="$DEV_HOME/.codex"' \
  'fi' \
  'mkdir -p "$AUTH_ROOT"' \
  'if [[ ! -r "$AUTH_ROOT/auth.json" ]]; then' \
  '  if [[ -r /var/seed-codex/auth.json ]]; then cp -f /var/seed-codex/auth.json "$AUTH_ROOT/auth.json"; elif [[ -r "/var/auth.json" ]]; then cp -f "/var/auth.json" "$AUTH_ROOT/auth.json"; fi' \
  'fi' \
  'mkdir -p "$AUTH_ROOT/rollouts"' \
  'if [[ ! -r "$AUTH_ROOT/auth.json" ]]; then' \
  '  echo "ERROR: Codex credentials not found. Mount HOST_CODEX_DIR (~/.codex) or HOST_CODEX_FILE and retry." >&2' \
  '  exit 2' \
  'fi' \
  'chmod 600 "$AUTH_ROOT/auth.json" || true' \
  'ln -sfn "$AUTH_ROOT" "$DEV_HOME/.codex"' \
  'mkdir -p "$DEV_HOME/.cache" "$DEV_HOME/.config" "$DEV_HOME/.local"' \
  'export HOME="$DEV_HOME" CODEX_HOME="$AUTH_ROOT" CODEX_ROLLOUT_DIR="$AUTH_ROOT/rollouts" XDG_CACHE_HOME="$DEV_HOME/.cache" XDG_CONFIG_HOME="$DEV_HOME/.config"' \
  'exec "$@"' \
  > /usr/local/bin/frontend-entrypoint.sh \
  && chmod +x /usr/local/bin/frontend-entrypoint.sh

# Pre-create shared auth mount point with node ownership so runtime volume stays writable.
RUN mkdir -p /var/host-codex \
 && chown -R node:node /var/host-codex

# Create caches and workspace for the node user (uid 1000)
RUN mkdir -p /home/node/.npm /home/node/.cache/yarn /workspace \
 && chown -R node:node /home/node /workspace

WORKDIR /workspace
USER 1000:1000
ENTRYPOINT ["/usr/local/bin/frontend-entrypoint.sh"]
