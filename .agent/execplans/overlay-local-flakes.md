# Add Overlay-Local Flakes While Preserving Root Compatibility

This ExecPlan is a living document. It is maintained according to `.agent/PLANS.md`.

## Purpose / Big Picture

Devkit currently has one root `flake.nix` that defines every native runtime shell and imports each overlay's `runtime.nix`. After this change, every flake-backed overlay will also have its own `flake.nix`, so an operator can inspect or enter an overlay runtime from the overlay directory while existing root-flake references such as `.#dev-all` keep working. The supported command path remains `kit/scripts/devkit`, which execs `kit/bin/devctl`.

## Progress

- [x] (2026-05-13T02:30:52Z) Inspected the current root flake, overlay `runtime.nix` files, and confirmed the repo did not yet contain `.agent/PLANS.md`.
- [x] (2026-05-13T02:30:52Z) Created the local ExecPlan convention file and this living plan.
- [x] (2026-05-13T02:38:00Z) Added overlay-local `flake.nix` files for all flake-backed overlays.
- [x] (2026-05-13T02:38:00Z) Added metadata validation so overlay flakes are checked structurally by root checks and `image-matrix --check`.
- [x] (2026-05-13T02:38:00Z) Updated docs to describe root-compatible and overlay-local flake entrypoints.
- [x] (2026-05-13T02:49:00Z) Ran the verification gate and recorded evidence.
- [x] (2026-05-13T02:55:00Z) Committed the overlay-local flake scaffold as `76a84fe`.
- [x] (2026-05-13T03:31:41Z) Proved overlay-local `runtime.flake` support with `_template` as canary while production overlays keep root refs.

## Surprises & Discoveries

- Observation: This repo did not contain `.agent/PLANS.md`; the closest available guidance was in `../ouroboros-ide/.agent/PLANS.md`.
  Evidence: `find . -maxdepth 3 -path '*/.agent/PLANS.md' -print` returned no local file.

- Observation: Nix ignores new files in a Git-backed flake until they are tracked or staged.
  Evidence: `nix flake metadata ./overlays/dev-all --output-lock-file /dev/null` first failed with `Path 'overlays/dev-all/flake.nix' ... is not tracked by Git`; staging the file allowed metadata and `nix develop` to succeed.

- Observation: The existing CLI can keep using root refs while overlay-local flakes are introduced.
  Evidence: A subagent inspected native flake resolution and found `runtime.flake` is treated as an opaque string by native planning, while `image-matrix` and `nix/validate-overlay-runtimes.py` intentionally validate root-style refs.

- Observation: `nix develop ./overlays/_template#default` is valid from the repo root, but writing locks must be disabled for read-only verification.
  Evidence: A subagent syntax probe succeeded and created `overlays/_template/flake.lock`; the file was removed and subsequent checks use `--output-lock-file /dev/null`.

## Decision Log

- Decision: Preserve root `runtime.flake` values such as `.#dev-all` during this slice and add overlay-local flakes as an additive entrypoint.
  Rationale: The objective explicitly requires preserving current root-flake refs until the transition is proven, and existing devkit commands already consume those refs.
  Date/Author: 2026-05-13 / Codex

- Decision: Use overlay-local flakes that delegate to the root flake outputs instead of duplicating package pinning in every overlay.
  Rationale: The root flake already owns pinned tools, cross-system details, and shell composition. Delegation gives each overlay an independent flake boundary without creating eight copies of the runtime dependency graph.
  Date/Author: 2026-05-13 / Codex

- Decision: Do not commit per-overlay `flake.lock` files.
  Rationale: The overlay-local flakes delegate to the root flake, so committing eight child lock files would duplicate and drift from the root lock graph. Verification uses `--output-lock-file /dev/null` for overlay-local smoke commands.
  Date/Author: 2026-05-13 / Codex

- Decision: Use `_template` as the canary for overlay-local `runtime.flake`.
  Rationale: `_template` is non-production scaffolding, already has an overlay-local flake, and is included in `--all` validation without risking active `dev-all` agents.
  Date/Author: 2026-05-13 / Codex

## Outcomes & Retrospective

The first implementation added thin overlay-local flakes for all flake-backed overlays while leaving `runtime.flake` values rooted at `.#...` for compatibility. Root flake checks, overlay runtime smoke, image-matrix validation, CLI dry-runs, and overlay-local `nix develop` checks all passed. No per-overlay lock files were generated or committed. The next canary phase makes `_template` use an overlay-local `runtime.flake` while keeping production overlays on root refs.

The canary phase changed `_template` to `runtime.flake: "./overlays/_template#default"`, kept `dev-all` and production overlays on root refs, taught validation to accept only that canary overlay-local ref, and updated native Nix invocations that may touch overlay flakes to pass `--output-lock-file /dev/null`.

## Context and Orientation

The repository root is `/home/bayesartre/dev/devkit`. The root `flake.nix` defines shared pinned tools and exposes `devShells.<system>.<overlay>` names. Each flake-backed overlay currently has `overlays/<name>/runtime.nix`, which expects shared arguments from the root flake and returns a shell. Overlay metadata lives in `overlays/<name>/devkit.yaml`; its `runtime.flake` field currently names root-flake refs such as `.#dev-all` or `.#pokeemerald`. The CLI wrapper `kit/scripts/devkit` must remain the supported executable path.

The flake-backed overlays in this slice are `_template`, `codex`, `dev-all`, `dumb-onion-hax`, `ouro-integration`, `ouroboros-static-front-end`, `ouroboros-terraform`, and `pokeemerald`.

## Plan of Work

First, add an overlay-local `flake.nix` to each flake-backed overlay. Each file should expose `devShells.<system>.default`, `devShells.<system>.<overlay>`, and `packages.<system>.default` if needed only when the root flake already has a package to delegate. The primary requirement is that `nix develop ./overlays/<name>` enters the same shell as `nix develop .#<name>` from the root.

Second, extend the existing overlay runtime metadata validation so it verifies that every flake-backed overlay has an overlay-local `flake.nix`. This keeps `nix flake check` meaningful for the new structure without changing CLI behavior.

Third, update documentation to show both root-compatible refs and overlay-local entrypoints, and explicitly state that `runtime.flake` remains root-compatible in this slice.

For the canary phase, teach validation and smoke tooling to accept both root refs and overlay-local refs for the matching overlay, then change only `_template` to an overlay-local `runtime.flake`. Run the verification gate from the repository root and record the observed evidence.

## Concrete Steps

Run all commands from `/home/bayesartre/dev/devkit` unless a command says otherwise.

1. Inspect and edit files using `rg`, `sed`, and `apply_patch`.
2. Run formatting if any Go files change.
3. Run `nix flake check`.
4. Run `make overlay-runtime-smoke`.
5. Run `kit/scripts/devkit image-matrix --all --check`.
6. Run representative root-flake dry-runs for every flake-backed overlay:
   `kit/scripts/devkit --dry-run -p <overlay> native plan --repo <repo> --flake .#<overlay-or-alias>`.
7. Run representative overlay-local Nix smoke commands:
   `nix --extra-experimental-features 'nix-command flakes' develop ./overlays/<overlay> --output-lock-file /dev/null --command true`.
8. For the canary phase, run `nix --extra-experimental-features 'nix-command flakes' develop ./overlays/_template --output-lock-file /dev/null --command true` and wrapper dry-runs for `_template`.

## Validation and Acceptance

Acceptance requires all flake-backed overlays to contain `flake.nix`, root `nix flake check` to pass, the image matrix to remain OK, the runtime smoke target to pass, and representative dry-runs to prove the CLI still uses root-compatible refs. For the canary phase, `_template` must use an overlay-local `runtime.flake`, validation must accept it, `dev-all` must still use a root ref, and no overlay lock files should be created. If CLI resolution code changes, run `go test -count=1 ./...`.

## Idempotence and Recovery

The changes are additive. Re-running Nix checks and dry-runs should not mutate tracked files. If an overlay-local flake creates a lock file during manual testing, remove the generated `overlays/<name>/flake.lock` unless the design explicitly changes to commit overlay locks.

## Artifacts and Notes

Verification transcripts will be recorded here as they are produced.

Trial evidence:

    nix --extra-experimental-features 'nix-command flakes' flake metadata ./overlays/dev-all --output-lock-file /dev/null
    nix --extra-experimental-features 'nix-command flakes' develop ./overlays/dev-all --output-lock-file /dev/null --command true

Both commands exited 0 after `overlays/dev-all/flake.nix` was staged. No `overlays/dev-all/flake.lock` file was created.

Gate evidence:

    nix --extra-experimental-features 'nix-command flakes' develop --command sh -c 'cd cli/devctl && go test -count=1 ./...'
    Result: exited 0; all Go packages passed.

    nix --extra-experimental-features 'nix-command flakes' develop --command make -C cli/devctl build
    Result: exited 0; `kit/bin/devctl` was rebuilt through the canonical build target.

    nix --extra-experimental-features 'nix-command flakes' flake check
    Result: exited 0; root `overlay-runtime-metadata` check evaluated with overlay-local flake validation.

    nix --extra-experimental-features 'nix-command flakes' develop --command make overlay-runtime-smoke
    Result: exited 0 with `overlay-runtime-smoke: ok`.

    kit/scripts/devkit image-matrix --all --check
    Result: exited 0 with `image-matrix: OK`.

    kit/scripts/devkit --dry-run -p <overlay> native plan --repo <repo> --flake <root-ref>
    kit/scripts/devkit --dry-run -p <overlay> ensure-ready --repo <repo> --runtime-only
    Result: exited 0 for `_template`, `codex`, `dev-all`, `dumb-onion-hax`, `ouro-integration`, `ouroboros-static-front-end`, `ouroboros-terraform`, and `pokeemerald`; each plan printed the expected root flake and each readiness dry-run printed `readiness_mode: runtime-only`.

    nix --extra-experimental-features 'nix-command flakes' develop ./overlays/<overlay> --output-lock-file /dev/null --command sh -c 'test "$DEVKIT_NIX_SHELL" = "$expected"'
    Result: exited 0 for all eight overlays. `find overlays -maxdepth 2 -name flake.lock -print` produced no output.

Canary evidence:

    nix --extra-experimental-features 'nix-command flakes' develop --command sh -c 'cd cli/devctl && go test -count=1 ./...'
    Result: exited 0; all Go packages passed.

    nix --extra-experimental-features 'nix-command flakes' develop --command make -C cli/devctl build
    Result: exited 0; `kit/bin/devctl` was rebuilt.

    nix --extra-experimental-features 'nix-command flakes' flake check
    Result: exited 0; root metadata validation accepted `_template` as overlay-local canary and kept other overlays on root refs.

    nix --extra-experimental-features 'nix-command flakes' develop --command make overlay-runtime-smoke
    Result: exited 0 with `overlay-runtime-smoke: ok`; output showed `_template (./overlays/_template#default)` and `dev-all (.#dev-all)`.

    kit/scripts/devkit image-matrix --all --check
    Result: exited 0 with `image-matrix: OK`; `_template` reported `./overlays/_template#default`.

    kit/scripts/devkit --dry-run -p dev-all native plan --repo ouroboros-ide
    kit/scripts/devkit --dry-run -p dev-all ensure-ready --repo ouroboros-ide --runtime-only
    Result: exited 0; `dev-all` still printed `flake: .#dev-all`.

    kit/scripts/devkit --dry-run -p _template native plan --repo your-repo-name
    kit/scripts/devkit --dry-run -p _template ensure-ready --repo your-repo-name --runtime-only
    Result: exited 0; `_template` printed `flake: ./overlays/_template#default`.

    nix --extra-experimental-features 'nix-command flakes' develop ./overlays/_template --output-lock-file /dev/null --command true
    find overlays -maxdepth 2 -name flake.lock -print
    Result: `nix develop` exited 0 and `find` produced no output.

## Interfaces and Dependencies

The root flake remains the source of truth for tool pins and dev shell composition. Overlay-local flakes should depend on the root flake through a relative path input and delegate to root `devShells`. Production CLI-facing `runtime.flake` values in `overlays/*/devkit.yaml` remain root-compatible in this slice; `_template` is the supported canary for the overlay-local `./overlays/_template#default` shape.
