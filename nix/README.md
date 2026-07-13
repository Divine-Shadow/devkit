# Devkit Nix Runtime Shells

This directory tracks the Nix-first replacement surface for long-lived devkit
agent runtimes. The root `flake.nix` remains the umbrella entrypoint, while
each overlay owns its shell definition in `overlays/<overlay>/runtime.nix` and
a thin overlay-local flake at `overlays/<overlay>/flake.nix`.

Current shell targets:

- `template-agent`: baseline shell for new overlays.
- `ouroboros-dev-agent`: compatibility alias for the external Ouroboros Codex
  agent image family.
- `dev-all`: canonical multi-worktree Ouroboros shell.
- `codex`: single-checkout Ouroboros shell.
- `dumb-onion-hax`: Scala/SBT shell for the dumb-onion-hax repo.
- `ouroboros-terraform`: Ouroboros agent plus Terraform and Packer.
- `ouro-integration`: integration shell with Terraform/Packer plus the
  Ouroboros agent toolchain.
- `pokeemerald`: Ouroboros agent plus ARM embedded toolchain.
- `ouroboros-static-front-end`: Node/static frontend shell.
- `runtime-test-agent`: lightweight integration-test shell.
- `tinyproxy`: host-service shell for proxy experiments.

Source-derived operator apps:

- `management-inspection`: explicitly snapshots one Git commit into the Nix
  store and exposes it through a read-only Management inspection profile with
  separate revision-specific writable state. See
  `kit/docs/management-readonly-inspection-profile.md`.

Use explicit experimental feature flags on hosts that do not enable flakes by
default:

```bash
nix --extra-experimental-features 'nix-command flakes' flake check
nix --extra-experimental-features 'nix-command flakes' develop ./overlays/dev-all#default --output-lock-file /dev/null --command bash -lc 'git --version && sbt --version'
```

Overlay smoke commands:

```bash
nix --extra-experimental-features 'nix-command flakes' develop ./overlays/_template#default --output-lock-file /dev/null --command bash -lc 'codex --version && git --version && uv --version && python3 --version'
nix --extra-experimental-features 'nix-command flakes' develop ./overlays/codex#default --output-lock-file /dev/null --command bash -lc 'codex --version && command -v sbt java go spago netlify deno playwright >/dev/null'
nix --extra-experimental-features 'nix-command flakes' develop ./overlays/dev-all#default --output-lock-file /dev/null --command bash -lc 'codex --version && spago --version && netlify --version && deno --version && playwright --version && go version && mgba-headless --help 2>&1 | grep -q -- --script'
nix --extra-experimental-features 'nix-command flakes' develop ./overlays/dumb-onion-hax#default --output-lock-file /dev/null --command bash -lc 'codex --version && command -v sbt java aws python3 >/dev/null'
nix --extra-experimental-features 'nix-command flakes' develop ./overlays/ouro-integration#default --output-lock-file /dev/null --command bash -lc 'codex --version && terraform version | head -1 && packer version && aws --version && command -v sbt java >/dev/null'
nix --extra-experimental-features 'nix-command flakes' develop ./overlays/ouroboros-static-front-end#default --output-lock-file /dev/null --command bash -lc 'codex --version && node --version && npm --version && spago --version && netlify --version && deno --version && playwright --version'
nix --extra-experimental-features 'nix-command flakes' develop ./overlays/ouroboros-terraform#default --output-lock-file /dev/null --command bash -lc 'codex --version && terraform version | head -1 && packer version && aws --version'
nix --extra-experimental-features 'nix-command flakes' develop ./overlays/pokeemerald#default --output-lock-file /dev/null --command bash -lc 'codex --version && arm-none-eabi-gcc --version | head -1 && arm-none-eabi-as --version | head -1 && mgba-headless --help 2>&1 | grep -q -- --script'
```

Run them together with:

```bash
kit/scripts/overlay-runtime-smoke
kit/scripts/native-overlay-matrix
```

Every shell conversion must pass the local smoke and lifecycle matrix gates.
`nix flake check` also runs `nix/validate-overlay-runtimes.py`, which requires
each overlay `devkit.yaml` to declare an accepted `runtime.flake` and have
matching `overlays/<overlay>/runtime.nix` and `overlays/<overlay>/flake.nix`
files.

Overlay-local flakes intentionally stay lockless. The root `flake.lock` is the
single pin source; direct overlay checks should use `--output-lock-file
/dev/null`, and generated `overlays/*/flake.lock` files should not be
committed. See
`kit/docs/native-operator-runbook.md` for the operator policy and rationale.

Historical parity notes are archived under `documentation/archive/compose-retirement/`.
