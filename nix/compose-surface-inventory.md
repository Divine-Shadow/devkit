# Compose Surface Inventory

Date: 2026-05-12

This inventory records the remaining Docker Compose references after the
Nix-first `dev-all` migration slice. The target boundary is simple:
`dev-all` is native-only, while Compose remains an explicit legacy surface for
non-`dev-all` overlays until those overlays receive native replacements.

## Classification Key

- **Native replacement**: `dev-all` uses a Nix/native implementation for this
  behavior.
- **Rejected for dev-all**: the path may still exist for legacy overlays, but
  it must fail before Docker/Compose is invoked when the project is `dev-all`.
- **Legacy quarantine**: the path is intentionally kept for non-`dev-all`
  overlays.
- **Historical artifact**: retained as reference material only; not an
  executable `dev-all` path.

## Inventory

| Surface | Classification | Current boundary |
| --- | --- | --- |
| `kit/scripts/devkit` | Native replacement | Thin exec shim only. It runs `kit/bin/devctl` or fails loudly with build instructions; no build/download fallback remains. |
| Top-level `up`, `down`, `restart`, `status`, `logs`, `scale`, `exec`, `attach`, `ensure-ready` | Native replacement | Registry dispatch owns these names for `-p dev-all`. Non-`dev-all` overlays fall through to legacy Compose where applicable. |
| `devctl -p dev-all compose ...` | Rejected for dev-all | `composecmd.EnsureLegacyProject` rejects `dev-all` before cleanup, readiness, or Docker invocation. |
| `cli/devctl/internal/commands/composecmd` | Legacy quarantine | Still implements Compose lifecycle for non-`dev-all`; unit tests cover the `dev-all` rejection. |
| `cli/devctl/internal/compose` builder/resolver | Legacy quarantine | Still resolves `kit/compose*.yml` and overlay `compose.override.yml` for non-`dev-all` overlays and legacy tests. |
| Legacy lifecycle helpers in `cli/devctl/main.go` | Legacy quarantine | Compose helpers remain for non-`dev-all`; `useLegacyTopLevelForProject` prevents dev-all fallback and layout guards reject dev-all mixed layouts. |
| `layout-apply` and `tmux-apply-layout` | Native replacement / rejected for dev-all | Pure `dev-all` layouts use native tmux/window commands. Legacy layouts that explicitly reference `dev-all` are rejected. |
| `tmux-sync`, `tmux-add-cd`, `wt-open --plain`, `worktrees-tmux` | Native replacement | `dev-all` commands build top-level native `devkit exec` invocations and no longer infer `dev-all` by Docker inspection or pass Compose project names. |
| `wt-release`, worktree branch/status/sync helpers, repo config/push helpers | Native replacement / legacy quarantine | `dev-all` uses host native worktree roots and native exec where an agent shell is needed. Non-`dev-all` helpers remain legacy. |
| `hosts` command | Rejected for dev-all | Container `/etc/hosts` mutation is Compose/container-specific. `dev-all` now fails before reading overlay config. |
| `allow` and `preflight` | Legacy quarantine / runtime-neutral | Allowlist and host preflight helpers are not a `dev-all` Compose execution path. They still serve legacy proxy/DNS files used by non-`dev-all` Compose overlays. |
| `hooks`, `warm`, `maintain`, `check-net`, `check-codex`, `check-sts` | Native replacement / legacy quarantine | `dev-all` goes through native exec. Non-`dev-all` can still use legacy Compose implementations. |
| `verify`, `verify-all`, `image-matrix`, `layout-validate`, `layout-generate` | Native replacement / rejected for dev-all / legacy quarantine | `dev-all` verification routes through native checks or refuses Compose-only diagnostics. Image matrix and legacy layout generation remain for historical overlays. |
| `tmux-bell-*` and tmux notification helpers | Runtime-neutral | These operate on local tmux state and are not Docker/Compose execution paths. |
| Credential pool surfaces | Legacy quarantine | `kit/compose.pool.yml` remains non-`dev-all` legacy behavior. Native Codex auth seeding uses per-agent native HOME state instead of the Compose pool mount. |
| Ingress/operator-attention metadata | Partial native evidence | `overlays/dev-all/devkit.yaml` carries route/listener metadata, but this slice does not yet prove host-managed ingress or operator-attention lifecycle. This is a retirement blocker for that surface only. |
| `doctor-runtime` and other Compose-only diagnostics | Rejected for dev-all | These commands refuse `dev-all` and point users to native readiness/capacity. |
| `overlays/dev-all/compose.override.yml` | Historical artifact | Kept as migration history/image pairing reference. The CLI no longer allows it to be executed through `-p dev-all compose`. |
| `overlays/*/devkit.yaml` runtime metadata | Native replacement | Every overlay declares `runtime.flake`; `runtime.image` is no longer authoritative runtime metadata. |
| `overlays/codex`, `overlays/ouroboros-static-front-end`, `_template`, and other overlay `compose.override.yml` files | Legacy quarantine | Still describe non-`dev-all` legacy lifecycle stacks. Their tool runtimes are Nix flake outputs, but top-level lifecycle dispatch still falls through to Compose except for `dev-all`. |
| `kit/compose.yml`, `kit/compose.dns.yml`, `kit/compose.hardened.yml`, `kit/compose.envoy.yml`, `kit/compose.pool.yml` | Legacy quarantine | Shared Compose base files for non-`dev-all` overlays and historical integration tests. |
| `cli/devctl/internal/testutil` Compose fixtures and integration tests | Legacy quarantine | Test infrastructure for legacy overlays. Native regression tests separately assert that `dev-all` does not emit Compose commands. |
| Postgres broker Docker API | Native replacement | Native agents receive `DOCKER_HOST=unix://<broker socket>`. This is brokered Docker API access, not Docker Compose, and `/var/run/docker.sock` is not mounted. |
| Tinyproxy/proxy environment | Native replacement / legacy quarantine | Native plans no longer default `HTTP_PROXY` or `HTTPS_PROXY` to a Compose-era proxy. Explicit proxy configuration is still honored. |
| Runtime tools: PureScript, Spago, Netlify, Playwright, Terraform, Packer, ARM embedded tools | Native replacement | Provided by per-overlay flake/devShell outputs and verified by overlay runtime smokes. Hooks should not install these CLIs or Playwright browsers at runtime. |
| Repo-local shebangs such as `#!/usr/bin/env node` | Native replacement | Bubblewrap launches add `/usr/bin/env`, `/bin/sh`, and `/bin/bash` symlinks into the blank-root sandbox so lockfile-installed tools can execute. |

## Readiness Matrix

| Surface | `dev-all` status | Evidence | Retirement blocker |
| --- | --- | --- | --- |
| Wrapper/build | Native canonical | `kit/scripts/devkit`, native smoke | none |
| Lifecycle | Native | `up/down/status/logs/scale` smokes | none |
| Exec/attach | Native bubblewrap | native exec broker/socket smoke | none |
| Readiness/capacity | Native split | full readiness plus `native capacity` split | heavyweight repo checks must stay explicit |
| Spago/PureScript | Native/app readiness | flake tool pins plus frontend repo checks | none after green full readiness |
| Netlify/dev server | Native/app readiness | `frontend-netlify-dev-server` check starts and curls `netlify dev` | none after green full readiness |
| Playwright/browser | Native/app readiness | runtime and frontend Chromium checks | broader local-backend e2e remains optional app coverage |
| Brokered OCI | Native broker | Redis denied, Postgres allowed | broader service policy only if new test dependencies are needed |
| Tmux/layout/worktrees | Native for `dev-all` | dry-run/runtime parity list plus linked-source-worktree readiness | none for current `dev-all` paths |
| Ingress/operator attention | Partial | config exists | host-managed smoke evidence missing |
| Compose command namespace | Rejected for `dev-all` | smoke and unit tests | none |
| Non-`dev-all` overlays | Native tool flakes, legacy lifecycle | per-overlay `runtime.flake` and `overlay-runtime-smoke` | native lifecycle/exec/down not implemented outside `dev-all` |

## Verification Hooks

- `cli/devctl/integration/native_defaults_dryrun_test.go` covers native
  top-level dispatch, non-`dev-all` legacy fallback, `dev-all compose`
  rejection, and legacy layout rejection.
- `cli/devctl/internal/worktrees/worktrees_integration_test.go` covers native
  dedicated worktree setup, absolute gitdir metadata, and the case where the
  source repo is itself a linked Git worktree.
- `cli/devctl/internal/commands/hosts/hosts_test.go` covers `dev-all` host
  mutation rejection.
- `kit/scripts/native-runtime-smoke` checks the canonical wrapper failure mode,
  `dev-all compose` rejection, native plan Git metadata binds, broker policy,
  runtime-only readiness, and PureScript/Spago/Netlify/Playwright availability.
- `kit/scripts/overlay-runtime-smoke` checks every overlay's Nix flake runtime
  tools without starting Compose.
- Full readiness is the final integration gate:

```bash
kit/scripts/devkit -p dev-all ensure-ready --repo ouroboros-ide --count 1 --flake .#dev-all
```
