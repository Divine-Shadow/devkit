# Devkit CLI Migration Proposal (Bash → Go)

## Context

The devkit’s primary controller script `kit/scripts/devctl` has grown to ~600 lines and now contains a mix of orchestration (docker compose), file mutation (allowlists, DNS), YAML-driven hooks, and host/terminal integrations (tmux, SSH, git). While it works, the current Bash implementation is increasingly difficult to evolve safely, test comprehensively, and maintain.

Key observations:
- Many subcommands with branching/validation (e.g., `up|down|restart|scale|exec|attach|logs|status|allow|proxy|warm|maintain|check-*|tmux-shells|fresh-open|ssh-*|repo-*|worktrees-*`).
- String parsing of configuration (grep/sed for YAML `devkit.yaml`).
- Idempotent file edits required (`kit/proxy/allowlist.txt`, `kit/dns/dnsmasq.conf`).
- Frequent external process orchestration: `docker compose`, `curl`, `aws`, `tmux`, `git`, `ssh`.

These are strong signals to move the core logic to a typed CLI for clearer structure, safer I/O, and better testability, while keeping thin shell shims for interoperability.

## Goals

- Preserve current behavior, flags, exit codes, and UX where sensible.
- Introduce a small, dependency-light, statically-typed CLI binary.
- Make core logic unit-testable (compose arg building, file edits, config parsing).
- Keep OS/glue that benefits from shell (e.g., simple `iptables` helper) as-is.
- Provide a gradual migration path without breaking existing workflows.

Non-goals:
- Rewriting Docker interactions to the Docker SDK (we keep shelling out to `docker compose`).
- Changing network architecture or container topology.

## Scope and Phasing

Phase 1 (MVP — lowest risk, highest value):
- Implement `up|down|restart|status|scale|exec|attach|logs` by executing `docker compose` with robust argument building and timeouts.
- Implement `allow` with safe, idempotent appends (atomic file writes) for proxy and DNS allowlists.
- Implement `warm|maintain` by reading `overlays/<project>/devkit.yaml` via a YAML parser and executing the resulting hook in the agent container.
- Add `check-net` basic connectivity check.
- Preserve `scripts/devkit` wrapper to exec the new binary only. No fallback. If the binary is missing, the wrapper must fail loudly with build instructions.

Phase 2:
- Port `check-codex`, `check-claude`, `check-sts` with structured output and timeouts.
- Port tmux session orchestration: `tmux-shells`, `fresh-open` (better parameter validation, clearer logs).
- Port SSH/Git helpers and worktrees commands.

Phase 3:
- Deprecate Bash `devctl` internals; wrapper scripts continue to exist but become thin shims to the binary.
- Optional: add a `--dry-run`/`DEVKIT_DRY_RUN` flag for safe previewing of external commands.

Out of scope / stay in shell:
- `kit/scripts/net-firewall.sh` (root-only `iptables` changes, already small and host-specific).

## Proposed File/Package Structure

Binary lives in devkit and is built locally; code is dependency-light and composable.

```
devkit/
  cli/
    devctl/
      go.mod
      main.go                 # flag parsing and subcommand dispatch
      internal/
        compose/
          builder.go          # builds docker compose args (-f …) + profiles
        config/
          overlay.go          # read overlays/<project>/devkit.yaml (warm/maintain)
        files/
          allowlist.go        # append-if-missing; atomic writes; regex helpers
        execx/
          run.go              # exec.CommandContext wrappers, timeouts, logging
        checks/
          net.go              # connectivity checks; structured output
        tmux/                 # (Phase 2) tmux helpers
        gitssh/               # (Phase 2) git/ssh helpers and worktrees
  kit/
    bin/
      devctl                 # built binary (ignored by git)
    scripts/
      devctl                 # legacy bash may exist for reference; wrappers must not call it
      net-firewall.sh        # unchanged
```

Notes:
- Place source in `devkit/cli/devctl` to separate code from `kit/` artifacts.
- Ship a simple `Makefile` target: `make -C devkit/cli/devctl build` that outputs to `devkit/kit/bin/devctl`.
- Wrappers (`scripts/devkit` and `ouroboros-ide/scripts/devkit`) exec `kit/bin/devctl` only; on missing binary, print a clear error and exit non‑zero.

## CLI Compatibility

- Preserve flags: `-p|--project`, `--profile` (comma-separated profiles: `hardened,dns,envoy`).
- Subcommands parity with current Bash. MVP covers: `up|down|restart|status|scale|exec|attach|logs|allow|warm|maintain|check-net`.
- Exit codes: propagate underlying process exit codes where applicable; consistent non-zero on validation failures.
- Output: human-readable logs; for checks, short structured lines suitable for grepping.

## Implementation Outline (MVP)

1) Compose builder
- Inputs: `KIT_DIR`, `OVERLAYS_DIR`, `PROJECT`, `PROFILE`.
- Output: `[]string` of `docker compose` args (`-f` files) resolving overlay `compose.override.yml` if present.
- Unit tests with table-driven cases.

2) Allowlist edits (idempotent)
- Files: `kit/proxy/allowlist.txt` and `kit/dns/dnsmasq.conf`.
- Functions: `AppendDomainIfMissing(domain)`, `AppendDnsRuleIfMissing(domain)` using atomic write (temp file + rename) and strict patterns.
- Unit tests using `t.TempDir()` fixtures and golden files.

3) Overlay hooks
- Parse `overlays/<project>/devkit.yaml` using a YAML lib; read `warm`/`maintain` keys.
- Execute hooks via `docker compose … exec` inside the agent container.
- Unit tests around parsing; execution path covered by integration test or dry-run.

4) Orchestration
- Wrap external commands with `context.Context` for timeouts and cancellation.
- Log commands when `DEVKIT_DEBUG=1`.

## Testing Strategy

Unit tests (Go):
- compose builder: profiles and overlay presence/absence.
- allowlist/dns edits: append-if-missing, ordering preserved, no duplicates; atomic writes simulation.
- config parsing: valid/invalid YAML, empty hooks.

Golden/help tests:
- `devctl -h` / per-subcommand `-h` to stabilize CLI surface.

Integration smoke (optional CI job, flagged):
- If `docker` available, run `devctl --dry-run` pathways to verify compose arg assembly and error messages.
- Skip by default when Docker is unavailable.

Static checks:
- `go vet`, `golangci-lint` (optional), and `shellcheck` retained for remaining shell scripts.

## Migration Plan and Rollout

1) Add Go scaffold and Makefile; wire the wrappers to exec the binary only (no fallback).
2) Implement MVP commands; add unit tests; document usage.
3) Dogfood internally; collect feedback; fix incompatibilities.
4) Port remaining commands (checks, tmux, ssh/git/worktrees).
5) Announce deprecation of Bash internals after parity; keep shell wrappers.

## Risks and Mitigations

- Behavior drift: mitigate via unit tests and side-by-side usage; preserve messages and exit codes where feasible.
- Cross-platform assumptions: current kit is Linux-focused; keep explicit OS checks and helpful errors.
- External tool availability: validate presence (`docker`, `tmux`, etc.) early with clear guidance.

## Action Items (Initial PR)

- Add module skeleton under `devkit/cli/devctl` (no external deps besides YAML parser).
- Add Makefile to build into `devkit/kit/bin/devctl` and `.gitignore` entry for that path.
- Implement MVP subcommands and tests.
- Update docs to reference the new CLI and migration status.
