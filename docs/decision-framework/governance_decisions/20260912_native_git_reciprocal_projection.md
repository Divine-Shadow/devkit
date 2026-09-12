# Native Git reciprocal projection

Date: 2026-09-12. Domain: existing native Git metadata and mount composition.
Owner: ROOT-assigned Devkit source owner. Trigger: representation tradeoff.
Decision type: correctness maintenance. Pressure: medium. Risk: medium.
Required artifacts: indexed ExecPlan, real Git namespace regressions, full source CI.
Rollback trigger: either view loses reciprocal ownership or sibling isolation.

## problem

Projecting only host agentN at native `/workspaces/dev` preserves the forward
relative `.git` pointer through the existing common bind, but the reverse pointer
still names a host-shaped agentN path. Whole-root relocation does not test this.

## options

1. Native host-shaped alias: changes visible geometry and leaves the reciprocal
   registration reporting an alias instead of the public cwd; outside authority.
2. Move the common repo under each lane: changes durable layout and reset custody.
3. Read-only native reverse-pointer projection: retains host metadata and the
   existing writable common/index/HEAD/refs while adapting only the view-specific
   registration path. Requires validated metadata and fail-closed preparation.

## selection_rationale

Choose 3. Exact host reciprocity and selected lane identity precede projection;
real Git effects from both views can verify the contract. The projection is
source-owned preparation data, not a new authority or registry. Origin admission
and reset ownership remain with their existing source paths.

## safety_checks

Read-only metadata inspection must reject foreign common roots, incorrect
backlinks, symlink/traversal and ambiguous projection bindings. Unrelated
registrations are not inspected or promoted into pruning admission gates. Plan construction and launch
revalidate the selected recipe. Preparation emits one selected state file and
binds it read-only over only the native reverse pointer, after the common bind.
Tests use actual emitted mounts, real bubblewrap and real Git status/index/commit
and ref effects, with host/sibling byte controls. Namespace unavailability is
reported explicitly. Run focused fixtures and full make ci-cheap before review.

## rollback_plan

ROOT reviews the unpublished candidate. If either view loses coherence or
isolation, correct source before publication. After any later authorized release,
ROOT owns reverting the source and canonical convergence; no live backlink repair.

## decision_scope

Existing worktrees metadata validation, runtime plan/launch preparation, native
CLI fixtures and corresponding docs only. No Product, WSL, reset, station,
launcher-binary or deployed-runtime mutation. No publication authority.

## CI fixture consequence

The full declared Go-test environment exposed an existing ETXTBSY race when the
OpenSSH integration fixture overwrote its executable after a refusal while its
ProxyCommand was still exiting. Install the next fixture by atomic rename. This
changes only disposable test installation; it preserves the real mismatched-key
refusal and delayed-pack SSH checks and adds no process wait, retry or waiver.
