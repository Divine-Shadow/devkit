# Managed native Git transport lifetime

## Purpose and contract
An existing native agent must perform ordinary Git operations through its
currently owned managed egress connector after software-only preparation.
Saved home/worktree SSH commands may name an expired previous execution.
No credential hydration, credential payload access, caller connector override,
shared config rewrite, history reset, or unrelated runtime effect is authorized.

## Progress
- Isolated canonical source from published Devkit bd41538; sole writer.
- Bind Git SSH command and variant in the cloned sandbox environment to the
  current immutable runtime plan and package SSH/host-key authority.
- First exact gate retained failed: fixture lacked package connector; real SSH
  canonical StrictHostKeyChecking output is true for configured yes. Retain all
  business/refusal assertions and supply those explicit fixture preconditions.
- Pending owned top-level/nested Git fixtures, credential-access fixture,
  execution/sibling isolation, full x86 checks, and independent review.
- Publication and composed Runtime selection remain separate boundaries.

## Surprises and decisions
Software-only preparation skips the shared SSH config writer along with
credential hydrators. Re-enabling that writer would touch the protected SSH
directory and race active consumers. Use an execution-local command-line
ProxyCommand override instead; preserve the existing identity selection.

## Verification and next action
Exercise ordinary Git through owned Unix connectors with stale saved configs,
use real OpenSSH configuration evaluation to verify option precedence, retain
the full credential directory inotify guard, then realize every applicable
source check/package and collect review before durable WSL dependency pins.
Runtime/Parent owns coordinated closure selection, retained-parent remote sync
and synchronized CI, then an additional accessible-station demonstration.

## Outcomes
Unpublished implementation in progress; no runtime effects or credentials used.
