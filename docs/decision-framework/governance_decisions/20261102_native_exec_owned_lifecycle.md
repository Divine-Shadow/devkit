# Ordinary native exec owning lifecycle

## problem

Source03 used ordinary devctl exec, whose selected source invokes cmd.Run directly.
The local drivers timed out without an exported remote cancel or cleanup result.
Accepted Devkit fce/af5 already supplies a managed process-group runner; native
execution bypasses it. Frozen source receipt 111 and passive client outcome 115
are the attributable evidence. This is normal canonical supplier repair under
Root's existing Source1 commission, not live process recovery.

## options

1. Reuse managed supervision for prepared native commands and an optional native
   deadline inside the existing exec command.
2. Add a remote job daemon, external cancellation endpoint and persistence model.
3. Continue direct execution and require callers to inspect or signal host PIDs.

## selection_rationale

Choose option 1. It stays inside Devkit's existing execution owner and capability
boundary, preserves the validated bubblewrap command and uses the existing
lifecycle implementation. The optional native budget lets a new invocation own
its deadline before an outer client wait expires. It does not create post-hoc
control of existing Source03 or certify cleanup after an unobserved client death.

Correctness requires retaining cwd, environment, streams, payload exit and the
managed app-server exception. Verifiability requires real group cleanup and
sibling controls. Safety keeps signals inside freshly owned command groups.
Epistemic integrity requires a distinct cleanup failure and truthful scope.
Delivery speed follows these properties. Options 2 and 3 are not selected.

This primitive owns one process group. Cooperative governed wrappers can forward
termination and reap a separate cold group; a wrapper killed while waiting for a
TERM-resistant, redirected cold group cannot supply that proof. An explicit
negative control preserves this boundary. A deadline result is not a general
descendant-reaping receipt or a terminal receipt for the original Source03.

## safety_checks

Run a failing redirected-descendant regression before the code change, then
cancellation/deadline and unaffected sibling tests. Preserve source-selected
containment and systemd-owned app-server lifecycle; reject its command timeout.
Verify actual exit/stdio and no cleanup-success classification on infrastructure
failure. Run normal independent review, full applicable Go/native/Nix source
gates and publication. No Main03 PID, client handle, broker, provider or profile
effect is authorized by these tests.

## rollback_plan

Before publication retain the candidate and exact failing evidence. If the new
qualified behavior regresses ordinary execution or shared-service isolation,
publish a reviewed forward correction/revert through the same gates and declared
Runtime adoption. Never revert by mutable runtime edits, copied binaries or
erasing operation/history evidence.

## decision_scope

Maintenance of the existing Devkit native execution lifecycle and internal
execx command adapter, plus tests/documentation. The optional deadline is an
additive option on the existing command, not a new host effect or remote API.
Main retains its Product work; Runtime retains deployment and actual receiving.
Source03 legacy disposition remains unearned. Apply the
[Tradeoff Decision Framework](../tradeoff_decision_framework.md).
