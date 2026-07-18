# Product linked-worktree portability

This ExecPlan repairs a support invariant used by governed Product GUI tasks.
It is not a Product business objective.

## Purpose

Native Product worktrees and Codex-created nested worktrees must keep working
when the canonical host dev root is projected into a workspace-egress sandbox.
Git and the governance flake evaluator must not depend on an unmounted absolute
host path.

## Violated invariant

`SetupNative` explicitly disabled Git relative-worktree metadata and rewrote
each native `.git` file to an absolute host gitdir. A later nested worktree
therefore captured the host source path. The workspace-egress runtime attempted
to compensate with an exact host alias, but the producer had already made
portable computation depend on station namespace identity.

## Source repair

- Configure native repositories with `worktree.useRelativePaths=true`.
- Rewrite existing and new native worktrees to relative gitdirs when their
  common metadata is inside the canonical dev root.
- Preserve an absolute gitdir only for a linked source whose common repository
  is genuinely outside that root.
- Project relative common Git metadata into the sandbox-resolved canonical path
  with a required narrow bind.
- Advance the workspace-egress mount-policy identity to v3 so consumers can
  prove they are using the portable metadata contract.

## Progress

- [x] Reproduced Git's relative metadata behavior for an outer native worktree
  and a nested Codex-style worktree.
- [x] Implement the source repair and hostile regression tests.
- [x] Run focused Go tests and the isolated Nix `devctl` package build.
- [ ] Run the full repository/Nix acceptance from a consumer that exposes the
  pinned Product source required by the complete flake.
- [ ] Publish Devkit master and update the canonical WSL/Nix pin.
- [ ] Converge through centralized fleet deployment.
- [ ] Prove two fresh Product consumers can run Git identity and governance
  status/admission canaries without local mutation.

## Verification

Focused tests must prove a nested worktree survives relocation of the complete
dev-root topology, an external linked source retains a resolvable absolute
gitdir, and the workspace-egress plan contains only narrow exact metadata binds
including the required canonical sandbox projection. Full repository and Nix
checks must pass before publication. Acceptance requires two fresh Product
consumers from the converged closure.

## Decision log

- Repair the metadata producer rather than add another candidate- or
  path-specific fallback.
- Keep the existing exact metadata alias for genuinely external linked-source
  repositories; it remains a narrow declared mount, not an alternate path.
- Reuse no output from the failed Product task. Its business objective remains
  independent and resumes only from a fresh governed consumer.

## Outcomes and retrospective

Pending.

The focused `worktrees`, runtime-plan, and workspace-egress launcher tests
passed. `nix build .#devctl` produced
`/nix/store/fgy3m6v9bz7izh6s0c66lscjqdh4yap0-devkit-devctl-dev`. A direct
host-environment `go test ./...` also exercised all packages but retained
pre-existing environment-sensitive failures in integration and home-config
tests; the isolated Nix package build passed and is the authoritative package
acceptance. Full flake acceptance remains pending because this restricted
consumer intentionally does not expose `/workspaces/dev/ouroboros-ide`.
