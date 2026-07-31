# Package-owned Unix SSH connector for Terraform GUI consumers

## Decision

Keep Ouro Terraform GUI consumers inside the existing `workspace-egress`
lifecycle and configure their managed Git SSH block to reach GitHub through
Devkit's per-consumer Unix proxy socket. Both the pre-worktree bootstrap and
the full runtime preparation path now derive the same package-owned command:

`devctl -p <project> native proxy-connect --socket <managed socket> --target %h:%p`

The full preparation pass continues to use sandbox-visible identity and SSH
config paths. HTTP clients still use the namespace-local
`http://127.0.0.1:18888` bridge; that endpoint is no longer translated into an
`nc` SSH `ProxyCommand`.

## Problem

`PrepareGitBootstrap` installed the managed Unix-socket connector correctly,
but `Prepare` later rewrote the SSH config from `HTTPProxy`. That produced
`nc -X connect -x 127.0.0.1:18888`, bypassed the source-owned connector
lifecycle, and made ordinary Git publication depend on a binary and transport
contract that were not part of the Ouro Terra runtime.

## Options considered

1. Remove `ProxyCommand` and allow direct SSH. Rejected because it breaks the
   mandatory isolated egress contract.
2. Add a host-wide TCP listener or edit the consumer SSH config manually.
   Rejected because either approach broadens authority and drifts from source.
3. Teach the runtime image to provide `nc`. Rejected because it preserves the
   wrong transport and duplicates Devkit's managed connector.
4. Reuse the package-owned Unix connector during full preparation. Selected.

## Selection rationale

The selected path keeps one transport owner, one per-consumer socket, and one
fail-closed source-derived command across bootstrap and normal runtime. It
preserves the private network namespace and the existing HTTP proxy bridge
without adding ambient host reachability.

## Safety checks

- The helper must be an executable beneath the pinned runtime authority root.
- The project identity and managed Unix socket must both be present.
- Generated runtime SSH config must use sandbox-visible identity paths.
- Tests reject the legacy `nc` command and `127.0.0.1:18888` in SSH config.
- Live acceptance requires no host listener on TCP 18888 and a fresh consumer
  that can fetch and publish through ordinary Git.

## Rollback plan

Revert this source change, repin Devkit in the controller Nix closure, deploy
only the controller, and replace only the affected Terraform GUI consumer. Do
not preserve an older generated SSH config as a manual fallback.

## Decision scope

This is narrow maintenance of the existing Devkit workspace-egress and Fleet
GUI lifecycle. It does not authorize broader mounts, host networking, manual
SSH configuration, Darksteel deployment, or Terraform/Tailscale state
mutation. It follows the workspace tradeoff decision framework by preserving
containment and source authority ahead of local convenience.
