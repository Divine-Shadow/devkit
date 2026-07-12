# Post-Retirement Surface Cleanup

Devkit has retired the old runtime stack. The supported operator surface should
now read as Nix/runtime-flake first: public commands, help text, active docs,
tests, and readiness checks should not keep migration-era names that imply
container image orchestration.

## Progress

- [x] (2026-05-14) Started a read-only audit of command help, docs, tests, and
  checks for stale post-retirement terminology.
- [x] (2026-05-14) Renamed the runtime metadata matrix command and updated
  active references.
- [x] (2026-05-14) Updated preflight language and behavior for required Nix/bubblewrap checks
  plus optional brokered Docker availability.
- [x] (2026-05-14) Ran the full verification gate and targeted stale-term
  audits.

## Surprises & Discoveries

- The old image-oriented matrix command still validates `runtime.flake`
  metadata, so the command name no longer matches the behavior.
- `preflight` still failed when Docker was unavailable even though Docker is now
  only required for overlays that intentionally request brokered access.

## Decision Log

- Decision: Rename the public matrix command to `runtime-matrix`.
  Rationale: The active metadata boundary is `runtime.flake`; keeping the old
  name teaches the retired model.
- Decision: Keep brokered Docker as an optional preflight capability.
  Rationale: Docker remains valid behind native broker policy, but it is not a
  universal runtime prerequisite.

## Implementation Plan

1. Rename the Go command package and command registration from image-oriented
   terminology to runtime-oriented terminology.
2. Update active docs, Make targets, and integration tests to call
   `runtime-matrix`.
3. Update preflight checks to require Nix and bubblewrap, warn for missing
   Docker broker upstream, and avoid container-era wording.
4. Run the requested verification commands and record the outcome.

## Acceptance

- `kit/scripts/devkit --dry-run -p dev-all runtime-matrix --all --check` passes
  and is what `make ci-cheap` invokes.
- CLI help presents devkit as the Nix/native runtime CLI, not experimental
  migration tooling.
- Active docs and tests use `runtime-matrix`; historical/archive records may
  still mention prior names when clearly archival.
- `preflight` does not fail solely because Docker is unavailable.
- The full verification gate in the objective passes.

## Evidence

- `nix --extra-experimental-features 'nix-command flakes' develop --command make ci-cheap`
  passed. The gate built `kit/bin/devctl`, ran `go test -count=1 ./...`,
  `nix flake check`, overlay runtime metadata validation, overlay lock policy,
  `kit/scripts/devkit --dry-run -p dev-all runtime-matrix --all --check`, and
  `kit/scripts/retired-runtime-guard`.
- `nix --extra-experimental-features 'nix-command flakes' develop --command make native-overlay-matrix`
  passed with `native-overlay-matrix: ok`.
- `nix --extra-experimental-features 'nix-command flakes' develop --command make native-e2e-lifecycle`
  passed with `native-e2e-lifecycle: ok`.
- `nix --extra-experimental-features 'nix-command flakes' develop --command make native-overlay-e2e-matrix`
  passed with report `/tmp/devkit-native-overlay-e2e.hSAm48/report.tsv`.
  Classifications: `dev-all`, `ouroboros-static-front-end`,
  `ouroboros-terraform`, and `pokeemerald` are `e2e-pass`; `codex`,
  `dumb-onion-hax`, and `ouro-integration` are `runtime-pass`.
- `kit/scripts/retired-runtime-guard` passed with
  `retired-runtime-guard: ok`.
- `find overlays -maxdepth 2 -name flake.lock -print` produced no output.
- `git diff --check` and `bash -n` for the touched shell entrypoints/check
  scripts passed.
- Targeted stale-term audits for `compose-retirement-guard`,
  `compose-retirement-static`, the old matrix command name, stale devctl
  experimental banner text, Docker-host preflight wording, retired binary
  paths, and Compose strings found no supported-surface matches outside the
  retired-runtime guard and historical archive exclusions.
- `kit/scripts/devkit --help` shows `devctl - Nix/native runtime CLI`,
  `runtime-matrix`, and Nix/bubblewrap preflight checks.
- `kit/scripts/devkit preflight` passed, reporting required Nix/bubblewrap
  checks and brokered Docker as a capability.
