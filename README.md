# Devkit

Devkit runs project agents through Nix flakes and the native devkit sandbox. For ordinary non-Product overlays, the supported entrypoint is `kit/scripts/devkit`, which execs the compiled CLI at `kit/bin/devctl`.

## Requirements

- Nix with flakes enabled.
- Bubblewrap for native sandbox launch.
- A host container daemon only when a native overlay intentionally uses brokered OCI access, such as testcontainers through the devkit broker.
- `tmux` for terminal sessions.

Build the CLI once:

```bash
make -C cli/devctl build
```

Run commands through the wrapper:

```bash
kit/scripts/devkit -p ouroboros-static-front-end up --repo ouroboros-static-front-end --count 2
kit/scripts/devkit -p ouroboros-static-front-end status --repo ouroboros-static-front-end --ready
kit/scripts/devkit -p ouroboros-static-front-end exec 1 --repo ouroboros-static-front-end -- bash -lc 'codex --version'
kit/scripts/devkit -p ouroboros-static-front-end ensure-ready --repo ouroboros-static-front-end --runtime-only
kit/scripts/devkit -p ouroboros-static-front-end down --repo ouroboros-static-front-end --count 2
```

If `kit/bin/devctl` is missing or not executable, the wrapper fails loudly and prints the build command. It does not silently choose another implementation.

## Product Authority Boundary

Product (`ouroboros-ide`) is not constructed or launched through the raw
wrapper examples above. The authoritative Nix derivation emits one immutable
`fleet-runtime-authority/v1` manifest. Product consumers must use only the
adapter and launcher paths named by that installed manifest; those consumers
may verify the manifest but may not select source, rebuild a runtime, or
reinterpret identity. The Devkit `product-consumer-boundary-diagnostic` check
is prerequisite evidence only. Product promotion requires the governed
Product-owned promotion app to complete the entire lifecycle on two fresh
consumers.

## Runtime Model

Each supported overlay declares an overlay-local `runtime.flake` in `overlays/<project>/devkit.yaml`, for example `./overlays/dev-all#default`. The root flake still exposes compatible shells for direct Nix use, but devkit runtime metadata is one flake ref per overlay.

Native agents use per-agent worktrees and state directories under the dev root. The sandbox binds only the host paths needed for the selected plan, seeds Codex and SSH state into the agent home, and exposes OCI API access only through configured broker sockets.

For day-to-day operations, see the native runbook:
`kit/docs/native-operator-runbook.md`.

## Common Commands

- `up`, `down`, `restart`, `status`, `logs`: native lifecycle.
- `scale`: resize a native agent set.
- `exec`, `attach`: enter a native sandbox for an agent index.
- `ensure-ready`: run runtime or repo readiness checks.
- `native plan`: inspect the computed sandbox plan.
- `runtime-matrix --all --check`: validate overlay-to-flake metadata.
- `nix run .#management-inspection`: explicitly refresh or enter a read-only,
  revision-identified source view for Management inspection.
- `preflight`: check host prerequisites.
- `verify-all`: run the supported verification flow for configured overlays.

## Verification

The primary local gates are:

```bash
make -C cli/devctl build
cd cli/devctl && go test -count=1 ./...
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

Historical migration notes live under `documentation/archive/compose-retirement/`. They are retained for context and are not supported runtime documentation.
