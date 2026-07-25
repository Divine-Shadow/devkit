# Management Fleet exec handle

## Outcome

The normal Management Codex app-server remains in workspace-egress/v3 with
`--unshare-net`, no station SSH key, and no Windows mount. When the Nix
controller exposes its protected exact-station Fleet handle, Devkit projects
only that mode-0600 Unix socket and marks installed `fleet exec` as handle
required.

## Implementation

- `runtime/plan` admits only the fixed
  `/run/fleet-controller-exec/control.sock` on the exact Management project and
  never binds the raw station identity.
- `runtime/launch` validates the socket, parent ownership/modes, exact
  source=target bind, and read-only projection.
- Remove the prior initial-command `--share-net` exception. Every
  workspace-egress child remains network-isolated.

## Verification

Focused Go tests and the full Devkit flake check must exercise the exact
package planner/launcher, reject socket substitution or unsafe modes, and prove
that no key or shared network enters the sandbox. These checks are supporting
evidence only. Acceptance is the freshly converged Management GUI child
running the unchanged real DrTalos `product-agent cycle-test` through the
handle.

## Failure rule

Repair any defect in canonical Fleet, Devkit, or WSL/Nix source, discard the
failed consumer, and rerun the whole Product-agent lifecycle. Do not expose
Tailnet, copy a key, or add another SSH route.
