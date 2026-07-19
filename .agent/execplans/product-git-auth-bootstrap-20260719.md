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
- An isolated package-owned worktree root may live outside the host dev root.
  Its linked-worktree gitdir is normal portable metadata when it belongs to the
  configured root and resolves through the source repository's owned common
  Git directory; host path spelling alone must not make it exceptional.
- Owned `.git`, `commondir`, and reverse-gitdir pointers are relative.
  External source-linked common repositories remain the narrow absolute-path
  exception and malformed or traversing topologies fail closed.
- Each native consumer root owns its common bare repository beneath that same
  root. Agent and nested worktrees are linked beneath the root, so host and
  sandbox projections preserve one relative topology without an ambient source
  checkout, host alias, or metadata rewrite during launch.
- The package-owned Unix CONNECT tunnel remains full-duplex for the complete
  Git smart-protocol lifetime. A clean EOF in either direction half-closes only
  that destination write side and cannot truncate a pack still draining in the
  opposite direction. Copy failure closes both peers with a directional error;
  cancellation closes the tunnel without waiting on an open child input stream.
- Remote fetch is governed by observable protocol progress rather than the
  ten-second wall-clock bound used for local metadata operations. Git,
  OpenSSH, and the package-owned ProxyCommand run as one managed descendant
  group; a truly idle or cancelled fetch terminates and waits for that complete
  group before bootstrap cleanup.
- Every lifecycle that reconstructs native worktrees, including the installed
  top-level `-p dev-all reset 3` path, derives the same agent-1 bootstrap plan,
  starts the same managed egress proxy, and supplies the same immutable
  package-owned SSH command to `SetupNative`.
- After materialization, every requested linked worktree receives its own
  package-owned SSH/worktree config before normal preparation or readiness.
  Readiness owns a bounded managed-proxy lifetime and reports cleanup failure;
  it never relies on a socket left by preparation.

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
- [x] Publish Devkit `be02d57ee140401be23efba945d442756edeef15`.
- [x] Publish WSL/Nix `5cc33657bd78f3f9323aa450961544bce2f17e8f`
  after the full clean gate and direct/source-selected-Colmena equality proof.
- [x] Obtain the terminal canonical apply and two fresh Product proofs for the
  package-owned SSH/Unix-proxy first-fetch and cleanup contract.
- [x] Classify the remaining Product failure: isolated top-level roots outside
  `devRoot` were deliberately left with absolute `.git` metadata despite being
  linked to the package-owned common repository.
- [x] Replace the host-root test with configured-worktree-root plus canonical
  common-repository topology validation, relativeize all owned metadata
  pointers, and retain the external-linked-source exception.
- [x] Add two-root, host/sandbox-projection, ref-write, no-host-alias, traversal
      rejection, and top-level prepare/exec regressions to the named Devkit check.
- [x] Classify the post-`ad338bc` Nix failure as an escaped reverse link: the
      linked worktree was under the selected root, but its ambient common
      repository was not, so Nix/libgit2 resolved the reverse link to a
      host-only absolute root after projection.
- [x] Replace ambient common-repository reuse with an atomic, marker-validated,
      bare common repository beneath each declared native worktree root.
      Preserve the one package-owned SSH/Unix-proxy fetch authority.
- [x] Require the entire `.git`/`commondir`/reverse-link topology to remain
      beneath that root, preserve per-worktree non-bare configuration, and
      reject standalone, foreign, traversing, stale, and partial bootstrap
      states.
- [x] Extend the declared WSL Nix compatibility check with a real authoritative
      Nix flake check after projection to an unrelated root.
- [x] Publish the owned-topology repair as Devkit
      `bbcb9e9692e828740205a46d44df6d77f5119370`.
- [x] Publish the authoritative WSL repin as
      `d709d4490fa6818c479203ae6c224c6b7498e994` after the real projected Nix
      regression, full clean gate, and direct/source-selected-Colmena equality.
- [x] Obtain the terminal canonical apply and two independent fresh Product
      receipts. Both select the exact package SSH/Unix-proxy authority and then
      fail their first nontrivial fetch with `early EOF`, before worktree
      creation and with complete proxy/bootstrap cleanup.
- [x] Trace the shared failure to first-direction-wins tunnel relay logic in
      `egressproxy.handleConnect` and the compatibility bridge.
- [x] Publish the half-close-aware Devkit transport lifecycle as
      `6c609a64ce5632804afb9a2bf71adc7c29b6126c`, repin/publish WSL as
      `1ac007b2ee2abb1a1ec19170350791ea6109b743`, and obtain an exact canonical
      apply receipt.
- [x] Reproduce the next two-consumer `early EOF` through the complete installed
      Git -> OpenSSH -> package ProxyCommand -> Unix Serve -> outer CONNECT
      chain. On unmodified `6c609a`, a deliberately healthy upload-pack still
      draining after ten seconds is killed at 10.13 seconds by
      `worktrees.run`, after which OpenSSH reports remote close/broken pipe.
- [x] Identify why the earlier regression was not representative: it called
      `Connect` from a fake SSH helper against a local upload-pack, completed
      below one second, and never crossed either real OpenSSH process ownership
      or the universal worktree-command deadline.
- [x] Publish the phase-aware Devkit fetch lifecycle and its declared actual
      OpenSSH/ProxyCommand plus stall/cancellation/descendant regressions as
      Devkit `9697daaf39a9fbdba677d78e9766ad0573dae23a` and consume it in WSL/Nix
      `cb6729b49c0f9259f255caacaeac9c5ea2ec651e`.
- [x] Reproduce the installed `-p dev-all reset 3` failure before workspace
      mutation: lifecycle reconstruction reached `SetupNative` without the
      package-owned SSH command required for the declared Product origin.
- [x] Route reset/up reconstruction through the same
      `prepareNativeGitBootstrapAndWorktrees` authority as native prepare,
      preflight protected roots before broker or bootstrap effects, bind each
      materialized slot to its package SSH command, and give readiness its own
      managed-proxy lifetime.
- [x] Add an executable exact top-level reset regression proving one SSH fetch,
      three clean relative worktrees, source-defined app-server preparation,
      clean proxy lifecycle, preservation of owned stale payload, and
      pre-effect rejection of foreign metadata.
- [ ] Publish the reset lifecycle repair through the declared Devkit checks.
- [ ] Rebase the WSL patches, repin the declared check, and repeat the clean
      full check plus direct/source-selected-Colmena equality gate.
- [ ] Return the exact tuple to the protected apply owner; do not apply here.

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
- After the accepted `5cc3365` apply, two independent Product consumers proved
  the SSH/bootstrap repair and then exposed the remaining metadata rule:
  `rewriteNativeGitdir` used `devRoot` containment for both the worktree and
  gitdir. The controller's isolated worktree roots are outside `devRoot`, while
  their common repository is still the package-owned source under `devRoot`.
- The first post-half-close regression was green because it omitted the actual
  OpenSSH and ProxyCommand processes and finished in about 0.18 seconds. The
  live fetch and the new installed-chain test instead enter `worktrees.run`,
  whose unconditional ten-second `CommandContext` kills only top-level Git.
  That makes OpenSSH and the package-owned CONNECT descendant unwind while a
  healthy remote upload-pack is still draining, producing the observed
  `early EOF`, remote close, and broken pipe.
- The installed top-level reset command did not call native prepare. Its
  `reset -> down -> up -> lifecycleUp -> SetupNative` path started the broker
  and then invoked worktree reconstruction directly, omitting the agent plan,
  package SSH command, and managed proxy used by one-shot prepare.
- Once reset used the shared bootstrap helper, multi-slot preparation exposed
  a second ordering dependency: configuring agent 1 enabled shared
  `extensions.worktreeConfig` while agents 2/3 still inherited the common
  bare repository. Addressing each owned gitdir directly lets every slot set
  `core.bare=false` before the normal `git -C` worktree proof.
- Lifecycle readiness also called `launch.Prepare` after preparation had
  correctly removed its proxy socket. Giving each readiness report its own
  package-managed proxy lifetime makes the required bind truthful and preserves
  cleanup on every result.

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
- Treat the isolated-root correction as enforcement of the same accepted v3
  portability/no-host-alias decision, not a new transport or mount authority.
  Ownership requires the exact configured worktree root, the canonical source
  common repository, a per-worktree gitdir beneath its `worktrees/` directory,
  and a reverse pointer back to that exact worktree.
- Treat the owned-common lifecycle as direct enforcement of that accepted v3
  decision rather than a new Ouro choice. The existing decision already
  requires one portable relative topology, prohibits ambient Git metadata and
  host aliases, and selects the package-owned SSH/Unix-proxy fetch. A common
  repository outside the projected owned root cannot satisfy those constraints;
  moving that same repository authority beneath the root removes the ambient
  authority without adding a new transport, credential, launcher, mount, or
  consumer exception.
- Treat the full-duplex relay correction as enforcement of the existing
  single-SSH-authority decision. It changes no endpoint, identity, credential,
  protocol, allowlist, launcher, timeout, or retry authority; it only prevents
  the selected CONNECT tunnel from closing its opposite direction when the
  first copy reaches EOF. No new Ouro decision is required.
- Treat the fetch lifecycle correction as enforcement of the same accepted
  single-SSH-authority decision, not a new Ouro choice. It retains the exact
  Git/OpenSSH/ProxyCommand/Unix-proxy chain and replaces an unrelated universal
  metadata-command wall clock with one progress-aware fetch policy. It adds no
  endpoint, retry, fallback, caller override, or consumer exception. Short
  metadata operations retain their fixed bound; idle/cancelled fetches
  terminate and wait for the whole descendant group.
- Treat reset/up coverage as mechanical enforcement of the same accepted
  single-SSH-authority decision. It reuses the existing plan, identity,
  `GIT_SSH_COMMAND`, managed proxy, and owned-root preflight; it introduces no
  endpoint, credential, transport, launcher, caller input, or alternate
  convergence choice, so no additional Ouro decision is required.

## Files

- `cli/devctl/internal/worktrees/worktrees.go`
- `cli/devctl/internal/worktrees/worktrees_integration_test.go`
- `cli/devctl/internal/execx/run.go`
- `cli/devctl/internal/execx/run_test.go`
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
The prior published bootstrap repair passed the complete declared Devkit gate
and its named `native-bootstrap-stdio-cleanup` check. The follow-up isolated
metadata correction keeps that check as the sole regression authority and adds
the real top-level prepare/exec and two-root metadata topology cases to it.
Follow-up publication and WSL convergence remain.
