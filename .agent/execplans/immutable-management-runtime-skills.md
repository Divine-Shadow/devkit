# Immutable Management Runtime Skills

## Purpose

Fresh `dev-workspace` consumers must obtain Management skills from the
source-derived Nix package identified by the authoritative WSL environment.
Mutable workspace copies must not override that package, and malformed or
missing package identity must fail preparation.

## Progress

- [x] Read repository guidance and locate the production `Prepare` skill-link
  path.
- [x] Agree the v2 identity fields with the WSL source owner.
- [x] Implement strict package identity and complete content validation.
- [x] Reconcile only mechanism-owned links and write a consumer receipt.
- [x] Exercise initial preparation, transition cleanup, failure cases, and
  immutable precedence through `Prepare`.
- [x] Add the manifest-bound `management-controller-convergence/v1` v4
  projection using the existing native Plan/Prepare/Bubblewrap path.
- [x] Run focused and repository Go gates; leave the worktree uncommitted for the
  parent owner.

## Contract

The required environment is `DEVKIT_MANAGEMENT_SKILLS_ROOT`,
`DEVKIT_MANAGEMENT_SOURCE_REV`, and `DEVKIT_MANAGEMENT_SKILL_SHA256`.
`identity.json` uses schema `wsl-nix-management-runtime-skills/v2` and declares
`managementSourceRev`, `packagePath`, `skillsRoot`, compatibility
`skillPath`/`skillSha256`, and the complete sorted
`files:[{path,sha256}]` manifest. The root must be the declared real
`/nix/store/.../share/management-runtime-skills/skills` directory.

Product skills continue to link from the Product worktree after immutable
Management names have claimed precedence. Existing foreign files and
directories are refused. A package transition may remove or replace only
links recorded by the prior Devkit receipt and still pointing at its recorded
immutable root.

The named controller profile is requested only by the exact Management
consumer marker. Its strict Nix-authored manifest declares the source roots,
inventory hashes, broker/source-acquisition packages, operation socket
identity, target allowlist, and operation schemas. Devkit validates the two
clean Git roots and the live socket inode, then extends workspace-egress v3 to
v4 with the WSL source root, reconnectable operation handle, and immutable
inventories. The legacy direct DrTalos exec handle is deliberately absent from
v4 so every effect remains typed, persisted, and reconnectable through the
operation broker. Product and other profiles remain on v3, including their
pre-existing non-v4 controller-handle behavior where applicable.

## Verification

Focused Go tests call the real `Prepare` path and cover the success and failure
contract. The package test suite and formatter must pass. Completion evidence
is test output plus a clean classification of every worktree change; runtime
convergence and publication remain outside this writer's authority.

Focused `plan` and `launch` packages passed, including a real Unix-socket,
committed-Git-worktree Plan/Prepare/BuildBubblewrap test. `go test -count=1
./...` passed across the complete Devkit CLI package tree. The existing SSH
seed test exposed umask-dependent public-key permissions; `SeedSSH` now applies
the declared mode explicitly after writing, and the full suite passes under
the controller's restrictive umask. A cross-repository integration audit then
removed the legacy direct exec socket and environment from the v4 plan while
adding negative assertions that a present legacy socket is not projected.
