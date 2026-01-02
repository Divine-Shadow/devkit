#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEVKIT_BIN="$ROOT_DIR/kit/bin/devctl"
DEVKIT_WRAPPER="$ROOT_DIR/kit/scripts/devkit"

make -C "$ROOT_DIR/cli/devctl" build

if [[ ! -x "$DEVKIT_BIN" ]]; then
  echo "devctl binary not found at $DEVKIT_BIN." >&2
  echo "Build it with: make -C \"$ROOT_DIR/cli/devctl\" build" >&2
  exit 1
fi

if [[ -z "${DEVKIT_WORKTREE_ROOT:-}" ]]; then
  DEVKIT_WORKTREE_ROOT="$HOME/devkit-worktrees"
  export DEVKIT_WORKTREE_ROOT
fi

if [[ ! -d "$DEVKIT_WORKTREE_ROOT" ]]; then
  mkdir -p "$DEVKIT_WORKTREE_ROOT"
fi

if ! chmod u+rwx "$DEVKIT_WORKTREE_ROOT"; then
  echo "Unable to set user permissions on DEVKIT_WORKTREE_ROOT: $DEVKIT_WORKTREE_ROOT" >&2
  echo "Ensure you own this path or pick another with DEVKIT_WORKTREE_ROOT." >&2
  exit 1
fi

if [[ ! -w "$DEVKIT_WORKTREE_ROOT" ]]; then
  echo "DEVKIT_WORKTREE_ROOT is not writable: $DEVKIT_WORKTREE_ROOT" >&2
  echo "Set DEVKIT_WORKTREE_ROOT to a writable absolute path and retry." >&2
  exit 1
fi

if [[ -z "${DEVKIT_SKIP_PREFLIGHT:-}" ]]; then
  if [[ -n "${DEVKIT_RESET:-}" ]]; then
    "$DEVKIT_WRAPPER" -p codex --profile dns down
    docker network rm devkit_dev-internal devkit_dev-egress >/dev/null 2>&1 || true
  fi
  if docker ps \
    --filter label=com.docker.compose.project=devkit \
    --filter label=com.docker.compose.service=dev-agent \
    --format '{{.Names}}' | rg -q . \
    && docker ps \
      --filter label=com.docker.compose.project=devkit \
      --filter label=com.docker.compose.service=tinyproxy \
      --format '{{.Names}}' | rg -q .; then
    "$DEVKIT_WRAPPER" -p codex --profile dns check-net
    if ! "$DEVKIT_WRAPPER" -p codex --profile dns exec 1 bash -lc \
      "curl -fsS -I -x http://tinyproxy:8888 https://repo1.maven.org | head -n 1"; then
      echo "Preflight failed: tinyproxy egress check failed." >&2
      exit 1
    fi
    echo "Preflight succeeded."
  else
    echo "Preflight skipped (dev-agent or tinyproxy not running)."
  fi
fi

if docker ps \
  --filter label=com.docker.compose.project=devkit \
  --filter label=com.docker.compose.service=dev-agent \
  --format '{{.Names}}' | rg -q .; then
  if command -v tmux >/dev/null 2>&1 && tmux has-session -t devkit-shells >/dev/null 2>&1; then
    tmux attach -t devkit-shells
  else
    echo "Devkit already running; skipping boot."
  fi
else
  "$DEVKIT_WRAPPER" -p codex --profile dns tmux-shells 3
fi
