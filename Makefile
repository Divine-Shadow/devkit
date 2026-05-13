SHELL := /bin/bash

# Paths
KIT        := kit
OVERLAYS   := overlays
PROJECT    ?= codex
CLI        := $(KIT)/bin/devctl
# Defaults for native run/health flows
REPO       ?= ouroboros-ide
N          ?= 4

.PHONY: build-cli health run native-runtime-smoke native-readiness-audit overlay-runtime-smoke compose-retirement-guard

build-cli:
	@echo "== Building Go CLI -> $(CLI) =="
	@$(MAKE) -C cli/devctl build

# Unified health check: verifies ssh + codex + worktrees for both overlays
health: build-cli
	@echo "== Health: codex overlay =="
	@$(CLI) -p codex verify
	@echo "== Health: dev-all overlay =="
	@$(CLI) -p dev-all verify

# Idempotent run: ensure worktrees and bring up N agents with tmux windows
run: build-cli
	@echo "== Run: $(REPO) with N=$(N) agents (dev-all overlay) =="
	@$(CLI) -p dev-all run $(REPO) $(N)

native-runtime-smoke: build-cli
	@echo "== Native runtime smoke (dev-all) =="
	@kit/scripts/native-runtime-smoke

native-readiness-audit: build-cli
	@echo "== Native readiness audit (two-agent dev-all) =="
	@kit/scripts/native-readiness-audit

overlay-runtime-smoke:
	@echo "== Overlay runtime smoke (Nix flakes) =="
	@kit/scripts/overlay-runtime-smoke

compose-retirement-guard:
	@echo "== Compose retirement static guard =="
	@kit/scripts/compose-retirement-guard
