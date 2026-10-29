# Bind the existing named preparation to the current writer

## problem

The selected package contains the frontend tools and an immutable
`frontend-warm` repo check. Existing readiness constructs a lane boundary;
its command override takes shell text and does not select that named check
for a registered nested writer in an already accepted GUI consumer.

## options

Copy the recipe into a caller command; alter lane/root selection; or let
existing readiness select one immutable named repo check in its current GUI
worktree without creating or preparing another boundary.

## selection_rationale

Use the current-worktree form of existing readiness. It loads only the
executable-owned overlay, derives the consumer root/home from the fixed GUI
projection, and verifies Git registration in that lane before executing one
named command. This repairs Devkit's producer/consumer join; it adds no Fleet
surface, package manager, Product semantics or host capability.

## safety_checks

Reject root/environment/command overrides, non-GUI or non-v4 consumers,
foreign/symlinked/unregistered worktrees and duplicate/unknown checks. Do not
prepare, seed, reconstruct, start listeners, restart, change PATH, or run
other repo checks. Preserve inherited containment and all external gates.
Test the nested Git binding, exact command/cwd and refusals; run normal full
CI and immutable packaging, independent review and publication.

## rollback_plan

Use a reviewed source revert and normal publication/composition. Runtime
retains actual preparation/adoption and receiving; no worker is mutated by
source qualification. The prior failed npm install and F0 remain terminal.

## decision_scope

Root's bounded Source supplier task for writer-frontend-continuation.
Runtime retains environment effects and the frontend parent retains business
semantics. Existing SourceR3/SBT, histories, locks and package bytes are protected.
