# Devkit Operator Notes

The supported runtime is Nix flakes plus the native devkit sandbox. For ordinary non-Product overlays, use `kit/scripts/devkit` from the repository root or an absolute path to that wrapper. It execs `kit/bin/devctl`.

## Build And Run

```bash
make -C cli/devctl build
kit/scripts/devkit -p ouroboros-static-front-end up --repo ouroboros-static-front-end --count 2
kit/scripts/devkit -p ouroboros-static-front-end status --repo ouroboros-static-front-end --ready
kit/scripts/devkit -p ouroboros-static-front-end exec 1 --repo ouroboros-static-front-end -- bash -lc 'pwd && codex --version'
kit/scripts/devkit -p ouroboros-static-front-end down --repo ouroboros-static-front-end --count 2
```

Product (`ouroboros-ide`) is an explicit exception. Its source acquisition,
consumer construction, and GUI lifecycle consume only the adapter and launcher
named by the installed `fleet-runtime-authority/v1` manifest. Raw wrapper,
binary, and native-lifecycle calls are diagnostic/non-Product interfaces and
cannot promote Product. Promotion belongs exclusively to the governed
Product-owned twice-fresh promotion app.

Overlay metadata lives in `overlays/<project>/devkit.yaml`. A supported overlay must declare an overlay-local `runtime.flake`, `defaults.repo`, and the readiness policy needed by that repo. Overlay-local flakes are available at `overlays/<project>/flake.nix`.

The detailed operator runbook is
[`native-operator-runbook.md`](native-operator-runbook.md).

## Native Agent State

Native agents use host worktrees plus sandboxed agent homes:

- Worktrees: `../agent-worktrees/agent<N>/<repo>` by default.
- Agent state: `../.devkit/native-agents/<project>-agent<N>`.
- Some ordinary overlays use a repo-local `.devhome-agentN`; their overlay
  contract defines that layout. Product home/auth placement is instead fixed
  by the installed authority manifest and its adapter.
- OCI API access is available only when the native broker policy provides a socket.

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
make retired-runtime-guard
make nix-overlay-runtime-guard
```

Historical migration notes are archived under `documentation/archive/compose-retirement/`.
