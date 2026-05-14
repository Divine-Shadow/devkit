# Devkit Operator Notes

The supported runtime is Nix flakes plus the native devkit sandbox. Use `kit/scripts/devkit` from the repository root or an absolute path to that wrapper. It execs `kit/bin/devctl`.

## Build And Run

```bash
make -C cli/devctl build
kit/scripts/devkit -p dev-all up --repo ouroboros-ide --count 2
kit/scripts/devkit -p dev-all status --repo ouroboros-ide --ready
kit/scripts/devkit -p dev-all exec 1 --repo ouroboros-ide -- bash -lc 'pwd && codex --version'
kit/scripts/devkit -p dev-all down --repo ouroboros-ide --count 2
```

Overlay metadata lives in `overlays/<project>/devkit.yaml`. A supported overlay must declare an overlay-local `runtime.flake`, `defaults.repo`, and the readiness policy needed by that repo. Overlay-local flakes are available at `overlays/<project>/flake.nix`.

The detailed operator runbook is
[`native-operator-runbook.md`](native-operator-runbook.md).

## Native Agent State

Native agents use host worktrees plus sandboxed agent homes:

- Worktrees: `../agent-worktrees/agent<N>/<repo>` by default.
- Agent state: `../.devkit/native-agents/<project>/agent<N>`.
- Codex state and SSH config are seeded into the agent home.
- Docker access is available only when the native broker policy provides a socket.

## Checks

Use these gates for local changes:

```bash
cd cli/devctl && go test -count=1 ./...
make -C cli/devctl build
nix flake check
make overlay-runtime-smoke
make native-overlay-matrix
make native-e2e-lifecycle
make native-overlay-e2e-matrix
make native-runtime-smoke
make native-readiness-audit
make postgres-broker-container-smoke
make compose-retirement-guard
```

Historical migration notes are archived under `documentation/archive/compose-retirement/`.
