# Devkit ExecPlans

An ExecPlan is a self-contained implementation plan that a future agent or
human can use without relying on chat history. Keep each plan as a living
document: update `Progress`, `Surprises & Discoveries`, `Decision Log`, and
`Outcomes & Retrospective` whenever work advances or the design changes.
Active plans must identify the current source-controlled path. Historical plans
remain audit evidence only; mark them superseded when their procedure no longer
governs current work, and never let an older plan override current repository
guidance.

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
- Put accepted durable effects and audit history in their durable evidence
  records. Use a transfer or lease record only as an active collision-control
  lease for one writer. Tasks, runs, trees, stations, homes, worktrees,
  candidates, and partial computation are replaceable; they do not acquire
  continuity custody over the business objective.

An active ExecPlan has a hard limit of 32,000 Unicode scalar values, not bytes
or tokens. The writer must count the complete proposed document before every
write and count it again before commit and closeout. If the next write would
exceed the limit, stop and file material in the correct ledger, evidence,
durable-doc, or transfer/lease surface before continuing. Never replace content
with a pointer-only plan, silently truncate it, or write past the limit.

A task OCVA is a separate, self-contained Objective, Constraints, Verification,
and Authority contract. Its complete core contract must fit within 4,000
Unicode scalar values. Supplemental context may be linked separately, but it
must not redefine or substitute for the OCVA's objective, constraints,
verification, or authority.

## Active ExecPlans

- [Runtime Authority Projection And Selector](execplans/runtime-authority-projection-selector-20260722.md): derive the Product runtime projection once, share it between adapter and final bundle, and install only a verified root-owned final selector.
