---
name: fleet-devkit-readiness
description: Extend devkit/fleet operations so each reachable worker can launch, warm, verify, and attach two Ouro dev agents through the canonical devkit wrapper path.
---

# Fleet Devkit Readiness

Use this skill when the task is to make fleet workers ready for Ouro development through `devkit`. The scope is runtime orchestration and readiness checks, not AWS secret provisioning or general fleet SSH inventory design.

## Autonomy Contract

### Objective

Teach devkit/fleet tooling to launch, warm, verify, and attach two native Ouro dev agents per active worker through the canonical `devkit/kit/scripts/devkit` path.

### Constraints

- Maintain the devkit ExecPlan at `logs/plans/fleet-devkit-readiness-20260520.md`.
- Do not use governed agents for implementation unless the operator explicitly re-authorizes them.
- Preserve devkit's single-entrypoint rule: supported automation must go through `devkit/kit/scripts/devkit`, which execs `devkit/kit/bin/devctl`.
- Do not add fallback scripts that silently bypass the compiled devctl binary.
- Do not restart, recreate, or scale down running dev agents unless the operator has explicitly approved disruption or the stack is unusable.
- Do not clobber `.codex`, `.ssh`, native-agent homes, rollout files, logs, shell snapshots, or dirty repo state on any worker.
- Treat Derpinator as paused unless the operator explicitly authorizes resume.

### Verification

- `make -C devkit/cli/devctl build` succeeds.
- `devkit/kit/scripts/devkit -p dev-all preflight` succeeds on the canary worker.
- `devkit/kit/scripts/devkit -p dev-all runtime-matrix --all --check` succeeds where applicable.
- A two-agent canary reaches `up`, `status --ready`, and `ensure-ready --repo-readiness` through `devkit/kit/scripts/devkit`.
- Both canary agents pass `zsh -ic 'codex exec "reply with exactly '\''ok'\''"'` with no MCP startup warning banners.
- `fleet tmux` or the devkit attach path lands in the intended per-agent home and worktree with correct `PWD`, `HOME`, `CODEX_HOME`, `CODEX_ROLLOUT_DIR`, `XDG_CACHE_HOME`, and `XDG_CONFIG_HOME`.

### Authority

- You may extend devctl commands, readiness JSON, and tmux attach behavior when the ExecPlan records the user-visible command and evidence.
- You may add tests around runtime plans, command rendering, route selection, and readiness parsing.
- You may coordinate with `fleet-access-control-plane` inventory outputs, but do not make devkit own AWS secret retrieval or Terraform mutation.
- You may use read-only subagents for codebase research and risk review.

## Required Starting Points

- Read `/home/bayesartre/dev/devkit/AGENTS.md`.
- Read `/home/bayesartre/dev/.codex/skills/devkit-management/SKILL.md`.
- Inspect existing `devkit/kit/scripts/devkit`, `devkit/cli/devctl`, and `devkit/kit/examples/orchestration-ouro8-devall.yaml` before designing new commands.
