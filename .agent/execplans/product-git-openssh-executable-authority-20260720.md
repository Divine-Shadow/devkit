# Product Git OpenSSH Executable Authority

> **Current status: prerequisite-only implementation record.** This plan does
> not define Product publication, deployment, or promotion. Its focused/full
> checks and consumer fixture establish Devkit package prerequisites only.
> Current Product promotion belongs exclusively to the governed Product-owned
> twice-fresh promotion app consuming the sole installed
> `fleet-runtime-authority/v1` manifest. Devkit work freezes on an immutable
> candidate branch until that promotion accepts it.
>
> Candidate `9a5381d97bf7f5671853e379bd59722bf59f50a4` is preserved as
> rejected audit history, not a promotion input. Its successor must close the
> independent-review findings for descriptor-rooted stopped-volume writes,
> exact Codex PID/listener ownership, recursively closed manifest objects,
> cross-consumer geometry separation, and operative skill/plan routing before
> another candidate freeze.
>
> The commit containing this plan is the additive successor candidate. Its
> immutable commit hash, tree hash, remote-branch readback, and gate receipts
> are external freeze evidence; embedding them here would make the source
> record self-referential.

## Purpose

Make the Devkit-owned Product source-acquisition prerequisite and every
persisted or explicit Git SSH command use one absolute OpenSSH executable and
one pinned host-key set selected by the immutable Devkit package. This closes
both the fresh-consumer failure where Git evaluated a bare `ssh` under a
protected empty `PATH` and the Derpinator failure where bootstrap inherited
caller `known_hosts` state and rejected the Product endpoint. It does not by
itself promote a Product runtime.

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
- The same package directly references source-pinned GitHub host keys. Native
  Product paths replace, rather than read or merge, caller `known_hosts`;
  `StrictHostKeyChecking yes` is mandatory for both `github.com` and the
  direct `[ssh.github.com]:443` endpoint.
- Reset/bootstrap preflight rejects an empty, relative, missing, directory, or
  non-executable bound path before proxy, Git, network, common-repository, or
  worktree effects.
- Bootstrap `GIT_SSH_COMMAND`, per-home global `core.sshCommand`, linked
  worktree `core.sshCommand`, and Product-bound explicit Git SSH emitters use the
  same authority and exact source-derived config path.
- Existing identity, managed CONNECT proxy, GitHub port 443, relative metadata,
  bwrap, mount, cleanup, and owned-root contracts remain unchanged.
- A dedicated, revision-neutral Product adapter consumes one parsed authority
  manifest. Its only public operations are exact `prepare --count C --index I`
  and validation-only `exec --count C --index I -- ARGV`; raw `devctl`
  Product construction and lifecycle paths fail before effects.
- The Devkit named consumer-boundary diagnostic executes a composed installed adapter whose
  test-only locator is compiled to a manifest inside the same immutable Nix
  store output. It uses real bwrap, package Git, package OpenSSH, strict
  fixture known-hosts, the managed proxy, and two independent absent
  candidates. Its app-server prerequisite proves the exact pinned Codex PID
  owns the declared listening socket, then performs initialize, ephemeral
  thread creation/readback, and declared governance MCP inventory through the
  managed app-server protocol. It does not claim governed task admission or
  Product promotion.
- SSH private/public and Codex auth handles are declared per consumer, owned
  by that consumer UID, copied only into that consumer's claimed boundary, and
  represented in receipts only by path identity and digest. The real VM uses
  distinct UIDs and independently generated handles for both consumers.
- Bubblewrap receives the private proxy-supervisor request through one
  inherited pipe and materializes it once at a fixed sandbox path with mode
  0600. The helper opens it without following symlinks, checks owner/type and
  exact package identities, unlinks it before use, and exposes no
  caller-selected destination or socket.
- A separate production-package contract proves that the production adapter
  consumes only the root-owned atomic Product runtime selector and its held
  immutable `fleet-runtime-authority/v1` generation, with no integration
  locator, caller authority, independent Product revision, or local bundle
  build fallback. The authoritative WSL derivation owns selector publication;
  `/run/current-system` is same-file verification only.

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
- [x] Export and pass the replacement composed-adapter, twice-absent
      consumer-boundary diagnostic; this remains prerequisite evidence only.
- [x] Prove the built package embeds the exact OpenSSH store executable and its
      closure contains OpenSSH.
- [x] Pin official GitHub raw host keys from `https://api.github.com/meta`
      (`ssh_keys`, retrieved 2026-07-20), independently verify their
      fingerprints against GitHub's fingerprint documentation, and bind the
      immutable file into the package authority.
- [x] Add real OpenSSH matching/mismatching host-key lifecycle coverage,
      including fail-before-checkout cleanup.
- [x] Run focused Go tests, full `go test ./...`, the replacement named check,
      and full `nix flake check --show-trace` on one frozen tree.
- [x] Review and complete the clean full source gate.
- [x] Freeze and preserve candidate `9a5381d` as rejected audit history.
- [x] Close every independent-review finding in the additive successor and
      rerun the expanded sabotage, full Go, all 19 flake, and twice-absent
      diagnostic gates.
- [x] Keep the successor candidate identity external to this plan. The freeze
      receipt records the immutable commit/tree and matching remote branch;
      `master` remains unchanged until Product-owned twice-fresh promotion
      accepts a candidate.

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
- The original Nix test substituted a scripted SSH implementation, synthetic
  readiness, and an exit-zero child. It was useful diagnostic evidence but did
  not exercise the composed Product adapter, real bwrap, real OpenSSH, or the
  app-server protocol. Its earlier green result is therefore historical and
  invalid as lifecycle acceptance.
- A pure build cannot use production GitHub host keys against a local sshd
  without putting the matching private host key in source/store. The
  replacement lifecycle uses an explicitly fixture-only key generated as a
  Nix test artifact and a compile-time immutable fixture locator. Production
  bytes are separately checked against the source-pinned GitHub authority.
- The direct Product origin is `ssh.github.com:443`, while the generated block
  previously matched only `github.com`. The strict block must match both names,
  and the pinned key aliases must include `[ssh.github.com]:443`.

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
- Build one fixture-only diagnostic adapter with a compile-time locator beneath
  its own immutable output. Its manifest names that exact adapter, real
  package Git/OpenSSH/bwrap/Codex artifacts, deterministic local SSH host
  authority, and two absent candidate geometries. Runtime client keys remain
  disposable. This fixture does not establish the installed selector,
  deployment, governed GUI-task admission, or Product promotion.
- Bubblewrap 0.11 `--sync-fd` keeps an FD in bubblewrap but does not expose it
  to the sandbox command. The composed adapter therefore uses `--file` with an
  inherited pipe and a fixed private 0600 sandbox path; the subordinate
  consumes and unlinks that request exactly once. This is an invocation
  transport, not a claimed same-UID security boundary.
- The first real two-UID VM attempt proved the production proxy/SSH/readiness
  path but exposed that `/proc/<parent>/exe` is not readable across the bwrap
  boundary under the target UID. Process ancestry was removed as a false
  authority check; package identity is now validated from the one-shot request
  and linked immutable artifacts.
- Keep production current-system handling as same-file verification only. It
  does not select, rebuild, or reinterpret authority; WSL proves the
  root-owned NixOS generation binding.
- Treat stale, unused bare SSH helper emitters as invariant violations even if
  they are not on the current reset path: migrate or remove their command
  generation so a future promoted path cannot reintroduce ambient authority.
- Extend that same accepted authority to the host-key file rather than create a
  second transport choice. Source-controlled raw keys are authenticated by the
  upstream API provenance and documented fingerprints; builds and runtimes
  never refresh them from the network.

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
- `cli/devctl/internal/productadapter/authority.go`
- `cli/devctl/internal/productadapter/authority_shape_test.go`
- `cli/devctl/internal/productseed/seed.go`
- `cli/devctl/internal/productseed/seed_test.go`
- `cli/devctl/internal/productsession/protocol.go`
- `cli/devctl/cmd/product-adapter-supervisor/main.go`
- `cli/devctl/cmd/product-adapter-supervisor/main_test.go`
- `cli/devctl/cmd/product-adapter-supervisor/ownership_integration_test.go`
- `nix/product-adapter-lifecycle-check.nix`
- `.codex/skills/fleet-devkit-readiness/SKILL.md`
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
nix build .#checks.x86_64-linux.product-consumer-boundary-diagnostic \
  --print-out-paths --show-trace
nix flake check --show-trace
```

Successor-tree verification before freeze:

- Focused adapter, stopped-volume seed, session, supervisor, and packaged
  real-pinned-Codex/same-UID-decoy sabotage tests passed.
- `go test -count=1 ./...` passed.
- `checks.x86_64-linux.product-consumer-boundary-diagnostic` passed and
  constructed and destroyed both consumers from absent state. Its exact output
  path is recorded in the external freeze receipt so this source plan does not
  recursively alter the artifact it names.
- `nix flake check --show-trace` passed all 19 declared checks.
- The candidate `devctl` package's direct references include package Git
  2.50.1 and exactly package OpenSSH 10.0p2. Its final output path is recorded
  outside this source tree so the receipt cannot recursively alter the package
  it identifies.

Package receipt:

```bash
grep -aF '<exact-openssh-store-path>/bin/ssh' \
  '<devctl-package>/kit/bin/devctl'
nix-store -q --references '<devctl-package>'
nix-store -qR '<devctl-package>'
```

## Devkit candidate freeze

The Devkit candidate may be frozen only when the checkout contains exactly the
intended change, every focused/full prerequisite gate is green, the named
packaged consumer-boundary diagnostic is present in the full flake check, and
the package/closure receipts select exactly one OpenSSH store authority. Freeze
means commit and push an immutable candidate branch with matching local/remote
commit and tree. It is not Product acceptance, publication, deployment, or
promotion, and it must not update `origin/master` before the Product-owned
twice-fresh promotion app accepts the complete lifecycle.

## Outcomes and retrospective

Historical result, now explicitly invalid as lifecycle acceptance: the prior
`checks.x86_64-linux.product-fresh-consumer-ssh-authority` output
`/nix/store/nxa4r4qgj0wwb0rrxasan5qw05z8mvgw-devkit-product-fresh-consumer-ssh-authority-check-dev`
was green, but it combined scripted SSH, synthetic readiness, and an exit-zero
child. It did not prove the proposition its old text claimed.

`checks.x86_64-linux.devctl-openssh-executable-authority` is also green for the
production package. It finds the exact `${pkgs.openssh}/bin/ssh` string in the
packaged binary and the exact OpenSSH output in closure metadata.

The historical replacement
`checks.x86_64-linux.product-fresh-consumer-ssh-authority` was terminal green at
`/nix/store/dc7nhskh8261m7m35kzzb97mb25q7rnl-vm-test-run-product-fresh-consumer-ssh-authority`.
It ran both consumers under distinct UIDs from absent boundaries through
package Git, real OpenSSH, strict fixture known-hosts, the managed proxy, real
bubblewrap, pinned Codex app-server initialize/thread/read/MCP, a historical
standalone command diagnostic, and total disposable teardown. The standalone
command observation was never governed task admission and is explicitly
insufficient for publication or promotion. The current diagnostic deliberately
does not repeat or report `command/exec` as a lifecycle or admission boundary.
The associated diagnostic outputs are
`/nix/store/b0w0z89cs3mscm22v76h4cqkkib38dfn-devkit-native-bootstrap-stdio-cleanup-check-dev`,
`/nix/store/8rv0mjp3lr13f4f7wpcxis0d9xrp9q6f-devkit-native-absent-index-construction-check-dev`,
and
`/nix/store/v19jfih1awykyjg5drhzyp0ly0g1cjq1-devkit-devctl-openssh-executable-authority`.

The frozen-tree default and `devkitintegration` Go suites are green, including
the genuine 250 ms app-server deadline plus descendant cleanup regression.
`nix flake check --show-trace` is green across all 15 declared checks. The
source/closure scan proves the adapter engine contains no legacy
`launch.Prepare`, independent Product revision, governance environment
authority branch, local Product flake fallback, global shared secret handles,
or unsupported inherited-FD flag; its exact closure contains package OpenSSH
10.0p2, bubblewrap 0.11.0, and Codex 0.144.0.

Independent source audit remained outstanding at that historical boundary.
The current governed Product-owned twice-fresh promotion app remains solely
responsible for governed turn/task admission, external GUI effects,
distinct-UID production composition, production NixOS
`/etc`/current-generation binding, and promotion. Passing Devkit diagnostics
or freezing a candidate branch cannot substitute for that gate.
