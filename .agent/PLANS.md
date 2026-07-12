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
