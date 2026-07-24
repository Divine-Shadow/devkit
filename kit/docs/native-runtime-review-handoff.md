# Native Runtime Review Handoff

## Summary

Flake-backed overlays now have Nix-native top-level lifecycle and exec paths.
Canonical user entry remains `kit/scripts/devkit`, which execs the compiled
`kit/bin/devctl` binary.

Default lifecycle and entry commands are native for overlays with
`runtime.flake`:
`up`, `down`, `restart`, `status`, `logs`, `scale`, `exec`, `attach`, and
`ensure-ready`. The retired runtime namespace fails before invoking any runtime.

## Review Commits

- `60104d0 test: cover native dev-all default helpers`
- `b54dd00 fix: support symlinked native worktrees`
- `f16d01f docs: record native runtime stabilization`
- `a23e313 feat: add native runtime smoke target`

## Repeatable Validation

Primary smoke:

```bash
nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#gnumake nixpkgs#go -c make native-runtime-smoke
```

The smoke covers:

- native `up`, lightweight `status`, `logs`, `scale`, and `down`
- dry-run `attach`
- real native `exec`
- broker policy deny for Redis and allow/create/delete for Postgres
- runtime-only `ensure-ready`
- repo-failure capacity preservation
- Spago, Playwright, and Netlify availability

Merge gate:

```bash
nix --extra-experimental-features 'nix-command flakes' flake check
cd cli/devctl && nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#go -c env CGO_ENABLED=0 go test -count=1 ./...
cd brokers/postgres-broker && nix --extra-experimental-features 'nix-command flakes' shell nixpkgs#go -c env CGO_ENABLED=0 go test -count=1 ./...
```

## Behavioral Notes

- `status` is lightweight by default. Use `status --ready`, `ensure-ready`, or
  `native capacity` when readiness/capacity checks should run.
- `ensure-ready` starts or reuses the managed broker before capacity checks;
  `--skip-broker` preserves the current-state-only broker check path.
- `up`, `restart`, and `scale` still run readiness unless `--skip-ready` is
  supplied.
- Repository readiness is intentionally separate from runtime capacity. Repo
  check failure is visible and retryable, but it does not hide launchable native
  agent capacity.
- Existing symlinked `agent1` worktrees are supported by projecting the resolved
  target into the mounted `/workspaces/dev/<repo>` path. New preparation should
  still create dedicated native worktrees.

## Retired Runtime Boundary

- The explicit retired runtime namespace is rejected before launch.
- Lifecycle and exec dispatch are owned by the native command registry.
- Supported overlays must declare `runtime.flake`.
- Diagnostics refuse unsupported runtime shapes instead of falling back.

## Operational Requirements

- Host Nix with flakes enabled.
- `bubblewrap` available for native agent execution.
- Host container daemon reachable through the configured upstream socket,
  defaulting to `/var/run/docker.sock`, for broker-backed OCI smokes. The socket
  is consumed by the broker and is not mounted into native agents.
- `postgres:latest` must be present or pullable for the broker allow-path smoke.
- The `dev-all` broker socket defaults under the managed devkit state root:
  `<dev-root>/.devkit/native-broker/broker.sock`. Repeatable smokes can still
  use a temporary socket/state root.

## Rollback

The migration is split into coherent commits. To back out only the review
readiness layer, revert `a23e313`. To back out the symlink compatibility fix,
revert `b54dd00`. To return before the broader native default transition, revert
the native runtime commits in reverse order from the branch tip.

Do not change `kit/scripts/devkit` as part of rollback unless the wrapper
contract itself is explicitly reopened.
