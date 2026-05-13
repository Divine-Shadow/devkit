---
name: autonomy-contract
description: Draft, rewrite, or review compact OCVA autonomy contracts using Objective, Constraints, Verification, and Authority for scoped agent work, prompt upgrades, and bounded delegation.
---

# Autonomy Contract

Use this skill when the user asks for an OCVA, an autonomy contract, or a compact Objective/Constraints/Verification/Authority prompt shape. The goal is conversational compression: make scoped agent autonomy explicit without turning the request into a full implementation plan or governed workflow.

## Concept

An autonomy contract grants bounded decision rights over a task. It captures what the agent is trying to accomplish, what must stay true, what evidence proves completion, and what choices the agent may make independently.

OCVA is the compact alias:

```text
Objective
Constraints
Verification
Authority
```

Use the compact form by default. Do not add richer sections such as stop conditions, handoff, blast radius, or ambiguity policy unless the user asks for a fuller autonomy contract or the omission would create a real risk.

## Section Meanings

- **Objective:** The successful outcome or invariant, not a step list. For multi-milestone work, name the tangible user- or operator-visible goal the milestones are building toward, not merely the next milestone or an internal state transition.
- **Constraints:** Binding boundaries, required inputs, non-goals, ownership limits, safety limits, and "do not" rules when needed. Always include using governed agents when possible and keeping fallbacks to the minimal scope required to unblock agents. For complex features, significant refactors, or governed implementation work, always include maintaining an ExecPlan per `.agent/PLANS.md`.
- **Verification:** Evidence required to trust the result: tests, commands, artifacts, schemas, review checks, diffs, or readbacks. Prefer concrete evidence that the target user/operator capability works, such as Playwright completing a meaningful flow, over proof that an internal layer merely exists.
- **Authority:** Decisions the agent may make without asking, including bounded design discretion. Always include using subagents for research. Include forbidden decisions here only when they are important and compact.

## Drafting Rules

1. Preserve user-stated intent and constraints as binding.
2. Mark inferred context as proposed only when it is high-impact or uncertain.
3. Convert recipe-heavy instructions into outcomes, constraints, evidence, and decision rights.
4. When milestones are present, treat them as the execution path toward the larger objective unless the contract is explicitly scoped to one bounded subtask.
5. Prefer objectives like "Playwright can complete a meaningful flow" over objectives like "domain is exposed"; the latter may be a milestone, but the former is the capability.
6. Keep exact sequence only when order matters for side effects, destructive actions, tool calls, schemas, parsers, handoffs, or safety gates.
7. Keep the output compact. The value is scoped autonomy, not section ceremony.

## Output Rules

When the user asks to propose or draft an OCVA, respond with exactly these four headings:

```text
Objective

Constraints

Verification

Authority
```

When reviewing an existing OCVA, focus on whether the objective is outcome-shaped, constraints are binding, verification is evidence-based, and authority is scoped. Prefer a revised OCVA over a long critique unless the user asks for analysis.

When rewriting a prompt for GPT-5.5-style use, remove process scaffolding that does not control real risk, then express the result as OCVA.
