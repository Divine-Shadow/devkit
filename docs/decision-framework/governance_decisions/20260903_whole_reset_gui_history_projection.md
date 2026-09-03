# Whole-reset GUI history projection validation

Framework: [Tradeoff Decision Framework](../tradeoff_decision_framework.md)

Decision type: tooling remediation at the disposable-execution custody
boundary.

Pressure: high. Risk: medium.

## problem

A source-derived two-slot `dev-all reset` on Derpinator failed before applying
its reset plan because lane 2's SQLite history referred to the same Codex home
through the GUI isolation projection
`/workspaces/dev/.devhome-agent2/.codex`. Selected-slot history capture already
recognizes that projection, but whole-prefix capture omits the workspace
geometry and rejects the reference as an escape.

The failed operation stopped the runtime and captured lane 1, but it did not
apply the destructive reset plan. The defect is therefore in the generic
Devkit capture boundary, not in Derpinator's data. Repeating resets on other
stations would only reproduce the same source defect.

## options

1. Pass the existing `SnapshotOptions.WorkspaceRoot` during whole reset. This
   recognizes the projected path but also relocates custody beneath the lane
   workspace that whole reset disposes; protecting it makes reset planning
   reject the overlap, while not protecting it deletes the new snapshot.
2. Infer a projected root inside the history package from every capture's host
   worktree. This avoids a new option but makes projection acceptance implicit
   for callers that did not declare a GUI projection.
3. Carry a separate source-derived workspace projection root. Continue using
   `WorkspaceRoot` only to opt selected-slot capture into lane-local custody;
   use the new geometry only to validate the additional GUI-visible Codex-home
   path.

## selection_rationale

Choose option 3. It preserves the existing global whole-reset custody contract
and makes the extra accepted path explicit. Both workspace values must equal
the exact parent of the source-derived host worktree, and the host home must be
contained by that root, so the new path cannot be supplied as an arbitrary
escape hatch. The native reset wrapper derives that explicit value centrally
from its already source-derived host worktree, avoiding duplicated identity
state across whole-reset, selected-slot, and manifest-shrink constructors.

This ordering favors correctness and explicit contracts first, then a direct
regression that proves storage and validation independently. It retains the
existing atomic custody and reset-plan boundaries, leaves the failure visible,
and avoids duplicate station failures. The added field is slightly more code
than implicit derivation, but its meaning remains inspectable to future
callers.

## safety_checks

- A whole-reset regression uses a lane-2 SQLite reference under
  `/workspaces/dev/.devhome-agent2/.codex`, requires capture to succeed in the
  global state-root custody location, and requires no lane-local custody root
  to be created.
- Existing selected-slot projection, hostile rollout-path rejection, missing
  history, malformed-history quarantine, SQLite integrity, atomic publication,
  and reset ordering tests remain mandatory.
- Projection geometry must be absolute and canonical, own the exact host
  worktree parent, and contain the host home before it contributes an accepted
  rollout root.
- The complete Devkit Go, vet, formatting, flake-evaluation, and packaged
  runtime gates must pass before publication. Fleet acceptance requires a
  source pin, centralized deployment, a fresh Derpinator cold replacement, and
  a fresh typed two-slot reset receipt before either lane is reconstructed.

## rollback_plan

Revert the source commit before repinning Devkit if any test shows projection
geometry accepting a path outside the exact source-derived home, custody moving
inside a whole-reset target, or reset ordering changing. If discovered only
after deployment, repin the prior Devkit revision and converge through the
centralized WSL-Nix path; preserve any already completed immutable history
generation.

## decision_scope

This decision covers only Devkit GUI-history path validation for selected-slot,
whole-prefix, and manifest-shrink capture. It does not authorize history import,
station-local repair, manual closure copying, reset retries before deployment,
Product publication, or reconstruction of a lane whose station reset has not
completed successfully.
