# Product Git OpenSSH Executable Authority

## Purpose

Make every promoted Devkit Product reset/bootstrap and every persisted or
explicit Git SSH command use one absolute OpenSSH executable selected by the
immutable Devkit package, together with the existing source-derived SSH
configuration. This closes the fresh-consumer failure where the configuration
was correct but Git evaluated a bare `ssh` under a protected empty `PATH`.

The accepted decision is Management
`9bb328ef7c72d944e5a23dc52c8d2034a63137a0`,
`docs/decision-framework/governance_decisions/20260719_product_git_bootstrap_single_ssh_authority.md`.
That revision includes the required named end-to-end fresh-consumer lifecycle
gate.

## Outcome contract

- One internal source-controlled SSH authority owns command construction.
- The production `devctl` package binds that authority at link time to
  `${pkgs.openssh}/bin/ssh`; no environment, caller flag, PATH lookup, fixed
  system path, fallback, or second launcher can replace it.
- The Devkit package directly references OpenSSH and its transitive closure
  contains the selected OpenSSH store path.
- Reset/bootstrap preflight rejects an empty, relative, missing, directory, or
  non-executable bound path before proxy, Git, network, common-repository, or
  worktree effects.
- Bootstrap `GIT_SSH_COMMAND`, per-home global `core.sshCommand`, linked
  worktree `core.sshCommand`, and promoted explicit Git SSH emitters use the
  same authority and exact source-derived config path.
- Existing identity, managed CONNECT proxy, GitHub port 443, relative metadata,
  bwrap, mount, cleanup, and owned-root contracts remain unchanged.
- A named flake check starts with no Product consumer and invokes the actual
  packaged Devkit entrypoint through one hermetic lifecycle. It proves proxy
  readiness, use of the package-selected immutable SSH injection slot,
  protected identity seeding, fixture checkout, portable writable Git
  metadata, normal prepared HOME/CODEX_HOME/config, the app-server/task-launch
  boundary, and complete cleanup without external network, credentials, GUI,
  or station effects. A separate production-package check proves that the same
  slot is exactly `${pkgs.openssh}/bin/ssh` and that OpenSSH is in the closure.

## Progress

- [x] Read Devkit `AGENTS.md` and `.agent/PLANS.md`.
- [x] Read the amended accepted decision at Management
      `9bb328ef7c72d944e5a23dc52c8d2034a63137a0`.
- [x] Create a fresh network clone and fast-forward it to clean Devkit
      `origin/master` `7bec227efebcf8d5c0ce870e920854a2328b0cb2`.
- [x] Audit package construction and promoted SSH command emitters.
- [x] Implement the compile-time package authority and migrate emitters.
- [x] Add focused unit, integration, persistence, and pre-effect rejection
      tests.
- [x] Export and pass the named packaged absent-consumer lifecycle flake check.
- [x] Prove the built package embeds the exact OpenSSH store executable and its
      closure contains OpenSSH.
- [x] Run focused Go tests, full `go test ./...`, the named check, and full
      `nix flake check --show-trace`.
- [ ] Review, commit, push to Devkit `master`, and read back a clean matching
      local/remote commit and tree.

## Surprises and discoveries

- Current trunk already owns proxy-before-fetch ordering, an explicit
  `GIT_SSH_COMMAND`, native reset reconstruction, relative linked-worktree
  topology, phase-aware Git fetch lifetime, and cleanup. The repair must not
  duplicate or broaden those mechanisms.
- `launch.GitBootstrapSSHCommand`, normal global/worktree Git configuration,
  and older explicit Git command helpers still construct bare `ssh -F ...`.
- `mkDevctl` includes OpenSSH elsewhere in development/test environments but
  does not link the production executable to `${pkgs.openssh}/bin/ssh`, so the
  Devkit package itself does not own the executable choice.
- The existing installed empty-root integration test provides most lifecycle
  assertions but redirects through a hostile PATH `ssh`. The widened decision
  requires the opposite proof: hostile `ssh` must remain unreachable while the
  package-selected executable reaches only a local fixture origin through the
  package-owned proxy chain.
- A Nix test package uses the same `mkDevctl` derivation and link-time injection
  slot as production, but binds a hermetic fixture executable so the lifecycle
  can prove exact selection and Git smart-protocol checkout without a real
  credential, SSH daemon, or network. The independent production derivation
  binds `${pkgs.openssh}/bin/ssh` and proves the exact store reference and
  closure membership.

## Decision log

- Add one internal `sshauthority` package. Its package path is a link-time
  string, and its resolver validates an absolute executable regular file.
  Tests may inject an explicit authority through source APIs; production
  callers use only the linked package authority.
- Resolve the authority before starting the managed proxy or mutating bootstrap
  homes/worktrees. Revalidation at persistence boundaries is acceptable; a
  fallback is not.
- Keep the existing SSH configuration and ProxyCommand generator. Only the
  executable prefix changes.
- Use a package-owned fixture SSH executable, a local bare Git origin, and a
  local outer CONNECT proxy for the named packaged check. This exercises the
  exact production injection mechanism and every Git command emitter while
  keeping external network and credentials unreachable. Prove real OpenSSH
  ownership separately on the production package and closure.
- Treat stale, unused bare SSH helper emitters as invariant violations even if
  they are not on the current reset path: migrate or remove their command
  generation so a future promoted path cannot reintroduce ambient authority.

## Files

- `flake.nix`
- `cli/devctl/internal/sshauthority/authority.go`
- `cli/devctl/internal/sshauthority/authority_test.go`
- `cli/devctl/internal/runtime/launch/launch.go`
- `cli/devctl/internal/runtime/launch/launch_test.go`
- `cli/devctl/internal/commands/nativecmd/native.go`
- `cli/devctl/internal/commands/nativecmd/native_test.go`
- `cli/devctl/internal/ssh/ssh.go`
- `cli/devctl/internal/ssh/ssh_test.go`
- `cli/devctl/internal/sshsteps/sshsteps.go`
- `cli/devctl/internal/sshsteps/sshsteps_test.go`
- `cli/devctl/main.go`
- `cli/devctl/service_test.go`
- `cli/devctl/integration/native_defaults_dryrun_test.go`
- `.agent/execplans/product-git-openssh-executable-authority-20260720.md`

## Verification

Focused Go tests:

```bash
go test ./internal/sshauthority ./internal/runtime/launch \
  ./internal/commands/nativecmd ./internal/worktrees ./internal/ssh \
  ./internal/sshsteps ./integration -count=1
```

Full Go and Nix gates:

```bash
go test ./...
nix build .#packages.x86_64-linux.devctl --print-out-paths
nix build .#checks.x86_64-linux.product-fresh-consumer-ssh-authority \
  --print-out-paths --show-trace
nix flake check --show-trace
```

Package receipt:

```bash
grep -aF '<exact-openssh-store-path>/bin/ssh' \
  '<devctl-package>/kit/bin/devctl'
nix-store -q --references '<devctl-package>'
nix-store -qR '<devctl-package>'
```

## Acceptance

Publication is allowed only when the fresh checkout is clean except for the
intended committed change, every focused/full gate is green, the named packaged
fresh-consumer check is present in the full flake check, the package and
closure receipts select exactly one OpenSSH store authority, and pushed
`origin/master` matches the accepted local commit and tree.

## Outcomes and retrospective

The named
`checks.x86_64-linux.product-fresh-consumer-ssh-authority` derivation is green.
It starts from an empty Devkit consumer root, leaves a hostile ambient `ssh`
unselected, observes the source-managed CONNECT to `ssh.github.com:443`, fetches
the local fixture origin, creates three clean relative linked worktrees, reads
and writes their metadata, reads back the exact persisted SSH commands, reaches
the ordinary app-server/task-launch boundary with the prepared homes and
configuration, and removes the disposable consumer and runtime residues.

`checks.x86_64-linux.devctl-openssh-executable-authority` is also green for the
production package. It finds the exact `${pkgs.openssh}/bin/ssh` string in the
packaged binary and the exact OpenSSH output in closure metadata.

Full `go test -count=1 ./...` is green, including the complete integration
package. Full host-system `nix flake check --show-trace` is green across all 14
checks; its named fresh-consumer run completed in 1.50 seconds and the existing
focused native bootstrap/stdio/cleanup integration run completed in 40.39
seconds. Publication remains in progress.
