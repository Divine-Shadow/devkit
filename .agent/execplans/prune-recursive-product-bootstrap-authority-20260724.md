# Prune Recursive Product Bootstrap Authority

## Purpose

Devkit must supply the generic agent runtime and lifecycle substrate. It must
not choose Product source, derive Product artifacts, construct a second Product
runtime identity, or advertise fixture behavior as a promoted Product agent.
WSL/Nix will compose this generic package with the outputs of one accepted
Product revision.

## Progress

- [x] Verify `origin/master` is `1dd0670c6f515951a635702cd051c1840cb7821b`.
- [x] Audit the Product-special ancestry and identify `84bf9c8` as the last
  generic runtime boundary with package-owned Git/OpenSSH and pinned host keys.
- [x] Reverse the post-`84bf9c8` Product adapter, selector, fixture, consumer,
  auth-seed, source-transport, and source-acquisition lineage in the working
  tree.
- [x] Remove the older Devkit-owned Product artifact selection and replace its
  bundle with a generic `dev-all` tool/runtime package.
- [x] Prune operative Product-exception guidance and add focused generic
  runtime checks.
- [x] Run hermetic Go and full Nix verification and update this plan.
- [x] Commit the clean change without push.

## Decisions

- Do not retain Product-special compatibility shims. The supported WSL
  composition input is the generic `dev-all-runtime-bundle` containing the
  compiled Devkit CLI and generic `dev-all-runtime-tools`.
- Preserve package-owned Git/OpenSSH, strict host-key configuration,
  bubblewrap/proxy support, Codex, worktree lifecycle, ordinary home/auth
  seeding, and process supervision.
- Tests in this change prove the generic package is buildable and contains its
  real tools. They do not claim that a governed Product VM has been promoted.

## Verification

- Repository search finds no Product adapter, source acquirer, opaque runtime,
  selector, fake Product MCP/connect fixture, or Devkit-owned Product artifact
  pin in operative code or guidance.
- The hermetic `devctl-go-tests` check compiles the complete Go package graph
  and runs every portable unit-test package. Host/repository integration tests
  remain real source-checkout tests and are not replaced with Nix-only fixtures.
- `nix flake check --show-trace` passes, including the generic runtime bundle
  and immutable Git/OpenSSH authority checks.
- The final worktree is clean after one local commit based on the verified
  remote head. Publication remains the parent owner's decision.

## Outcomes & Retrospective

- Removed the recursive Product adapter/runtime/source-acquisition stack,
  selector and credential authorities, fake Product MCP/connect/readiness
  executables, special network profile, and their active plans and decisions.
- Replaced Devkit's Product artifact bundle with one generic package exposing
  `bin/dev-all-runtime-tools` and `kit/bin/devctl`. WSL/Nix is now the sole
  composition layer for the accepted Product flake revision and artifacts.
- `nix flake check --show-trace -L` passed all 11 checks, including the
  hermetic portable Go suite, generic runtime inventory, package-owned
  Git/OpenSSH identity, overlay metadata, and retired-runtime guards.
- The built `.#dev-all-runtime-bundle` contains both generic interfaces and no
  Product artifact/source-selection path. `git diff --check` passed.
- This is enabling cleanup only. It does not claim the parent disposable VM
  lifecycle has passed.
