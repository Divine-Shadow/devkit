# Fleet Devkit Readiness

This ExecPlan is a living document. The sections `Progress`, `Surprises & Discoveries`, `Decision Log`, and `Outcomes & Retrospective` must be kept up to date as work proceeds.

This repository does not have a checked-in `.agent/PLANS.md`; maintain this document using the ExecPlan shape from `/home/bayesartre/dev/ouroboros-ide/.agent/PLANS.md` and the repository instructions in `/home/bayesartre/dev/devkit/AGENTS.md`.

## Purpose / Big Picture

Operators need each reachable fleet worker to be ready for two Ouro development agents without hand-running fragile command sequences. After this work, devkit/fleet tooling can launch, warm, verify, and attach two native `dev-all` agents per worker through the canonical `devkit/kit/scripts/devkit` entrypoint.

## Progress

- [x] (2026-05-20 16:20-04:00) Created the `fleet-devkit-readiness` skill under `/home/bayesartre/dev/devkit/.codex/skills/`.
- [x] (2026-05-20 16:20-04:00) Recorded this initial ExecPlan.
- [x] (2026-05-20 17:33-04:00) Inspected existing devctl runtime plan and readiness code and identified existing native readiness surfaces.
- [x] (2026-05-20 17:35-04:00) Used the workspace `fleet exec` route as the fleet command surface while preserving devkit's canonical `kit/scripts/devkit` entrypoint.
- [x] (2026-05-20 17:38-04:00) Built local devctl successfully with `make -C cli/devctl build`.
- [x] (2026-05-20 17:40-04:00) Built SpaceQueen devctl and passed `devkit -p dev-all preflight`.
- [x] (2026-05-20 17:43-04:00) Passed SpaceQueen `runtime-matrix --all --check` when run from the devkit repository root.
- [x] (2026-05-20 17:45-04:00) SpaceQueen two-agent native `up` succeeded for `dev-all`.
- [x] (2026-05-20 17:48-04:00) SpaceQueen plain two-agent native `status` returned `status=running`.
- [x] (2026-05-20 20:23-04:00) Identified readiness timeout cause: devctl launched a fresh `nix develop`/bubblewrap for each readiness check on each agent, sequentially and without progress output.
- [x] (2026-05-20 20:27-04:00) Patched devctl readiness to batch all checks for an agent into one sandbox invocation while preserving per-check JSON details.
- [x] (2026-05-20 20:28-04:00) Built and tested the patched devctl locally.
- [x] (2026-05-20 20:31-04:00) Copied the scoped devctl patch to SpaceQueen, rebuilt there, and ran focused Go tests there.
- [x] (2026-05-20 20:34-04:00) SpaceQueen `status --ready --runtime-only` now returns `status=ready` for both agents.
- [x] (2026-05-20 20:38-04:00) SpaceQueen `ensure-ready --repo-readiness` passed for both agents.
- [x] (2026-05-20 20:42-04:00) Both SpaceQueen agents passed Codex `ok` checks through `zsh -ic` with Codex CLI `0.130.0` and no MCP startup warning banners.
- [x] (2026-05-21 00:56-04:00) Local devkit build passed and focused Go suites passed for native readiness, runtime planning, capacity, and readiness after the fleet rollout.
- [ ] DrTalos remains narrow-cleanup only: controller access works, GitHub host-key trust is repaired, and the remaining bootstrap path must use authorized GitHub access or a verified non-secret repo bundle/source transfer; no private credentials have been copied.

## Surprises & Discoveries

- Observation: The devkit repo enforces a single wrapper path: `devkit/kit/scripts/devkit` must exec `devkit/kit/bin/devctl`, and missing binaries should fail loudly.
  Evidence: `/home/bayesartre/dev/devkit/AGENTS.md`.

- Observation: `runtime-matrix --all --check` must be run from the devkit repo root for the current command shape.
  Evidence: Running `devkit/kit/scripts/devkit -p dev-all runtime-matrix --all --check` from `/home/bayesartre/dev` failed because flake paths resolved as `/home/bayesartre/dev/overlays/...`; running `kit/scripts/devkit -p dev-all runtime-matrix --all --check` from `/home/bayesartre/dev/devkit` passed.

- Observation: SpaceQueen can launch and report two native `dev-all` agents as running, but readiness probes can hang beyond practical fleet command timeouts.
  Evidence: `up --count 2 --skip-ready --format json` returned `status: running`; plain `status --format json` returned `status: running`; `status --ready` timed out after 300 seconds; `ensure-ready --repo-readiness` timed out after 900 seconds.

- Observation: The readiness hang was not a failing tool check; it was repeated Nix startup overhead with no progress output.
  Evidence: individual runtime checks passed on both SpaceQueen agents, while process inspection showed `devctl status --ready` inside repeated `nix develop`/bubblewrap invocations.

- Observation: Batching readiness checks resolves the runtime-only timeout and makes full repo readiness practical.
  Evidence: after the patch, `status --ready --runtime-only --format json` returned `status: ready`, `runtime_ready: 2`, and `capacity_available: 2`; `ensure-ready --repo-readiness --format json` returned `status: ready`, `repo_ready: 2`, and all listed repo checks `ok: true`.

## Decision Log

- Decision: Keep fleet readiness commands inside the canonical devkit path.
  Rationale: Hidden shell fallbacks caused confusion in prior incidents; devkit's own rules require one supported path.
  Date/Author: 2026-05-20 / Codex

- Decision: Treat Derpinator pause semantics as part of readiness orchestration.
  Rationale: Broad readiness commands must not wake Derpinator while it is reserved or paused.
  Date/Author: 2026-05-20 / Codex

- Decision: Pause this ExecPlan before Codex `ok` checks.
  Rationale: The required acceptance sequence depends on readiness completing. Running `ok` checks after a hung readiness gate would produce ambiguous evidence and could hide the readiness blocker.
  Date/Author: 2026-05-20 / Codex

- Decision: Batch readiness checks per agent in devctl instead of increasing fleet SSH timeouts.
  Rationale: Higher timeouts would mask the real cost and still provide no per-check boundary. One sandbox invocation per agent preserves readiness semantics while removing repeated `nix develop` startup cost.
  Date/Author: 2026-05-20 / Codex

## Outcomes & Retrospective

The fleet route and canonical devkit command path are proven through build, preflight, runtime matrix, two-agent `up`, ready status, full repo readiness, and Codex `ok` checks on SpaceQueen. The timeout blocker was remediated by batching readiness checks per agent. SpaceQueen is now the canary-ready worker for the two-agent Ouro dev topology.

## Context and Orientation

The devkit repo is `/home/bayesartre/dev/devkit`. The canonical command is `devkit/kit/scripts/devkit`. That script calls the compiled CLI at `devkit/kit/bin/devctl`. The default Ouro development profile is `dev-all`, and the expected two-agent command shape includes `--repo ouroboros-ide --count 2 --flake ./overlays/dev-all#default`.

The fleet access layer will decide how to reach a worker. Devkit should focus on what to do once a command is running on the target worker: build devctl, run preflight, launch agents, run status, run readiness, and attach tmux or equivalent shells.

## Plan of Work

First, inspect the current devctl packages that implement runtime plans, readiness checks, tmux, and native-agent handling. Record the exact files in this ExecPlan before editing.

Second, design a command or integration point that can run the standard two-agent readiness sequence without bypassing `devkit/kit/scripts/devkit`. Prefer extending devctl over adding shell logic.

Third, add tests around command rendering and readiness parsing. The tests should catch missing `SBT_OPTS`, incorrect count handling, and routes that would clobber native-agent homes.

Fourth, run a canary on one worker before broad rollout. SpaceQueen is the preferred canary unless the operator selects another.

## Concrete Steps

From `/home/bayesartre/dev/devkit`, start with:

    sed -n '1,180p' AGENTS.md
    rg -n "ensure-ready|runtime-matrix|native|tmux|SBT_OPTS" cli kit

Build after edits:

    make -C cli/devctl build

Run canary commands from the worker's `/home/bayesartre/dev` root:

    devkit/kit/scripts/devkit -p dev-all preflight
    devkit/kit/scripts/devkit -p dev-all runtime-matrix --all --check
    devkit/kit/scripts/devkit -p dev-all up --repo ouroboros-ide --count 2 --flake ./overlays/dev-all#default --skip-ready --format json
    devkit/kit/scripts/devkit -p dev-all status --repo ouroboros-ide --count 2 --flake ./overlays/dev-all#default --ready --format json
    devkit/kit/scripts/devkit -p dev-all ensure-ready --repo ouroboros-ide --count 2 --flake ./overlays/dev-all#default --repo-readiness --format json

## Validation and Acceptance

The work is accepted when `make -C cli/devctl build` succeeds, the canary worker reaches two-agent `up` and ready status, `ensure-ready --repo-readiness` passes, and both agents return exactly `ok` from `zsh -ic 'codex exec "reply with exactly '\''ok'\''"'` without MCP warning banners.

Current validation evidence from 2026-05-20:

    make -C /home/bayesartre/dev/devkit/cli/devctl build
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o ../../kit/bin/devctl ./

    fleet exec spacequeen -- 'cd /home/bayesartre/dev && make -C devkit/cli/devctl build && devkit/kit/scripts/devkit -p dev-all preflight'
    [preflight] nix flakes: OK
    [preflight] bubblewrap: OK
    [preflight] brokered Docker upstream: OK
    [preflight] tmux: OK
    [preflight] ~/.codex: OK (auth.json present)
    [preflight] SSH key: OK ( id_ed25519 )

    fleet exec spacequeen -- 'cd /home/bayesartre/dev/devkit && kit/scripts/devkit -p dev-all runtime-matrix --all --check'
    runtime-matrix: OK

    fleet exec spacequeen -- 'cd /home/bayesartre/dev/devkit && kit/scripts/devkit -p dev-all up --repo ouroboros-ide --count 2 --flake ./overlays/dev-all#default --skip-ready --format json'
    "command": "up"
    "runtime": "native"
    "status": "running"
    "count": 2

    fleet exec spacequeen -- 'cd /home/bayesartre/dev/devkit && kit/scripts/devkit -p dev-all status --repo ouroboros-ide --count 2 --flake ./overlays/dev-all#default --format json'
    "command": "status"
    "runtime": "native"
    "status": "running"
    "count": 2

Blocking evidence:

    fleet exec --timeout 300 spacequeen -- 'cd /home/bayesartre/dev/devkit && kit/scripts/devkit -p dev-all status --repo ouroboros-ide --count 2 --flake ./overlays/dev-all#default --ready --format json'
    timeout after 300s

    fleet exec --timeout 900 spacequeen -- 'cd /home/bayesartre/dev/devkit && kit/scripts/devkit -p dev-all ensure-ready --repo ouroboros-ide --count 2 --flake ./overlays/dev-all#default --repo-readiness --format json'
    timeout after 900s

Remediation evidence:

    make -C /home/bayesartre/dev/devkit/cli/devctl build
    go test ./internal/commands/nativecmd ./internal/runtime/capacity ./internal/runtime/readiness

    SpaceQueen:
    cd /home/bayesartre/dev/devkit/cli/devctl
    make build
    go test ./internal/commands/nativecmd ./internal/runtime/capacity ./internal/runtime/readiness

    SpaceQueen:
    kit/scripts/devkit -p dev-all status --repo ouroboros-ide --count 2 --flake ./overlays/dev-all#default --ready --runtime-only --format json
    status: ready
    runtime_ready: 2
    capacity_available: 2

    SpaceQueen:
    kit/scripts/devkit -p dev-all ensure-ready --repo ouroboros-ide --count 2 --flake ./overlays/dev-all#default --repo-readiness --format json
    status: ready
    runtime_ready: 2
    repo_ready: 2
    capacity_available: 2

    SpaceQueen agent 1:
    kit/scripts/devkit -p dev-all exec 1 --repo ouroboros-ide -- zsh -ic 'codex --version; codex exec "reply with exactly ok"'
    codex-cli 0.130.0
    assistant answer: ok

    SpaceQueen agent 2:
    kit/scripts/devkit -p dev-all exec 2 --repo ouroboros-ide -- zsh -ic 'codex --version; codex exec "reply with exactly ok"'
    codex-cli 0.130.0
    assistant answer: ok

Fleet rollout evidence from 2026-05-21:

    Matrix artifact:
    /home/bayesartre/dev/docs/workflows/fleet-devkit-rollout-current/matrix.tsv

    Green workers:
    spacequeen, davidlich, gilliansandwich, darksteel, meowlnir, shadow

    For each green worker:
    - controller access succeeded through the fleet/red-pill route model
    - patched devctl built successfully
    - focused Go tests passed for native readiness and runtime plan paths
    - two native dev-all agents reached runtime readiness
    - ensure-ready --repo-readiness returned status=ready with repo_ready=2
    - both agents ran `codex exec "reply with exactly ok"`
    - Codex CLI reported 0.130.0
    - no MCP startup warning banner appeared in the Codex probe output

    Notable per-worker remediation:
    - gilliansandwich required copying repo-readiness fixes into native agent worktrees after the root checkout fix.
    - darksteel was on stale devkit master; its partial rollout copy was moved under .devkit-rollout-backups, devkit was switched to origin/codex/upstream-devkit-nixos-fixes, and the root ouroboros-ide checkout was normalized from branch agent1 to main so devkit could create agent1/agent2 worktrees.
    - meowlnir and shadow were missing /home/bayesartre/dev/devkit and /home/bayesartre/dev/ouroboros-ide; both repos were cloned, patched, built, and warmed successfully.

Remaining blocker:

    drtalos:
    - fleet controller access works.
    - GitHub host key trust was repaired non-destructively in ~/.ssh/known_hosts.
    - repo clone still fails with `git@github.com: Permission denied (publickey)`.
    - No private key material was copied.
    - Next remediation is either install authorized GitHub repo access on DrTalos or stream verified repo bundles/source from an already-authorized controller.
    - If using bundle/source transfer, inspect any existing `/home/bayesartre/dev/devkit` and `/home/bayesartre/dev/ouroboros-ide` paths first, move partial failed checkouts aside under a timestamped backup directory, and do not overwrite dirty or valid repos.

## Idempotence and Recovery

Readiness commands must be safe to retry. They may update generated runtime state but must not delete `.codex`, `.ssh`, native-agent homes, rollout logs, or shell snapshots. Any command that would restart or recreate active agents must print the impact and require explicit operator approval unless the stack is already unusable.

## Artifacts and Notes

The initial skill artifact is `/home/bayesartre/dev/devkit/.codex/skills/fleet-devkit-readiness/SKILL.md`.

## Interfaces and Dependencies

This plan depends on the fleet access inventory for station routing, but devkit should not retrieve AWS secrets or own cross-controller SSH key installation. It should expose a predictable readiness interface that the fleet CLI can call once a worker shell is available.
