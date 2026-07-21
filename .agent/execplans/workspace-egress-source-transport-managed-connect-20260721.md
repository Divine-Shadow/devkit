# Workspace-egress source transport managed CONNECT

This bounded ExecPlan records the architectural decision for the Management
workspace-egress source-acquisition repair. It is not Product, cloud,
Terraform, production, or station-local configuration work.

## Evidence and decision

The exact Fleet A3 candidate launched its pinned source-transport server inside
`devkit/workspace-egress/v3`. The sandbox correctly retained `--unshare-net`
and the existing package-owned HTTP CONNECT bridge at `127.0.0.1:18888`.
One behavioral canary reached the package transport and failed closed with
`managed CONNECT returned 502 Bad Gateway`; the direct transport server could
not cross the unshared network namespace.

The rejected alternatives were:

- sharing the host network, because it grants unrelated raw-network authority;
- a privileged low-port loopback bridge, because UID 1000 could not bind port
  443 in the actual user/network namespace even with only
  `CAP_NET_BIND_SERVICE`;
- namespace-root, nested-bwrap, nftables, or sysctl plumbing, because the
  unpublished Fleet candidate is disposable and those mechanisms add a second
  privilege boundary to preserve it;
- caller proxy flags or environment, because hostile caller state must not be
  authority and the exact gate is exercised under `env -i`.

The selected contract changes the normal compiled source-transport server to
chain every allowlisted CONNECT through the package-fixed existing bridge URL
`http://127.0.0.1:18888`. There is no direct fallback and no public option or
environment variable for replacing that upstream. The source transport keeps
its own exact `ssh.github.com:443` allowlist; the outer workspace-egress proxy
independently applies the runtime allowlist before any network effect.

Because this is a behavioral interface change, the public source-transport
schema advances from v3 to v4 and its network contract from v1 to v2. The
Git-SSH v2 identity/config/known-hosts contract is unchanged. The unpublished
Fleet A3 candidate must be reconstructed to pin v4 before any Fleet
publication.

## Acceptance

- Focused Go tests prove the upstream is package-fixed and ignores hostile
  proxy environment, while the proxy implementation preserves immediate
  tunnel bytes and fails closed on rejection or absence.
- The Nix interface proves the v4/v2 schema, fixed bridge URL, exact target and
  allowlist, and `directFallback = false`.
- The NixOS Git-SSH lifecycle supplies a real CONNECT bridge, performs two
  hostile-environment clones, and accepts only after proxy, transport, SSH,
  mounts, sockets, and mutable key material have been strictly reaped.
- Full flake gates must pass before publication.
- A reconstructed Fleet A3 package must pass nominal and wrong-lock sabotage
  from two freshly converged workspace-egress consumers. The live proof must
  also show exact binary identity, canonical 0600 identity projection, hidden
  Windows mounts, zero caller Git/SSH/PATH authority, and no new bridge/socket
  process after teardown.

## Rollback and triggers

Rollback is a normal source revert and runtime/Fleet reconvergence; no host
state is mutated by this design. Stop publication if the fixed bridge can be
overridden, a direct connection occurs when the bridge is absent, a target
outside the two allowlists is reached, identity contents enter evidence, the
wrong-lock run leaves residue, or a fresh consumer leaves a new bridge/socket
process after teardown.

## Progress

- [x] Record the single bounded denial canary.
- [x] Reject privileged namespace plumbing after direct capability tests.
- [x] Implement and test the source-transport v4 / network v2 contract.
- [x] Pass focused Go, package, lifecycle, and full flake gates.
- [ ] Publish Devkit through its normal owning route.
- [ ] Reconstruct, gate, and publish the final Fleet A3 pin.
- [ ] Centrally converge and prove two fresh consumers.

## Verification receipts

- `go test -count=1 ./cmd/source-transport ./internal/runtime/egressproxy`
  passed.
- `go test -count=1 ./...` passed every package under `cli/devctl`.
- `nix build --no-link --print-out-paths .#source-transport
  .#checks.x86_64-linux.source-transport-interface` produced
  `/nix/store/m8ps35lh4lc9g0zz51k1kc1hjbyn9f4w-devkit-source-transport-dev`
  and
  `/nix/store/g1kai1c2lnbzd17awmgx9ixs43gz2ahn-devkit-source-transport-interface`.
- `nix build --no-link --print-out-paths
  .#checks.x86_64-linux.source-transport-git-ssh-lifecycle` produced
  `/nix/store/p14n1756yqh00fzm1s36vrv1gvwnjj99-vm-test-run-devkit-source-transport-git-ssh-lifecycle`.
- `nix flake check --show-trace` passed all 19 x86_64-linux checks. A
  second cached invocation returned exit status 0 after the first invocation's
  controller wait was bounded; neither invocation changed source.
