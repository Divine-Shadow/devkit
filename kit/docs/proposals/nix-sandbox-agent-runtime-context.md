# Nix-Native Runtime Context

## Current Decision

The target architecture is Nix-first. Devkit should be refactored toward durable
host-managed agent sandboxes, not toward a long-term mixed Compose/Nix framework.
If preserving Compose support forces the wrong structure, the native runtime should
win and Compose should be treated as legacy or break-glass code during migration.

The architectural distinction is:

- Agents are durable Nix-provisioned sandboxes with explicit worktrees, HOME,
  Codex state, SSH state, proxy/DNS policy, and resource controls.
- OCI containers are ephemeral test dependencies requested through a broker.
- Docker or Podman may remain useful, but only behind the broker boundary.

## Conversation Summary

The starting proposal in `kit/docs/proposals/nix-sandbox-agent-runtime.md`
identified Docker Desktop/WSL socket fragility as the incident that exposed a
larger architectural problem. Docker Compose currently provides packaging,
agent process isolation, network policy, sidecars, test dependencies, and access
to a privileged Docker control plane. Only test dependencies strongly require an
OCI runtime.

We agreed that the better shape is not to preserve Compose as a peer runtime.
Compose currently makes devkit's real concepts look like container orchestration:
Compose project names, Docker labels, service names, bridge networks, and
`docker exec` are acting as agent identity. The target state should instead make
agent identity first-class.

The migration should still try to keep current agents in-flight where feasible.
The existing compiled CLI at `kit/bin/devctl` and wrapper path
`kit/scripts/devkit` are useful operational anchors while new scaffolding lands.
However, new design should not preserve Compose abstractions merely to maintain
compatibility. A git revert is acceptable if the system needs to be rebuilt, but
we should avoid needlessly killing active sessions during the transition.

## Historical Repo State Observations

Update on 2026-05-12: these observations describe the starting state that
motivated the migration. The `dev-all` path now has a Nix flake shell, native
bubblewrap plans, managed broker lifecycle, native top-level lifecycle/exec
commands, runtime/repo readiness checks, app-level Spago/Netlify/Playwright
evidence, fresh linked-source-worktree support, and native tmux/layout routing.
The remaining Compose code is intentionally legacy for non-`dev-all` overlays
unless called out in `nix/compose-surface-inventory.md`.

At the start of the migration, devkit was Compose-first:

- `cli/devctl/internal/compose/builder.go` builds Docker Compose file arguments
  from base files, profiles, overlays, and generated ingress fragments.
- `cli/devctl/internal/runner/runner.go` shells out to `docker compose` for
  lifecycle operations.
- `cli/devctl/main.go` still contains many inline command paths that use Docker
  labels, Compose project names, `docker ps`, `docker exec`, and
  `docker compose attach`.
- `cli/devctl/internal/agentexec/agentexec.go` builds tmux shell commands as
  `docker exec` snippets.
- `kit/compose.yml`, `kit/compose.dns.yml`, `kit/compose.hardened.yml`, and
  overlay compose files encode agent networking, DNS, proxy, mounts, sidecars,
  and resource controls.

Useful target-state primitives already exist:

- Per-agent worktree and HOME path helpers live in
  `cli/devctl/internal/paths/paths.go`.
- SSH, Git, Codex, and credential seeding logic exists, though it currently runs
  through Docker exec.
- DNS/proxy policy files already exist under `kit/dns` and `kit/proxy`.
- `brokers/postgres-broker` already demonstrates the desired broker boundary:
  agents talk to a broker socket while the broker owns daemon access and
  enforces allowlists.
- Runtime integration tests are organized under `cli/devctl/integration/runtime`
  and `testing/runtime`, but they currently depend on Docker/Compose.

Important starting gaps and current disposition:

- Nix runtime scaffold: closed for `dev-all` with root `flake.nix`,
  `devctl native plan|exec`, and bubblewrap launch preparation.
- Runtime metadata: closed for `dev-all`; `overlays/dev-all/devkit.yaml`
  declares `runtime.flake: .#dev-all` and native state/worktree roots.
- Readiness split: closed for `dev-all`; runtime capacity is separate
  from repo readiness. Flake-backed overlays declare
  `readiness.default_mode: runtime-only`; `verify` and explicit
  `--repo-readiness` runs still fail on repo-check failures so app readiness is
  not hidden.
- Host-managed services: broker is implemented and verified; proxy/DNS policy is
  represented in native plans. Ingress/operator-attention remains the main
  service evidence area before broader Compose retirement.

## Work Layers

There are two broad units of work.

First, each existing container image needs to be converted into a Nix expression
or flake output that can produce the same useful tool environment without
requiring Compose to materialize a long-lived agent. This is per-overlay,
parallelizable work.

Second, devkit itself needs design and implementation work so lifecycle, exec,
tmux, readiness, layout application, DNS/proxy, and broker access target native
agents directly. This is central control-plane work and should be kept separate
from per-overlay tool conversion.

## Subagent-Friendly Shape

The migration should be organized so subagents can work in independent worktrees
without conflicting:

- Per-overlay flake conversions can be assigned by overlay directory or source
  image.
- Broker generalization can be isolated under `brokers/` and associated docs.
- Devkit runtime contracts can be built under new packages before touching legacy
  Compose-heavy command paths.
- Tests can be added as dry-run or plan-rendering tests before native launch is
  fully operational.

Each task should have an explicit write scope. The existing Compose path should
not be casually rewritten by flake-conversion workers.

## Operating Constraints

- Prefer preserving the currently running executable and active agents while
  scaffolding lands.
- Keep the canonical user entrypoint as `kit/scripts/devkit`.
- Do not introduce new wrapper fallbacks. The local agent instructions require
  wrappers to be thin exec shims that fail loudly if `kit/bin/devctl` is missing.
- New code should use the native agent model directly. Compose compatibility can
  remain temporarily, but it should not define the new architecture.
- Docker/Podman daemon access belongs to the broker, not to standard agent
  shells.
