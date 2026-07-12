# Codex Home Preservation

This ExecPlan is a living document. It is maintained according to `.agent/PLANS.md`.

## Purpose

Native agent setup must preserve existing Codex home state across repeated
runs. Existing `.codex` contents, including auth, rollout state, config, and
other continuity files, should survive reset, anchor, direct-home, and native
prepare/setup flows while missing support directories are still created.

## Progress

- [x] (2026-05-14T23:45:00Z) Reviewed current uncommitted seed changes and
  native setup call sites.
- [x] (2026-05-15T00:23:32Z) Removed remaining destructive Codex-home seed
  behavior.
- [x] (2026-05-15T00:23:32Z) Added focused tests for reset, seed,
  force-reseed, anchor, and direct-home preservation behavior.
- [x] (2026-05-15T00:23:32Z) Added a native guard that proves repeated setup
  preserves Codex state through `kit/scripts/devkit`.
- [x] (2026-05-15T00:23:32Z) Ran required verification and prepared the scoped
  preservation commit.

## Surprises & Discoveries

- Reset, anchor, and direct-home paths had already been changed to stop
  removing the entire `.codex` directory.
- Force reseeding still removed `.codex/auth.json`, which can destroy auth when
  host auth mounts are absent. The preservation invariant should keep existing
  auth in that case and overwrite only when a replacement source exists.
- Native lifecycle setup calls `launch.Prepare` repeatedly from `up`, `exec`,
  readiness, and `native prepare`, so a guard can prove repeated setup by
  placing sentinel files in the agent host home and invoking `kit/scripts/devkit`
  again.

## Decision Log

- Decision: Keep the implementation scoped to seed/home lifecycle behavior,
  focused tests, and a small guard script.
  Rationale: The desired behavior is a Codex-home lifecycle invariant, not a
  broader home migration or runtime refactor.
- Decision: Preserve existing auth during force reseed if no host replacement is
  readable.
  Rationale: Repeated runs should not make an in-flight agent lose auth just
  because a reseed source is temporarily unavailable.

## Acceptance

Acceptance requires:

- Reset planning and reset shell snippets create required directories without
  deleting `.codex`.
- Seed and force-reseed snippets preserve existing auth, rollout, and config
  sentinels while creating missing directories and the seeded marker.
- Anchor and direct-home generated scripts contain required directory creation
  and no destructive `.codex` cleanup.
- A canonical `kit/scripts/devkit` guard proves repeated native setup preserves
  `.codex` sentinels and exposes them inside the sandbox.
- Focused seed tests pass.
- Relevant native lifecycle tests or guards pass.
- `make ci-cheap` passes.
- Current seed/home lifecycle sources contain no destructive Codex-home cleanup
  commands.

## Outcomes & Retrospective

The implementation preserves existing `.codex` state instead of deleting it.
`BuildResetPlan`, `ResetAndCreateDirsScript`, anchor seeding, direct-home
seeding, and force reseed now create required directories without removing the
Codex home. Force reseed still overwrites auth when a readable host auth source
exists, but it no longer deletes existing auth when that source is unavailable.

Verification passed:

- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command go test -count=1 ./internal/seed`
- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command make native-codex-home-preservation-guard`
- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command go test -count=1 ./internal/seed ./internal/runtime/launch ./internal/commands/nativecmd`
- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command make ci-cheap`
- `nix --extra-experimental-features 'nix-command flakes' develop /home/bayesartre/dev/devkit/overlays/dev-all#default --output-lock-file /dev/null --command make native-e2e-lifecycle`
- `bash -n kit/scripts/native-codex-home-preservation-guard`
- `git diff --check`
- `find overlays -name flake.lock -print` returned no files.
- A destructive Codex-home cleanup and retired-runtime pattern search returned
  no matches in the scoped sources.

The native guard writes auth, rollout, and config sentinels into the agent home
after an initial `up`, runs `up` again through `kit/scripts/devkit`, and then
uses native `exec` to prove the preserved state is visible inside the sandbox.
