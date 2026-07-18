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
- [x] Run the full repository/Nix acceptance from a disposable consumer that
  exposes the pinned Product source required by the complete flake.
- [x] Publish Devkit master.
- [ ] Update the canonical WSL/Nix pin through its existing sole writer.
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
passed. `nix build .#devctl` first produced an isolated package candidate.
After supplying the flake's pinned Product repository as disposable read-only
input, `nix flake check --show-trace` passed all nine x86_64 checks. A direct
host-environment `go test ./...` also exercised all packages but retained
pre-existing environment-sensitive failures in integration and home-config
tests; the isolated Nix package build and complete flake check passed and are
the authoritative acceptance.

The implementation landed on Devkit master as `294dd3829c2c95725d67c360e92ddf6cb6235781`.
The test name was then adjusted in
`53f01af5808feb33c83c769566c8dd93192fc0a2` because the static retired-runtime
guard correctly treats the contiguous legacy wrapper token as forbidden even
inside an otherwise unrelated test identifier. The final source readback and
downstream WSL/Nix pin are recorded after this plan closeout commit.
