# Proposal: Nix-Native Agent Runtime With Brokered Test Containers

## Status: Implemented For `dev-all`

This proposal is now historical context plus design rationale. The active
`dev-all` control plane is Nix-native through the canonical
`kit/scripts/devkit` entrypoint and compiled `kit/bin/devctl` binary.

Implemented command surface:

- `devkit -p dev-all up|down|restart|status|logs`
- `devkit -p dev-all scale N`
- `devkit -p dev-all exec <index> ...`
- `devkit -p dev-all attach <index>`
- `devkit -p dev-all ensure-ready`
- `devkit -p dev-all native plan|prepare|exec|readiness|capacity`
- `devkit -p dev-all broker start|status|stop`

The native runtime launches agents with bubblewrap, Nix flakes, per-agent
HOME/Codex/XDG state, managed DNS/proxy environment, and brokered Docker access.
Native plans set `DOCKER_HOST` to the broker socket and do not bind
`/var/run/docker.sock` into the agent sandbox. Compose is no longer an implicit
fallback for `dev-all`; legacy Compose workflows must be requested explicitly via
`devkit -p dev-all compose <command>`, and non-`dev-all` overlays keep their
legacy Compose surface until they receive native replacements.

The implementation evidence, smoke commands, operational caveats, and parity
status live in `nix/runtime-parity.md`. The review handoff for the current
branch lives in `kit/docs/native-runtime-review-handoff.md`.

## Context

The current `ouro8` development layout uses Docker Compose to run every long-lived
agent container, plus sidecars for DNS, proxying, ingress, the Postgres broker, and
operator attention. This gives each agent a repeatable image and lets devkit attach
tmux windows into the right working tree, but it also makes the whole control plane
depend on a working Docker socket.

That dependency became visible while recovering a pair of NixOS WSL instances under
memory pressure. We added zram-backed swap to both instances and restarted them, then
brought the local and remote `devkit-ouro8` stacks back online. The local stack was
recoverable once Docker Desktop was running again. The remote WSL instance was more
fragile: WireGuard and zram came back after the WSL restart, but Docker Desktop's
WSL integration socket did not.

The remote recovery required manually starting Docker Desktop's user-distro proxy:

```bash
sudo nohup /mnt/wsl/docker-desktop/docker-desktop-user-distro \
  proxy \
  --distro-name NixOS \
  --docker-desktop-root /mnt/wsl/docker-desktop \
  "C:\\Program Files\\Docker\\Docker\\resources" \
  >/tmp/docker-desktop-user-distro-proxy.log 2>&1 &
```

After that, `/var/run/docker.sock` existed again and the remote devkit services could
be scaled back up. A previous attempt to drive Compose through `docker.exe` over
Windows interop was not a good substitute: it could reach the Windows Docker engine,
but Compose failed on WSL bind-mount service paths such as
`/run/guest-services/distro-services/nixos.sock`.

The immediate incident was not caused by devkit code, but it exposed an architectural
weak point: every long-lived agent depends on a privileged Docker control socket even
when the agent mostly needs a reproducible shell, isolated state, DNS/proxy controls,
and a working tree.

## What We Changed During Recovery

- Added zram-backed swap to the NixOS WSL host configuration with high swap priority.
- Loaded the `zram` module explicitly because the regular zram generator path was not
  reliable in WSL.
- Set `vm.swappiness = 100` and `vm.page-cluster = 0` for compressed-swap behavior.
- Added a remote `wireguard-wg0` systemd service so the WireGuard interface returns
  after WSL restart.
- Restarted local and remote WSL instances and verified zram, swap priority, and
  WireGuard handshakes.
- Brought the local stack back as eight agents.
- Brought the remote stack back as two agents after restoring Docker Desktop's
  user-distro proxy.

The remote full warm hook still failed on an Ouroboros Scala compile error in the
worktree, so remote capacity was restored with readiness skipped. That compile error
is separate from the runtime architecture issue.

## Problem Statement

Docker is currently doing several jobs at once:

- Packaging repeatable development tools.
- Isolating agent filesystem state and process trees.
- Supplying a virtual network where DNS and egress policy are controlled.
- Hosting long-lived sidecars such as DNS, proxy, ingress, and brokers.
- Running short-lived service dependencies for tests.
- Acting as a broad privileged control plane through `/var/run/docker.sock`.

Only the last two jobs strongly need OCI-style containers. The long-lived agent shell
itself can plausibly be modeled as a Nix-provisioned sandbox with narrower privileges.

The current Docker-centered design has several costs:

- WSL instances are brittle when Docker Desktop integration does not recreate its
  distro proxy and socket after restart.
- SSH automation becomes dependent on Windows interop details that are not stable
  across WSL sessions.
- Agents that only need a shell inherit broad Docker control-plane access.
- Restarting or recreating containers is operationally expensive because agents may
  contain live Codex sessions and multi-hour work.
- Warm hooks are heavyweight and can block capacity restoration even when the host
  runtime is otherwise healthy.

## Candidate Direction Implemented By `dev-all`

Move long-lived agents to a Nix-native sandbox runtime and keep Docker or Podman
behind a narrow broker for test-only containers.

The target shape:

- Agents are materialized from a flake or equivalent Nix closure.
- Each agent runs in a bounded sandbox using `bubblewrap`, systemd transient units,
  or a small privileged helper for namespace setup.
- Each agent gets its own working tree, HOME, Codex state, SSH state, process tree,
  and optional network namespace.
- DNS and proxy policy remain host-managed and declarative.
- Agents do not receive direct access to `/var/run/docker.sock`.
- Tests that need real OCI services call a broker, not Docker directly.
- The broker owns Docker or Podman access and enforces labels, quotas, allowlists,
  network attachment, cleanup, and audit.

This is not an argument for removing Docker from all workflows. It is an argument for
moving Docker out of the baseline agent runtime and into the narrower surface where
it is actually needed.

## Proposed Architecture

### Host Services

Run these as NixOS-managed services where possible:

- Controlled DNS service.
- Controlled HTTP(S) proxy.
- Optional ingress service.
- Test-container broker.
- Optional native Docker or Podman daemon owned only by the broker.

On WSL, prefer native Linux services inside the distro over Docker Desktop integration
for SSH-driven automation. If Docker Desktop must remain in the loop, declare a
systemd service for the Docker Desktop user-distro proxy so the current manual
recovery step becomes repeatable and restartable.

### Agent Sandbox

Each agent sandbox should receive:

- A Nix-built tool environment.
- A specific working tree path.
- A per-agent HOME anchor.
- A per-agent Codex home and rollout directory.
- A per-agent SSH config.
- Bind mounts for explicit caches only.
- A generated `resolv.conf` pointing at the managed DNS service.
- Proxy environment variables matching the current devkit egress policy.
- Optional CPU, memory, and process limits through systemd or cgroups.

Filesystem isolation can start with `bubblewrap`. Network isolation needs more care:
either use `systemd-run` with network namespace controls or add a small privileged
host helper that creates netns/veth/DNS wiring for each agent.

### Test Container Broker

The broker should expose narrow operations such as:

- Start an approved ephemeral dependency, for example Postgres, Redis, browser, or
  localstack.
- Attach the dependency to an agent-visible network or publish a controlled endpoint.
- Return connection details to the caller.
- Enforce time-to-live, resource limits, image allowlists, and cleanup labels.
- Deny arbitrary Docker socket operations from agent shells.

This broker can initially wrap Docker because the Docker ecosystem remains valuable
for Testcontainers-like workflows. The key design boundary is that agents ask for
services; they do not control the daemon.

## Historical Implementation Plan

### Phase 0: Stabilize Current WSL Runtime

- Keep the zram and WireGuard services declarative in the NixOS WSL config.
- Add a declarative Docker Desktop user-distro proxy service if Docker Desktop remains
  in use.
- Add an operator check that reports whether `/var/run/docker.sock` is backed by a
  live integration before devkit attempts a Compose operation.
- Add an escape hatch for capacity recovery that can scale agents with readiness
  skipped when warm hooks fail for repository-code reasons.

### Phase 1: Prototype One Nix-Native Agent

- Pick one overlay, preferably `dev-all`, and generate a flake-based shell matching
  the current agent image's core tools.
- Launch a single agent sandbox with `bubblewrap` or `systemd-run`.
- Bind only the selected worktree, per-agent HOME, and approved caches.
- Point DNS and proxy settings at the existing devkit sidecars or equivalent host
  services.
- Attach a tmux window to the sandbox using the existing `exec-cd` user experience as
  the model.

### Phase 2: Add Brokered Test Containers

- Define the first broker API around the existing Postgres broker pattern.
- Support an allowlisted set of images and service profiles.
- Make the broker own Docker or Podman credentials and socket access.
- Add cleanup on timeout, sandbox exit, and devkit shutdown.
- Add audit logs that connect a service instance to the requesting agent and worktree.

### Phase 3: Move More Agents Off Docker

- Scale the Nix-native runtime to multiple agents.
- Preserve per-agent HOME, Codex state, SSH state, and worktree behavior.
- Add resource controls comparable to or better than the current Compose limits.
- Keep Compose support during the migration for overlays that still require it.

### Phase 4: Retire Docker-As-Agent-Runtime

- Make the Nix-native sandbox the default for long-lived agents.
- Keep Docker or Podman only for brokered test dependencies and any overlays that
  explicitly require a container image.
- Remove direct Docker socket access from standard agent shells.

## Remaining Design Questions

- Which sandbox launcher should be canonical: direct `bubblewrap`, `systemd-run`, or
  a small devkit-owned helper that combines filesystem and network setup?
- How much network isolation is required per agent: shared controlled network,
  per-agent netns, or per-task netns?
- Should the test-container broker use Docker, Podman, or support both behind one API?
- Which current warm-hook steps are runtime provisioning, and which are repository
  readiness checks that should be moved out of capacity restoration?
- How should devkit represent mixed runtimes during migration: per-overlay runtime
  selection, per-agent runtime selection, or a project-level mode?
- What is the minimum flake interface each project must expose for devkit to launch a
  useful agent shell?

## Acceptance Criteria

- A WSL restart does not require Docker Desktop integration to recreate long-lived
  agent shells.
- `mega-devkit` or equivalent tmux layout can attach to Nix-native local and remote
  agents.
- Standard agent shells have no direct Docker socket access.
- Tests that need OCI services can still request them through a broker.
- DNS and proxy restrictions remain at least as strong as the current Compose setup.
- Agent state under Codex homes, worktrees, and SSH directories survives runtime
  restarts.
- Capacity restoration is not blocked by repository compile failures unrelated to
  the host runtime.

## Historical First Task

Create a small prototype command, separate from the existing Compose path, that starts
one `dev-all` agent as a Nix-provisioned sandbox on NixOS WSL. It should bind the
root Ouroboros worktree, create a per-agent HOME, set proxy/DNS environment, launch an
interactive shell, and document every host capability it still needs. Do not remove
or rewrite the Compose path during the prototype.
