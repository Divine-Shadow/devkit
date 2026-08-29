# Devkit ExecPlans

An ExecPlan is a self-contained implementation plan that a future agent or
human can use without relying on chat history. Keep each plan as a living
document: update `Progress`, `Surprises & Discoveries`, `Decision Log`, and
`Outcomes & Retrospective` whenever work advances or the design changes.

Every ExecPlan must explain the user-visible purpose, name the concrete files
and commands involved, and define acceptance with observable evidence. Prefer
small, ordered milestones that can each be verified independently. Do not treat
passing tests as sufficient unless the tests cover the stated objective.

When implementation is underway, continue milestone to milestone without asking
for routine next steps. Escalate only for real blockers or operator decisions.
At completion, the plan must contain the commands run, the result observed, and
any remaining risks.

## Content Placement And Size Contracts

Keep an active ExecPlan focused on the outcome contract, current trajectory and
milestones, decisions that constrain later work, durable pointers to real
deliverables, the current blocker when one exists, and the exact next action.
Do not turn it into an invocation transcript or experiment notebook.

Place supporting material according to its lifecycle:

- For Product governed runs, put each run's hypotheses, parameters,
  invocations, observations, and comparisons in the Product Probe Lab Ledger
  or its governed notebook surface.
- Keep immutable outputs and report cards as evidence; point to them from the
  ExecPlan only when they affect acceptance, a decision, a blocker, or the next
  action.
- Put stable specifications and reusable operating guidance in durable docs.
- Put accepted durable effects, active external-transaction safety, and
  current writer-collision state in the applicable durable record. A task,
  run, tree, station, home, worktree, candidate, or failed attempt is not a
  prerequisite for continuing the objective.

An active ExecPlan has a hard limit of 32,000 Unicode scalar values, not bytes
or tokens. The writer must count the complete proposed document before every
write and count it again before commit and closeout. If the next write would
exceed the limit, stop and file material in the correct ledger, evidence,
durable-doc, or external-transaction record before continuing. Never replace
content with a pointer-only plan, silently truncate it, or write past the
limit.

A task OCVA is a separate, self-contained Objective, Constraints, Verification,
and Authority contract. Its complete core contract must fit within 4,000
Unicode scalar values. Supplemental context may be linked separately, but it
must not redefine or substitute for the OCVA's objective, constraints,
verification, or authority.

Tasks, runs, trees, stations, homes, worktrees, candidates, and partial
computation are replaceable. Keep one active writer per source lane to avoid
collisions, but preserve the business objective and accepted durable effects,
not the lifetime of a particular execution.

## Active ExecPlans

- [GUI Target Configuration Projection](execplans/gui-target-config-projection-20260829.md):
  project one immutable inventory-selected Codex config during the exact GUI
  target launch and admit only a proven suffix-only stale slot-manifest shrink.
- [Management Fleet Exec Handle](execplans/management-fleet-exec-handle.md):
  keep workspace-egress network-isolated while projecting only the
  source-derived Nix-owned handle for the existing exact-station Fleet effect.
- [Immutable Management Runtime Skills](execplans/immutable-management-runtime-skills.md):
  make fresh Management consumers validate and link the manifest-bound
  immutable Management skill package instead of mutable workspace copies.
