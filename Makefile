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

.PHONY: build-cli health run ci-cheap ci-cheap-run native-reset-custody-gate devctl-overlay-runtime-authority native-e2e-lifecycle native-overlay-e2e-matrix native-runtime-smoke native-readiness-degraded-guard native-codex-home-preservation-guard native-overlay-matrix overlay-runtime-smoke retired-runtime-guard nix-overlay-runtime-guard postgres-broker-container-smoke

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

# The full source suite retains integration/config coverage. Only dependency
# acquisition is supplied by the same immutable recipe as production Devctl.
ci-cheap:
	@python3 -B nix/test_devctl_ci_dependencies.py
	@set -eu; supplier="$$($(NIX) build --no-link --print-out-paths .#devctl-ci-dependencies)"; \
	  python3 nix/devctl-ci-dependencies.py "$$supplier" -- $(MAKE) ci-cheap-run

ci-cheap-run: build-cli
	@echo "== Go tests =="
	@$(MAKE) -C cli/devctl test
	@$(MAKE) native-reset-custody-gate
	@echo "== Nix flake check =="
	@$(NIX) flake check
	@$(MAKE) devctl-overlay-runtime-authority
	@echo "== Overlay runtime metadata =="
	@$(NIX) develop --command nix/validate-overlay-runtimes.py overlays >/tmp/devkit-overlay-runtimes.json
	@echo "== Overlay lock policy =="
	@! find overlays -maxdepth 2 -name flake.lock -print | grep -q .
	@echo "== Runtime matrix =="
	@kit/scripts/devkit --dry-run -p dev-all runtime-matrix --all --check
	@echo "== Retired runtime guard =="
	@kit/scripts/retired-runtime-guard
	@echo "== Overlay Nix runtime guard =="
	@kit/scripts/nix-overlay-runtime-guard

native-reset-custody-gate:
	@set -eu; system="$$($(NIX) eval --impure --raw --expr builtins.currentSystem)"; \
	  fixture="$$($(NIX) build --no-link --print-out-paths --output-lock-file /dev/null ".#checks.$$system.native-cli-fixed-owner-consequences")"; \
	  cat "$$fixture/native-cli-consequences.log"; \
	  grep -F -- '--- PASS: TestDevAllResetReconstructsThreeSlotsThroughPackageSSHAuthority' "$$fixture/native-cli-consequences.log" >/dev/null

devctl-overlay-runtime-authority:
	@echo "== Immutable devctl overlay runtime authority =="
	@set -eu; tools="$$($(NIX) build --no-link --print-out-paths .#devctl-test-tools)"; \
	  PATH="$$tools/bin:$$PATH" python3 -B nix/test_devctl_overlay_runtime_authority.py; \
	  PATH="$$tools/bin:$$PATH" kit/scripts/devctl-overlay-runtime-authority -- $(NIX)

native-runtime-smoke: build-cli
	@echo "== Native runtime smoke (dev-all) =="
	@kit/scripts/native-runtime-smoke

native-e2e-lifecycle: build-cli
	@echo "== Native end-to-end lifecycle =="
	@kit/scripts/native-e2e-lifecycle

native-overlay-e2e-matrix: build-cli
	@echo "== Native overlay end-to-end lifecycle matrix =="
	@kit/scripts/native-overlay-e2e-matrix

native-readiness-degraded-guard: build-cli
	@echo "== Native degraded readiness guard =="
	@kit/scripts/native-readiness-degraded-guard

native-codex-home-preservation-guard: build-cli
	@echo "== Native Codex home preservation guard =="
	@kit/scripts/native-codex-home-preservation-guard

native-overlay-matrix: build-cli
	@echo "== Native overlay lifecycle matrix =="
	@kit/scripts/native-overlay-matrix

overlay-runtime-smoke:
	@echo "== Overlay runtime smoke (Nix flakes) =="
	@kit/scripts/overlay-runtime-smoke

retired-runtime-guard:
	@echo "== Retired runtime static guard =="
	@kit/scripts/retired-runtime-guard

nix-overlay-runtime-guard:
	@echo "== Overlay Nix runtime static guard =="
	@kit/scripts/nix-overlay-runtime-guard

postgres-broker-container-smoke:
	@echo "== Postgres broker container smoke =="
	@kit/scripts/postgres-broker-container-smoke
