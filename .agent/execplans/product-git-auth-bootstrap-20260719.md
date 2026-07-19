# Product Git Authentication Bootstrap

## Purpose

Make canonical `devctl -p dev-all native prepare` able to reconstruct a missing
Product linked worktree without ambient Git configuration, HTTPS fallback, or
station-local credential repair.

The governing Ouro decision is
`docs/decision-framework/governance_decisions/20260719_product_git_bootstrap_single_ssh_authority.md`
in the Management repository. It requires the package-owned per-consumer SSH
identity and managed egress proxy to exist before the first fetch and requires
Git to receive that exact SSH command explicitly.

## Outcome contract

- `native prepare` derives the agent plan before its first remote fetch.
- The managed egress proxy is accepting before the bootstrap callback.
- The existing Devkit SSH seeding contract creates the per-consumer identity
  files; no caller or task copies or mounts credentials.
- The bootstrap config points at host-visible per-consumer identity and
  known-host paths and routes GitHub SSH through `ssh.github.com:443` and the
  exact per-consumer Unix managed CONNECT proxy. It does not select the fixed
  loopback runtime bridge used only after bubblewrap launch.
- The fetch receives the exact command through `GIT_SSH_COMMAND`, with global
  and system Git configuration disabled.
- A Product HTTPS, file, ambient, or missing-identity route fails before fetch
  and worktree creation.
- Normal post-materialization preparation rewrites the same managed config to
  sandbox-visible identity paths.
- The WSL v3 patch continues to enforce relative linked-worktree metadata and
  no normal-consumer host alias.

## Progress

- [x] Read current Management, WSL/Nix, Fleet, and Devkit authority.
- [x] Classify the stale sandbox `/run/current-system` separately from the
  authoritative `mp9` system profile.
- [x] Trace the first `SetupNative` fetch and prove it precedes
  `launch.Prepare` while suppressing configured SSH.
- [x] Interrupt the Product consumer after its unauthorized HTTPS fallback
  became visible; leave its worktree inert and uninspected.
- [x] Add package-owned bootstrap identity, explicit Git transport, managed
  proxy ordering, and hostile fallback tests.
- [x] Publish the accepted Ouro decision at Management
  `aa83e96a6aeaeaa7a6647ce4e59ccbdf445d6b14`.
- [x] Rebase the repair onto Devkit `origin/master`
  `5367ac3ac809fbec5cd24d28c6a07ed380c1f58c`.
- [x] Run focused and full Devkit checks on that published v3 base.
- [x] Diagnose the first protected fresh-consumer failure without reopening its
  disposable root: bootstrap SSH selected fixed `127.0.0.1:18888` before the
  package bridge existed.
- [x] Replace bootstrap loopback/netcat selection with the immutable
  source-derived devctl's stdio CONNECT helper bound to the exact per-consumer
  Unix socket.
- [x] Preserve upstream CONNECT bytes buffered with the HTTP 200 response.
- [x] Make native exec unwind through managed-proxy cleanup before preserving a
  child exit status, and cover byte-exact stdout projection.
- [x] Run the focused proxy/launch/worktree/native-command tests, full
  `go test ./...`, and all nine Devkit flake checks.
- [x] Make the native-exec integration fixture provide its declared resolver
  input so the same test passes inside the Nix build sandbox without ambient
  `/etc/resolv.conf`.
- [x] Export the real bootstrap/stdout/cleanup regression suite as pinned
  Devkit flake check `checks.x86_64-linux.native-bootstrap-stdio-cleanup` for
  authoritative WSL consumption.
- [ ] Publish Devkit master and record the exact commit.
- [ ] Rebase the WSL v3 patch onto the new Devkit input, update pin/lock and
  runtime identity assertions, and run the full WSL gate.
- [ ] Prove direct and source-selected Colmena return the exact same closure and
  deriver.
- [ ] Apply through Fleet Control to Colmena.
- [ ] Verify two fresh Product consumers and authorize one later Joe start-turn.

## Surprises and discoveries

- The local bwrap mount of `/run/current-system` still exposes a retired
  workspace-egress v2 launcher. The protected Windows controller and system
  profile expose the authoritative `mp9` closure and v3 package. The stale
  launcher dry-run is evidence of the stale-consumer failure mechanism, not an
  acceptance canary for the current closure.
- The current v3 source preserved the underlying `SetupNative` network defect:
  remote fetch used `env -u GIT_SSH_COMMAND` and
  `core.sshCommand=ssh -F /dev/null`; v3 corrected worktree metadata and later
  per-worktree configuration but not the first-fetch ordering.
- The fresh Product task selected HTTPS after SSH failed and created a
  worktree. That consumer is tainted and was interrupted without cleanup or
  reuse.
- The later protected Product consumer correctly prohibited HTTPS/ambient
  fallback but exposed a remaining ordering violation: `PrepareGitBootstrap`
  wrote a fixed-loopback `nc` ProxyCommand while the package-owned TCP-to-Unix
  bridge was not started until `BuildBubblewrap`, after the fetch.
- The managed proxy's upstream CONNECT parser returned the raw connection after
  reading through a new buffered reader. An SSH banner coalesced with the 200
  response could therefore be discarded.
- Native exec inherited stdout correctly, but `runCommandPreservingExit`
  called `os.Exit` inside the command handler. That skipped deferred cleanup
  and explains the terminal Management consumer's stale socket pathname.
- The first WSL package reconstruction correctly ran the new integration test
  in a Nix build sandbox and exposed one fixture-only ambient dependency:
  `/etc/resolv.conf` is absent there. Supplying the test's own `--resolv-conf`
  input keeps the production contract unchanged and makes the hostile test
  hermetic.

## Decision log

- Use the existing Devkit per-consumer SSH seeding and managed egress
  authorities; do not add another key store, proxy, launcher, or transport.
- Build agent 1's authoritative plan before `SetupNative`, because the shared
  Product repository fetch happens once before materializing all requested
  worktrees.
- Use host-visible per-consumer identity paths for the pre-worktree fetch, then
  let ordinary `launch.Prepare` replace the managed block with sandbox-visible
  paths after worktree creation.
- Use one package-owned stdio CONNECT subcommand from the immutable runtime
  authority root for bootstrap SSH. It talks only to the exact per-consumer
  Unix proxy socket and has no fixed-loopback, HTTPS, ambient-proxy, or binary
  fallback.
- Preserve ordinary native exec stdout directly. Cleanup occurs while command
  handlers unwind; only the top-level CLI translates a returned child exit
  error into its original status.
- Require the Product remote to remain SSH and reject HTTPS/file fallback.
- Treat this as a tooling remediation requiring the operator-mandated Ouro
  decision; no Product architecture or implementation authority is added. No
  new Ouro record is required for this follow-on because the accepted
  Management decision already binds the exact pre-fetch ordering, per-consumer
  identity, managed egress, and prohibition on a second transport authority.
  The same decision is present in the currently consumed unified
  Management/Fleet source `24f310d0750b55874a2f7043c7bf0e4adcdfed7f`.

## Files

- `cli/devctl/internal/worktrees/worktrees.go`
- `cli/devctl/internal/worktrees/worktrees_integration_test.go`
- `cli/devctl/internal/runtime/launch/launch.go`
- `cli/devctl/internal/runtime/launch/launch_test.go`
- `cli/devctl/internal/runtime/egressproxy/egressproxy.go`
- `cli/devctl/internal/runtime/egressproxy/egressproxy_test.go`
- `cli/devctl/internal/commands/nativecmd/native.go`
- `cli/devctl/internal/commands/nativecmd/native_test.go`
- `cli/devctl/integration/native_defaults_dryrun_test.go`
- `cli/devctl/main.go`
- `.agent/execplans/product-git-auth-bootstrap-20260719.md`

## Verification

Focused:

```bash
env -u DEVKIT_CODEX_CONFIG_SOURCE \
  -u DEVKIT_NATIVE_ISOLATION_PROFILE \
  -u DEVKIT_NATIVE_MOUNT_POLICY_IDENTITY \
  -u DEVKIT_OVERLAYS_DIR \
  go test ./internal/worktrees ./internal/runtime/launch \
    ./internal/commands/nativecmd -count=1
```

Full:

```bash
go test ./...
nix flake check --show-trace
```

The WSL owner then pins the accepted Devkit commit, rebases the v3 patch, runs
the WSL full flake check, builds the authoritative system directly and through
source-selected Colmena, compares exact store path and deriver, and applies
only through Fleet Control to Colmena.

## Acceptance

Two fresh Product consumers must independently report the same immutable
authority manifest and closure, the package-owned per-consumer Git identity
path, successful noninteractive SSH fetch of Product `origin/main`, relative
linked-worktree metadata, writable Git metadata, a read-only Product flake open,
and a governance admission canary. No task-local protocol switch, config edit,
credential copy, worktree repair, or alternate launcher counts.

## Outcomes and retrospective

Focused worktree, launch, native-command, proxy, and integration tests pass.
The complete Devkit Go suite passes with ambient consumer runtime variables
removed. `nix flake check --show-trace --no-write-lock-file` passes all nine
x86_64 checks, including rebuilt devctl derivation
`/nix/store/8khnrbyz03s89zd5qgg6r15bh1sar1dm-devkit-devctl-dev.drv`.
Publication, WSL convergence, protected-controller apply, and fresh-consumer
proof remain.
