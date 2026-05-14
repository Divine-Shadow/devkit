SHELL := /bin/bash

# Paths
KIT        := kit
OVERLAYS   := overlays
PROJECT    ?= codex
CLI        := $(KIT)/bin/devctl
# Defaults for native run/health flows
REPO       ?= ouroboros-ide
N          ?= 4

NIX       ?= nix --extra-experimental-features 'nix-command flakes'

.PHONY: build-cli health run ci-cheap native-e2e-lifecycle native-runtime-smoke native-readiness-audit native-overlay-matrix overlay-runtime-smoke compose-retirement-guard postgres-broker-container-smoke

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

ci-cheap: build-cli
	@echo "== Go tests =="
	@cd cli/devctl && go test -count=1 ./...
	@echo "== Nix flake check =="
	@$(NIX) flake check
	@echo "== Overlay runtime metadata =="
	@$(NIX) develop --command nix/validate-overlay-runtimes.py overlays >/tmp/devkit-overlay-runtimes.json
	@echo "== Overlay lock policy =="
	@! find overlays -maxdepth 2 -name flake.lock -print | grep -q .
	@echo "== Image matrix =="
	@kit/scripts/devkit --dry-run -p dev-all image-matrix --all --check
	@echo "== Compose retirement guard =="
	@kit/scripts/compose-retirement-guard

native-runtime-smoke: build-cli
	@echo "== Native runtime smoke (dev-all) =="
	@kit/scripts/native-runtime-smoke

native-e2e-lifecycle: build-cli
	@echo "== Native end-to-end lifecycle =="
	@kit/scripts/native-e2e-lifecycle

native-readiness-audit: build-cli
	@echo "== Native readiness audit (two-agent dev-all) =="
	@kit/scripts/native-readiness-audit

native-overlay-matrix: build-cli
	@echo "== Native overlay lifecycle matrix =="
	@kit/scripts/native-overlay-matrix

overlay-runtime-smoke:
	@echo "== Overlay runtime smoke (Nix flakes) =="
	@kit/scripts/overlay-runtime-smoke

compose-retirement-guard:
	@echo "== Compose retirement static guard =="
	@kit/scripts/compose-retirement-guard

postgres-broker-container-smoke:
	@echo "== Postgres broker container smoke =="
	@kit/scripts/postgres-broker-container-smoke
