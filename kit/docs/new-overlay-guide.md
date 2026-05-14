# New Overlay Guide

Create overlays as Nix-first runtime definitions.

## Files

```text
overlays/<name>/
  devkit.yaml
  flake.nix
  runtime.nix
  README.md
```

`devkit.yaml` should declare the default repo, agent count, readiness mode, and runtime flake:

```yaml
defaults:
  repo: your-repo
  agents: 1
runtime:
  flake: ./overlays/your-overlay#default
  codex_version: 0.130.0
  core_check: make test
readiness:
  default_mode: runtime-only
native:
  worktree_root: ../agent-worktrees
  state_root: ../.devkit/native-agents
  worktree_container_root: /worktrees
  state_container_root: /agent-state
```

`runtime.nix` defines the packages and shell hooks for the overlay. `flake.nix` should expose the overlay-local shell by delegating to the root flake, matching the existing overlays.

## Validation

From the repo root:

```bash
nix flake check
kit/scripts/devkit runtime-matrix --all --check
nix --extra-experimental-features 'nix-command flakes' develop ./overlays/<name> --output-lock-file /dev/null --command true
kit/scripts/devkit --dry-run -p <name> native plan --repo <repo>
kit/scripts/devkit --dry-run -p <name> ensure-ready --repo <repo> --runtime-only
```

Use `kit/scripts/devkit -p <name> up/status/exec/down` for operator flows after the dry-run plan is correct.
