# Compose Retirement Guardrail

This inventory records the remaining Docker Compose and Docker helper surfaces
after the native runtime transition. The guardrail is:

- overlays with `runtime.flake` must route lifecycle and helper commands through
  native/Nix behavior by default;
- `kit/scripts/devkit` remains the supported entrypoint and execs
  `kit/bin/devctl`;
- `compose ...` remains the explicit legacy escape hatch for overlays that still
  need Compose semantics;
- any remaining Compose/Docker call site is classified as native-replaced,
  explicit legacy, or compatibility support.

## Dispatch Guard

`cli/devctl/main.go` owns the pre-dispatch decision in
`commandNeedsComposeFiles`. The native guard has three buckets:

- Never load Compose files: `native`, `proxy`, `allow`, `broker`,
  `image-matrix`, `layout-validate`, `preflight`, `verify-all`,
  `worktrees-init`, `wt-release`, `tmux-bell-install`,
  `tmux-bell-show-config`, and `tmux-notify-bell`.
- Native-aware helpers: lifecycle, exec/attach, hooks, network checks,
  layout/tmux helpers, worktree helpers, SSH/repo helpers, and `hosts` avoid
  Compose files for `runtime.flake` overlays and for native `dev-all` where
  applicable.
- Native-aware auth: `codex-auth` and its `creds` alias seed native state for
  any `runtime.flake` overlay and keep the old Compose reseed path only for
  legacy overlays.
- Legacy default: unknown commands and explicit `compose ...` still load Compose
  files so legacy overlays keep their old behavior. The retired
  `dev-all compose ...` namespace refuses before Compose file resolution.

Tests:

- `cli/devctl/service_test.go::TestCommandNeedsComposeFilesGuardrail`
- `cli/devctl/integration/native_defaults_dryrun_test.go::TestFlakeBackedNonDevAllRegistryCommandsAvoidComposeWorkspaceValidationDryRun`
- `cli/devctl/integration/native_defaults_dryrun_test.go::TestFlakeBackedHostsCommandIsLegacyOnlyDryRun`
- `cli/devctl/integration/native_defaults_dryrun_test.go::TestDevAllComposeNamespaceRefusesBeforeComposeFilesDryRun`

## Explicit Legacy Compose

These paths intentionally remain Compose-backed. They are valid only when the
caller has selected legacy behavior explicitly or the overlay lacks
`runtime.flake`.

- `cli/devctl/internal/commands/composecmd/compose.go`
  - `compose up|down|restart|ps|run|exec|attach`
  - This is the documented legacy escape hatch. It refuses `dev-all`, where
    Compose is retired.
- `cli/devctl/main.go`
  - Legacy fallback switch for top-level `up`, `down`, `restart`, `status`,
    `logs`, `scale`, `ensure-ready`, `exec`, and `attach` when the selected
    overlay does not declare `runtime.flake`.
  - Legacy helper branches for `exec-cd`, `attach-cd`, `codex-auth`/`creds`,
    `codex-test`, `codex-debug`, `check-sts`, `tmux-shells`, `open`,
    `fresh-open`, `reset`, and mixed legacy `layout-apply`.
  - Legacy SSH/repo helper fallback branches for overlays without
    `runtime.flake`.
- `cli/devctl/internal/commands/network/network.go`
  - `check-net` and `check-codex` run native exec for native overlays and keep
    Compose exec only for legacy overlays.
- `cli/devctl/internal/commands/hooks/hooks.go`
  - `warm` and `maintain` run native hook exec for native overlays and keep
    Compose exec only for legacy overlays.
- `cli/devctl/internal/commands/tmuxcmd/tmux.go`
  - Plain/native window commands avoid Compose for native overlays.
  - Legacy tmux and Windows Terminal compatibility still uses the injected
    legacy command builders for non-native overlays.
- `cli/devctl/internal/commands/hosts/hosts.go`
  - `hosts` is classified as a legacy Compose/container command. It now refuses
    `dev-all` and flake-backed overlays before Docker or Compose container
    discovery can run.

## Intentional Compatibility

These surfaces mention Compose or `dev-all` but do not represent default native
overlay dispatch through Compose.

- `cli/devctl/internal/runner/runner.go`
  - Low-level Compose runner functions are retained for explicit legacy paths.
- `cli/devctl/internal/compose/builder.go`
  - Compose file argument construction remains for legacy overlays and the
    explicit `compose ...` namespace.
- `cli/devctl/internal/commands/verifyall/verifyall.go`
  - `verify-all` preserves legacy `COMPOSE_PROJECT_NAME` selection while it
    shells back through the canonical executable. It no longer requires Compose
    files before dispatch.
- `cli/devctl/internal/commands/imagematrix/imagematrix.go`
  - Docker image checks are retained only for legacy `runtime.image` metadata.
    Flake-backed entries use Nix checks and the command no longer requires
    Compose files before dispatch.
- `cli/devctl/internal/agentexec/agentexec.go`
  - Docker exec command builders remain for legacy tmux/window command
    generation. Native command generation uses `BuildNativeCommand`.
- `cli/devctl/internal/commands/composecmd/compose.go`
  - Legacy cleanup of shared containers is retained to remove stale pre-native
    infrastructure when a legacy Compose path runs.
- `cli/devctl/main.go`
  - `worktrees-init` is retained as obsolete/manual guidance and does not need
    Compose files.
  - `ensureProjectReady` still contains legacy Compose readiness helpers, but
    native dispatch routes `dev-all` and `runtime.flake` overlays through
    native lifecycle/readiness before that helper is reachable.
- `cli/devctl/internal/testutil/fixture.go`
  - Compose cleanup helpers are test infrastructure for legacy integration
    coverage.
- `cli/devctl/internal/paths/paths.go` and layout metadata
  - `dev-all` path handling is a native layout compatibility rule, not a
    Compose runtime path.

## Native-Replaced Surfaces

These user-facing commands must not emit `docker compose` or `docker exec` for
flake-backed overlays.

- Top-level lifecycle: `up`, `down`, `restart`, `status`, `logs`, `scale`,
  `ensure-ready`
- Agent entry: `exec`, `attach`, `exec-cd`, `attach-cd`
- Hooks, auth, and checks: `warm`, `maintain`, `codex-auth`, `creds`,
  `check-net`, `check-codex`, `check-sts`, `codex-test`, `codex-debug`,
  `verify`, `doctor-runtime`
- Layout and tmux: `layout-apply`, `layout-generate`, `tmux-sync`,
  `tmux-add-cd`, `tmux-apply-layout`, `tmux-shells`, `open`, `fresh-open`,
  `reset`, `wt-open`, `layout-validate`, `worktrees-tmux`, `run`, `bootstrap`
- Worktree and repo helpers: `worktrees-setup`, `worktrees-branch`,
  `worktrees-init`, `worktrees-status`, `worktrees-sync`, `ssh-setup`,
  `ssh-test`, `repo-config-ssh`, `repo-config-https`, `repo-push-ssh`,
  `repo-push-https`
- Registry utilities that never need Compose files: `allow`, `broker`,
  `image-matrix`, `preflight`, `verify-all`, `wt-release`,
  `tmux-bell-install`, `tmux-bell-show-config`, `tmux-notify-bell`

Dry-run integration tests assert these native paths do not emit Compose/Docker
exec commands and do not fail on legacy workspace validation when a
flake-backed overlay intentionally points at a missing workspace.

## Audit Command

Repeat the inventory with:

```bash
rg -n 'runner\.Compose|ComposeInteractive|docker compose|docker exec|project == "dev-all"|project != "dev-all"|HasRuntimeFlake|runtime\.flake' cli/devctl -S
```

New hits should be classified in this document and either added to
`commandNeedsComposeFiles` coverage or covered by a focused test.
