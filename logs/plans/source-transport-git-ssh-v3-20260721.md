# Package-owned source transport v3 / Git-SSH v2

This bounded ExecPlan records a source-acquisition diagnostic repair. It is
not Product derivation, runtime promotion, consumer construction, deployment,
or business progress.

## Purpose and invariant

The compiled Devkit source layer must acquire exact source without ambient
Git, SSH, shell, PATH, Python, Product runtime, or a second identity authority.
OpenSSH executes `ProxyCommand` through `SHELL`, so the Git-SSH v2 helper binds
that interpreter to the package-owned Bash executable while retaining the
package-owned OpenSSH executable, SSH config, known hosts, CONNECT transport,
socket, and identity handles. The only public schema authorities are the Nix
interface values `devkit/source-transport/v3` and
`devkit/source-transport-git-ssh/v2`; Go does not duplicate them.

## Acceptance

- The exact package and interface checks prove the v3 transport and v2
  Git-SSH paths are immutable Nix-store members and the proxy shell is the
  package-owned Bash.
- The NixOS lifecycle begins with a hostile `PATH` and `SHELL`, no `/bin/sh`,
  and a hostile account login shell. It performs two independent real Git SSH
  clones through the package-owned Unix-socket CONNECT transport and verifies
  the exact fixture revision.
- Successful acceptance occurs only after the proxy and SSH daemon have been
  terminated and reaped with checked exit statuses, their socket is absent,
  both bind mounts are unmounted, all mutable private/public/authorized key
  copies are absent, the hostile shell was never invoked, and logs/receipts do
  not contain private-key markers or either public key payload.
- A separate idempotent `EXIT` trap performs best-effort cleanup on failure.
  Its cleanup is never used as evidence that the successful teardown gate
  passed.

## Progress

- [x] Preserve rejected commit `0453cefbb6975753bfbd3eb192a27f1940544b4f`
  as immutable parent history.
- [x] Remove the unused stale Go schema constant rather than synchronize a
  duplicate schema authority.
- [x] Add the explicit fail-closed teardown and no-secret evidence gate.
- [x] Pass focused Go tests and the full Go suite.
- [x] Build the exact package, interface, and NixOS lifecycle outputs.
- [x] Pass the full `nix flake check --show-trace`.
- [x] Record the exact immutable outputs and freeze an unpublished clean
  successor commit for independent review.

## Decision log

- Cleanup is an acceptance precondition, not deferred housekeeping.
- Mutable test credentials are disposable and must leave no residue. Test
  fixture keys remain immutable derivation inputs only; they are never emitted
  in logs, receipts, or lifecycle output.
- This check proves only compiled package-owned source acquisition. It cannot
  promote a Product runtime or stand in for the later authoritative
  derivation, manifest, deployment, fresh-consumer, or governed-Scala gates.

## Verification receipts

- `go test -count=1 ./internal/sourcetransport -run
  '^Test(ValidateGitSSHArgs|OpenSSHEnvironmentBindsOnlyPackageShellAndGitProtocol)$'`
  passed.
- `go test -count=1 ./...` passed every package in `cli/devctl`.
- `nix build --no-link --print-out-paths .#source-transport
  .#checks.x86_64-linux.source-transport-interface` produced
  `/nix/store/cjm5qdb1rrq5bv7dzp4s359dzhm3p472-devkit-source-transport-dev`
  and
  `/nix/store/9fk30n6ksyv4w4sc3sqyhn2cqm9a9c2b-devkit-source-transport-interface`.
- `nix build --no-link --print-out-paths
  .#checks.x86_64-linux.source-transport-git-ssh-lifecycle` produced
  `/nix/store/ngc9klylx93kclnl6z6xvh33msww5mqz-vm-test-run-devkit-source-transport-git-ssh-lifecycle`.
- The first full `nix flake check --show-trace` exposed an existing 120 ms
  timing failure in `native-bootstrap-stdio-cleanup`; its exact isolated check
  then passed at
  `/nix/store/ywp8hzq0f7bk5cqw4jwisvd7p7q71ggb-devkit-native-bootstrap-stdio-cleanup-check-dev`.
  A second full `nix flake check --show-trace` passed all 19 x86_64-linux
  checks. This timing retry did not alter source.
