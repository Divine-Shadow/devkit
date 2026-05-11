# Nix Runtime Verification Contract

## Objective

Every Nix artifact produced for the devkit runtime migration must be proven
reproducible, usable for agent work, and equivalent enough to the container or
tool environment it replaces.

This contract applies to flakes, Nix modules, dev shells, packages, launch
helpers, and host-service definitions created for the Nix-native runtime plan.

## Constraints

- Preserve current agents and executable workflows unless the task explicitly
  authorizes disruption.
- Keep OCI daemon access brokered. Verification must not require standard
  agents to access `/var/run/docker.sock`.
- Use governed agents or repository-provided helpers when a matching workflow
  exists; keep manual fallback minimal and evidence-backed.
- Maintain an ExecPlan per `.agent/PLANS.md` for significant refactors,
  governed implementation work, or multi-agent migrations.
- Do not update flake inputs, lockfiles, Compose behavior, or host services
  unless that change is explicitly in scope.
- Do not declare parity from successful Nix evaluation alone. Usability evidence
  from the shell or runtime is required.

## Required Evidence

For each Nix artifact, record:

- Artifact path and intended replacement target.
- Source container, Dockerfile, Compose service, or host behavior being
  replaced.
- `nix flake check` result when the artifact is part of a flake.
- Targeted `nix build` command and resulting store path for package outputs.
- `nix develop` or equivalent shell smoke command and output.
- Tool parity comparison against the source environment.
- Runtime smoke result when the artifact participates in launching agents.
- Exact failure logs when blocked.
- Explicit host capability gaps or follow-up tasks.

## Tool Parity Checklist

Compare against the source container or current Compose service:

- Language runtimes and build tools.
- Project CLIs and wrappers.
- Codex, Git, SSH, and authentication prerequisites.
- Certificates and trust stores.
- Proxy and DNS-related environment expectations.
- Cache paths and writable state directories.
- Browser or test-driver dependencies.
- Java, Node, Python, Go, Scala, sbt, or other stack-specific versions.
- Any service-specific environment variables used by current overlays.

## Runtime Verification Checklist

For native agent launch work, evidence must include:

- Dry-run launch plan with tool environment, bind mounts, HOME, Codex state,
  SSH state, proxy settings, DNS settings, broker endpoint, and resource limits.
- At least one real shell smoke when host capabilities are available.
- Confirmation that the standard agent shell has no direct Docker socket access.
- Confirmation that OCI test dependencies are reachable only through the broker.
- Confirmation that current executable workflows remain undisturbed, or a clear
  operator-authorized disruption note.

## Evidence Template

```text
Artifact:
Replacement target:
Source environment:

Commands:
- nix flake check ...
- nix build ...
- nix develop ... --command ...

Store paths:
- ...

Tool parity:
- present:
- missing:
- intentionally different:

Runtime smoke:
- dry-run plan:
- shell smoke:
- broker check:
- docker socket check:

Gaps and follow-ups:
- ...
```

## Current Baseline

At the time this contract was added, devkit had no checked-in Nix artifacts:
there was no `flake.nix`, `flake.lock`, or `*.nix` file under the repository.
The first verification work should therefore begin with the first artifact added
by the flake-conversion tasks described in
`kit/docs/proposals/nix-sandbox-agent-runtime-plan.md`.
