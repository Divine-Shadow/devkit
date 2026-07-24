---
name: fleet-devkit-readiness
description: Maintain and verify the generic Devkit agent runtime substrate, including its Nix toolchain, compiled devctl entrypoint, Git/OpenSSH, bwrap/proxy, Codex auth/config hydration, worktrees, process supervision, and real readiness checks. Use when a fleet consumer cannot launch or run reliably through the canonical packaged Devkit path.
---

# Fleet Devkit Readiness

Read `AGENTS.md` and maintain the applicable ExecPlan from `.agent/PLANS.md`.

## Boundary

Keep Devkit Product-agnostic. Devkit supplies generic tools and lifecycle
mechanics; it does not select or acquire Product source to derive a runtime,
build Product JARs, publish Product identity, or choose among Product artifacts.
The authoritative Nix composition consumes one accepted Product revision and
combines its outputs with the generic Devkit package.

Devkit may use package-owned Git/OpenSSH to create the writable checkout for an
already-composed writer. That checkout is disposable computation, not the
source of the runtime that launches it.

## Workflow

1. Reproduce the failure through `kit/scripts/devkit`, which must execute the
   packaged `kit/bin/devctl`.
2. Identify the generic source invariant that failed: immutable executable
   selection, Git/SSH, proxy, sandbox, credential hydration, worktree
   construction, process supervision, or readiness.
3. Repair that invariant in Devkit source and verify it through the same real
   code path. Do not add a fallback, caller-selected executable, ambient-PATH
   dependency, or Product-specific compatibility layer.
4. Rebuild and converge the canonical package. Discard the failed consumer and
   validate on a fresh consumer when the repair changes its closure or runtime
   assumptions. Reuse unrelated healthy consumers after cheap identity checks.
5. Report the source repair and executable result. Do not substitute status,
   documentation, package membership, or a synthetic fixture for a working
   consumer.

Protect accepted published history, external data, and active external
transactions. Tasks, processes, homes, worktrees, sockets, and failed partial
attempts are replaceable. Never preserve them merely to avoid reconstruction.

## Verification

- `nix flake check --show-trace` passes for Devkit.
- The generic `dev-all-runtime-bundle` exposes `bin/dev-all-runtime-tools` and
  `kit/bin/devctl` and contains no Product artifact or source-selection
  authority.
- Relevant real Git/SSH, proxy, bwrap, auth, worktree, process, and readiness
  paths execute successfully for the consumer being repaired.
- A Product-environment deliverable is credited only by its owning lifecycle
  gate, not by Devkit checks alone.
