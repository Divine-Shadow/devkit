# Devkit ExecPlans

Active bounded custody refinement: [Unavailable archived references](execplans/unavailable-archived-reference-custody-20261015.md).

Active custody evidence repair: [Content-free GUI-history refusal evidence](execplans/missing-gui-history-observations-20261015.md).

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

## Completed ExecPlans

- [Product Retained-Reader Manifest Consumption](execplans/product-retained-reader-manifest-20260907.md): completed 2026-09-07; all 12 checks, publication, integrated WSL gates, deployment, fresh manifest consumption and six actual reads passed their contracts. Missing indexes remain unavailable history.

## Active ExecPlans
- [Optional finite-retirement profile projection](execplans/optional-retirement-profile-projection-20261015.md): repair actual strict-consumer compatibility through typed immutable identity validation; normal source qualification/publication precedes Runtime effects.
- [Retired station missing-PID reconciliation](execplans/missing-pid-station-reconciliation-20261013.md): bounded broker lifecycle repair; independent review and native CI/publication required, runtime effects held.
- [Derpinator preserving native history](execplans/derpinator-preserving-history-20261007.md): exact cold-goal and rollout projection during normal preparation; source review/gates before parent-owned runtime judgment.
- [Standard native fleet software-only preparation](execplans/fleet-software-only-native-20261007.md): source-class admission, immutable execution binding, no credential hydration; runtime activation remains held.


- [Ordinary controller recovery](execplans/ordinary-controller-recovery-20261004.md): restore the accepted launcher and communication frontier through reviewed bounded source and separately held Nix recovery.

- [Codex 0.154.0 runtime](execplans/codex-0.154.0-runtime-20260922.md): advance the immutable Codex bundle and every ordinary overlay runtime identity without changing lifecycle semantics.

- [Native selected-lane Git reciprocal projection](execplans/native-git-reciprocal-projection-20260912.md): validate and project the selected reverse pointer without changing host or sibling Git custody; unpublished review candidate.

- [Retire the EMDR appliance dependency](execplans/emdr-appliance-retirement-20260908.md): remove the retired preparation capability from ordinary Management launch; preserve generic controller validation and user workspace data.

- [Historical-root Consumer Alias Manifest Shrink](execplans/native-historical-root-consumer-alias-20260903.md):
  admit only the exact manifest-derived sandbox spelling of an otherwise exact
  historical-root linked-worktree registration while preserving every custody
  refusal and the read-only shared-root boundary.
- [Whole-reset GUI History Projection Repair](execplans/whole-reset-gui-history-projection-20260903.md):
  preserve GUI-projected rollout references during whole-prefix reset without
  moving custody into the reset-owned lane.
- [Management Controller Profile v8](execplans/management-controller-profile-v8-20260831.md):
  consume the one strict v8 controller manifest and live identity, including
  the isolated typed Product station-reset capability, without a compatibility
  projection.
- [GUI Target Configuration Projection](execplans/gui-target-config-projection-20260829.md):
  project one immutable inventory-selected Codex config during the exact GUI
  target launch and admit only a proven suffix-only stale slot-manifest shrink.
- [Management Fleet Exec Handle](execplans/management-fleet-exec-handle.md):
  keep workspace-egress network-isolated while projecting only the
  source-derived Nix-owned handle for the existing exact-station Fleet effect.
- [Immutable Management Runtime Skills](execplans/immutable-management-runtime-skills.md):
  make fresh Management consumers validate and link the manifest-bound
  immutable Management skill package instead of mutable workspace copies.

- [Codex 0.159.3 and GPT-6.1 Sol runtime](execplans/codex-0.159.3-sol-runtime-20261001.md): isolated source preparation; activation remains parent-coordinated.

- [Codex 0.160.0 / Sol 6.1 package](execplans/codex-0.160.0-sol-runtime-20261003.md): existing commissioned server update, isolated from broker/observer work.

- [Fixed Product-agent reconstruction preservation](execplans/product-agent-fixed-slot-custody-20261005.md): repair the existing cold-history provider and exact-slot lifecycle before independently judged Agent1 reconstruction.

- [Davidlich retained software-only start](execplans/davidlich-software-only-retained-start-20261007.md): canonical maintenance preparation; runtime effects held.

- [Davidlich explicit legacy broker retirement](execplans/davidlich-explicit-legacy-broker-stop-20261007.md): existing stop/start compatibility maintenance; runtime effects held.

- [Managed native Git transport lifetime](execplans/managed-native-git-egress-lifetime-20261007.md): current owned execution connector without credential hydration or shared config mutation.

- [Standard native GitHub SSH projection](execplans/managed-native-git-home-projection-20261008.md): immutable trust/current-home managed policy without shared SSH mutation; coordinated adoption pending.

- [Terraform Agent1 source preparation](execplans/terraform-agent1-source-preparation-20261014.md): exact existing startup acquisition/geometry compatibility; Root 2831 scope, source review/native gates before Runtime adoption.


Runtime-only TF broker follow-on: Root2857 authenticated source judgment limits
this repair to the Terraform overlay request and existing source plan. Preserve
owner2 and all guards; no SAM grant or wider Docker-workload acceptance follows.
See `execplans/terraform-agent1-source-preparation-20261014.md`. Native checks,
old3 refusal/new2 actual-selected-owner consequence proof, independent review,
publication and canonical Runtime supply remain required.

Root2874 bounded TF1 tools phase: full provider preparation remains the default. The typed `tf1-tools-provider-deferred` startup retains source-selected Git/Codex/isolation/broker controls, defers AWS/provider projection, and reports provider readiness unassessed. Actual immutable qualification, independent review, publication and Runtime tools startup remain separate acceptance boundaries.

- TF1 required-bind materialization repair (Root2918): extend existing launch projector only after exact native-plan geometry validation; retain Required bindings and full defaults. See [execution plan](execplans/terraform-agent1-source-preparation-20261014.md).

- TF1 immutable tool/profile readiness (Root2941): preserve generic GUI0.160, enter only the fixed Terraform child for pure tool identity checks, and compare the existing declared Terraform profile source/projected bytes. Four existing Devkit leaves only; [plan](execplans/terraform-agent1-source-preparation-20261014.md) records actual failure, old/new consequences and separately owned Runtime acceptance.

- TF1 native ownership refusal observation (Root2956): retain every d819 owner/hash/mode/byte guard and nonzero result; emit only bounded public metadata from the existing failed checker. Actual TF namespace ownership remains a receiving observation; no UID65534 allowance or installed-readiness claim. See the existing TF1 execution plan.

- TF1 namespace ownership correction: actual checker evidence now binds immutable source/store ownership in the selected namespace. Anchor only the foreign immutable-source alternative to the fixed read-only store descriptor; retain mutable EUID/0600 and every independent source check. See [plan](execplans/terraform-agent1-source-preparation-20261014.md) and [decision](../docs/decision-framework/governance_decisions/20261015_terraform_namespace_immutable_store_owner.md). Source qualification and actual installed acceptance remain separate obligations.
