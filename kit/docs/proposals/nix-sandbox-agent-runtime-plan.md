# Nix-Native Runtime Implementation Plan

## Objective

Refactor devkit to a Nix-native agent runtime and retire Compose as the core
framework. Compose may remain temporarily as legacy or break-glass support, but
new architecture should model agents directly instead of abstracting over
Compose concepts.

The first useful milestone is one `dev-all` agent launched as a Nix-provisioned
sandbox while current Compose agents can continue running.

## Guiding Rules

- Build Nix-first, not mixed-runtime-first.
- Keep OCI access behind a broker.
- Preserve active agents where feasible by adding new commands/packages before
  replacing old paths.
- Make runtime plans inspectable with dry-run output before launching sandboxes.
- Keep per-overlay flake conversion independent from devkit control-plane work.
- Verify every Nix artifact with
  `kit/docs/proposals/nix-runtime-verification-contract.md`.
- Avoid changing unrelated Compose behavior until the native path can replace it.

## Phase 0: Stabilize The Transition Surface

Goals:

- Keep `kit/bin/devctl` usable while native scaffolding is introduced.
- Make the current executable path a stable operational anchor.
- Document the legacy Compose path as temporary.

Tasks:

- Audit `kit/scripts/devkit` against the local no-fallback wrapper rule.
- Add a short migration note that current Compose commands are legacy while the
  native runtime is built.
- Avoid broad edits to `cli/devctl/main.go` until native packages have tests.
- Add dry-run command surfaces first so implementation can be reviewed without
  stopping active containers.

Deliverables:

- Wrapper behavior decision.
- Legacy-runtime note.
- No disruption to existing running agents.

## Phase 1: Convert Tool Environments To Flakes

Goals:

- Replace long-lived agent images with Nix-provisioned tool environments.
- Make each overlay's toolchain reproducible without requiring Compose.

Initial targets:

- `dev-all` / `ouroboros-ide` first.
- Then overlays that currently share the same image or broker pattern.

Tasks per overlay:

- Inspect the current Dockerfile and compose environment.
- Create a flake output or Nix module that provides the agent shell tools.
- Preserve required tools, language runtimes, CLIs, certificates, and wrappers.
- Document remaining host capabilities or non-Nix inputs.
- Add a smoke command that proves the shell has the expected core tools.
- Record evidence using the Nix runtime verification contract.

Suggested write scopes for subagents:

- One worker per overlay directory.
- One worker for shared Nix package helpers.
- One worker for docs/examples after the first overlay is proven.

Deliverables:

- A `dev-all` flake shell that can replace `local/dev-agent:ouroboros-ide` for
  interactive agent work.
- A repeatable pattern for converting additional overlays.

## Phase 2: Define Native Agent Runtime Contracts

Goals:

- Make agent identity independent of Docker.
- Give tmux/layout/exec/readiness a native target model.

New concepts:

- Agent ID: project, index, repo/worktree, home, state root, runtime unit.
- Agent plan: tool environment, binds, environment, DNS/proxy, broker endpoint,
  resource limits, launcher.
- Agent operations: create, start, stop, exec, attach, list, status.

Likely code scaffolding:

- `cli/devctl/internal/runtime/agent`
- `cli/devctl/internal/runtime/native`
- `cli/devctl/internal/runtime/plan`
- `cli/devctl/internal/sandbox`

Tasks:

- Extend `devkit.yaml` with native runtime fields, or add a new experimental
  native config block while schema settles.
- Implement plan generation for one `dev-all` agent without launching it.
- Reuse existing path helpers from `cli/devctl/internal/paths`.
- Move common SSH/Git/Codex environment assembly behind runtime-neutral helpers.
- Add unit tests for plan rendering and path/env derivation.

Deliverables:

- `devkit native plan` or equivalent dry-run command for one `dev-all` agent.
- Tests that do not require Docker, Nix, or systemd.

## Phase 3: Launch One Native Agent

Goals:

- Start one useful `dev-all` sandbox on NixOS WSL.
- Preserve current Compose agents during the prototype.

Launcher options:

- Start with `bubblewrap` if filesystem isolation is the first priority.
- Use `systemd-run` if cgroups, process ownership, and cleanup are more urgent.
- Add a small privileged helper only if network namespace setup cannot be done
  cleanly with existing host tools.

Prototype requirements:

- Bind the root Ouroboros worktree.
- Create and use a per-agent HOME.
- Set `CODEX_HOME`, `CODEX_ROLLOUT_DIR`, XDG dirs, and cache locations.
- Seed SSH/Git/Codex state through runtime-neutral helpers.
- Set proxy environment consistently with current policy.
- Generate or bind resolver config for managed DNS.
- Expose the broker endpoint without direct Docker socket access.
- Launch an interactive shell.
- Document every host capability still needed.

Deliverables:

- One native `dev-all` agent shell usable through `kit/scripts/devkit`.
- Dry-run output matching the real launch.
- A short capability report.

## Phase 4: Move Broker Services To Host Runtime

Goals:

- Preserve Testcontainers-style workflows without granting agents daemon access.
- Generalize the existing Postgres broker into the target broker boundary.

Tasks:

- Rename or wrap the Postgres broker concept as a test-container broker.
- Add service profiles for approved dependencies such as Postgres, MinIO, Redis,
  browser, or localstack as needed.
- Add requester identity, TTL, labels, cleanup, and audit logs.
- Make broker endpoint delivery work for native sandboxes.
- Keep Docker and Podman behind one broker-facing interface if practical.

Deliverables:

- Host-managed broker service.
- Native agent can request an approved dependency.
- No standard native agent has `/var/run/docker.sock`.

## Phase 5: Replace Tmux, Layout, Exec, And Readiness Paths

Goals:

- Make user workflows target native agents.
- Stop building tmux commands as Docker exec snippets.

Tasks:

- Refactor `agentexec` to request a runtime-specific command from the native
  agent model.
- Update `tmux-sync`, `tmux-add-cd`, `wt-open`, and layout apply to use native
  agent identity.
- Split readiness into:
  - runtime readiness: sandbox exists, shell launches, DNS/proxy/broker visible,
    state paths present;
  - repository readiness: warm hooks, installs, compile/typecheck checks.
- Make capacity restoration depend only on runtime readiness.
- Keep repository warm checks explicit and separately retryable.

Deliverables:

- Native tmux windows for multiple `dev-all` agents.
- Layout apply starts or attaches native agents.
- Warm hook failures no longer block basic agent capacity restoration.

## Phase 6: Retire Compose Framework

Goals:

- Remove Compose as the organizing framework once native workflows cover the
  core use cases.

Tasks:

- Stop adding features to Compose-specific command paths.
- Mark Compose commands as legacy or break-glass.
- Migrate overlays from compose files to flake/native config.
- Remove Compose profiles once equivalent host services exist.
- Delete obsolete Docker identity assumptions after users are off them.

Exit criteria:

- `dev-all` runs native by default.
- Tmux/layout/exec/readiness work without Compose.
- DNS/proxy/ingress/broker are host-managed.
- Tests needing OCI services use the broker.
- Standard agents have no direct Docker socket access.
- Capacity restoration is not blocked by repository compile or warm failures.

## Parallel Work Queue

Good independent worktree tasks:

- Convert `dev-all` tool image to a flake shell.
- Inventory all overlay Dockerfiles and group them by shared toolchain.
- Build native runtime plan structs and tests.
- Generalize broker config and policy naming.
- Add native dry-run CLI docs.
- Extract runtime-neutral SSH/Git/Codex seeding helpers.
- Design host-managed DNS/proxy service modules.
- Add a native test fixture that skips when Nix or sandbox tools are missing.

Avoid assigning overlapping write scopes:

- Do not have multiple workers editing `cli/devctl/main.go`.
- Do not mix overlay flake conversion with runtime command refactors.
- Do not rewrite Compose files as part of native launcher scaffolding unless the
  task is explicitly a legacy cleanup.
