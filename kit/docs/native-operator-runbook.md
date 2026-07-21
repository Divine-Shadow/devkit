# Native Operator Runbook

Devkit's supported runtime is Nix flakes plus the native sandbox. For ordinary
non-Product overlays, use `kit/scripts/devkit` as the operator entrypoint. It
execs `kit/bin/devctl` and fails loudly if the compiled binary is missing.

Product (`ouroboros-ide`) is outside this raw native operator surface. Product
source acquisition, consumer construction, auth seeding, and GUI lifecycle
must use only the adapter and launcher named by the installed
`fleet-runtime-authority/v1` manifest. Do not use raw `kit/scripts/devkit`,
`devctl`, `native prepare`, `native exec`, `attach`, `ssh-setup`, or
`codex-auth reseed` to construct or repair a Product consumer. The Devkit
consumer-boundary diagnostic cannot promote Product; the governed Product-owned
promotion app must prove the complete lifecycle twice from absent consumers.

## Build

```bash
make -C cli/devctl build
```

The build writes `kit/bin/devctl`. Direct binary invocation is diagnostic only;
scripts and runbooks for ordinary non-Product overlays should use
`kit/scripts/devkit`.

## Launch

Start one or more agents for an overlay:

```bash
kit/scripts/devkit -p ouroboros-static-front-end up --repo ouroboros-static-front-end --count 2
kit/scripts/devkit -p ouroboros-static-front-end status --repo ouroboros-static-front-end --ready
```

For a single-overlay repo, use that overlay:

```bash
kit/scripts/devkit -p ouroboros-static-front-end up --repo ouroboros-static-front-end --count 1
```

## Exec And Attach

Run a non-interactive command inside an agent sandbox:

```bash
kit/scripts/devkit -p ouroboros-static-front-end exec 1 --repo ouroboros-static-front-end -- bash -lc 'pwd && codex --version'
```

Open an interactive shell:

```bash
kit/scripts/devkit -p ouroboros-static-front-end attach 1 --repo ouroboros-static-front-end
```

The sandbox should expose:

- `DEVKIT_NATIVE_AGENT=1`
- the overlay-declared `CODEX_HOME` and agent-state paths
- `DOCKER_HOST=unix://...` only when brokered OCI access is configured
- no direct `/var/run/docker.sock` bind for standard agents

## Scale

Resize the agent set:

```bash
kit/scripts/devkit -p ouroboros-static-front-end scale 3 --repo ouroboros-static-front-end
kit/scripts/devkit -p ouroboros-static-front-end status --repo ouroboros-static-front-end --ready
```

`scale` updates native manifests and preserves per-agent state under the native
state root.

## Down

Stop broker state for the overlay and clean lifecycle metadata:

```bash
kit/scripts/devkit -p ouroboros-static-front-end down --repo ouroboros-static-front-end --count 3
```

Use the same broker socket and state-root overrides on `down` that were used on
`up` when running isolated smokes.

## Troubleshooting

Broker:

```bash
kit/scripts/devkit -p ouroboros-static-front-end broker status --format json
kit/scripts/devkit -p ouroboros-static-front-end logs --repo ouroboros-static-front-end --tail 50 --format json
```

If brokered OCI commands fail, confirm that `DOCKER_HOST` points at the
broker socket in `exec`, that the socket exists, and that the requested image is
allowed by overlay policy.

Egress:

```bash
kit/scripts/devkit -p ouroboros-static-front-end exec 1 --repo ouroboros-static-front-end -- bash -lc 'env | grep -E "HTTP_PROXY|HTTPS_PROXY|NO_PROXY"'
```

A blocked host should fail through the proxy; an allowed host should connect.
`make native-runtime-smoke` exercises both paths.

Nix:

Overlay runtime refs are intentionally overlay-local, for example
`./overlays/dev-all#default`. Automation must pass `--output-lock-file
/dev/null` when running overlay flakes directly. Do not commit generated
`overlays/*/flake.lock` files.

Decision: keep overlay flakes lockless. The root `flake.lock` remains the
single pin source; committing one lock per overlay would remove the warning but
would duplicate pins and create drift risk. The supported friction reduction is
to use devkit commands or smoke scripts, all of which pass `--output-lock-file
/dev/null`
for direct overlay checks.

Auth:

Codex and SSH state for ordinary non-Product overlays live under each agent
home. For overlays using the repo-local home layout, the active Codex home is:

- agent 1: `<dev-root>/<repo>/.devhome-agent1/.codex`
- agent N: `<dev-root>/agent-worktrees/agentN/.devhome-agentN/.codex`

The native state root under `.devkit/native-agents/<project>-agentN/` remains
for manifests, resolver state, broker metadata, and legacy imports. Native
prepare imports only missing Codex files from the old
`.devkit/native-agents/<project>-agentN/home/.codex` location; it does not
delete files or overwrite existing auth, config, sessions, logs, rollouts,
shell snapshots, or SQLite state.

To inspect the active state:

```bash
kit/scripts/devkit -p ouroboros-static-front-end exec 1 --repo ouroboros-static-front-end -- bash -lc 'printf "HOME=%s\nCODEX_HOME=%s\n" "$HOME" "$CODEX_HOME"; ls -la "$CODEX_HOME"'
```

Use the native reseed commands when auth needs to be refreshed:

```bash
kit/scripts/devkit -p ouroboros-static-front-end codex-auth reseed 1 --repo ouroboros-static-front-end
kit/scripts/devkit -p ouroboros-static-front-end ssh-setup /path/to/id_ed25519 --index 1
```

## Add A New Overlay Flake

1. Create `overlays/<overlay>/runtime.nix`.
2. Create `overlays/<overlay>/flake.nix` as a thin wrapper around the root
   flake output for that overlay.
3. Add `runtime.flake: ./overlays/<overlay>#default` to
   `overlays/<overlay>/devkit.yaml`.
4. Add `runtime.codex_version` and `runtime.core_check`.
5. Add the overlay to `flake.nix` root outputs and to smoke scripts when it is
   intended to be part of the supported matrix.
6. Run:

```bash
nix --extra-experimental-features 'nix-command flakes' flake check
nix --extra-experimental-features 'nix-command flakes' develop --command nix/validate-overlay-runtimes.py overlays
kit/scripts/devkit --dry-run -p ouroboros-static-front-end runtime-matrix --all --check
make overlay-runtime-smoke
make native-overlay-matrix
```

## Local And CI Gates

Cheap CI-facing gate:

```bash
make ci-cheap
```

Full local readiness gate:

```bash
make native-e2e-lifecycle
make native-overlay-e2e-matrix
make native-runtime-smoke
make native-readiness-audit
make postgres-broker-container-smoke
```

`make native-e2e-lifecycle` runs real native `up`, `status --ready`, `exec`,
stdin-driven `attach`, `scale`, and `down` cycles for `dev-all` and the
configured smaller overlay, defaulting to `ouroboros-static-front-end`.

`make native-overlay-e2e-matrix` classifies real overlays as `e2e-pass`,
`runtime-pass`, or `not-locally-runnable` with per-overlay evidence under
`/tmp`. It preserves the cheap CI boundary because full E2E coverage expects
sibling repository checkouts and creates temporary native Git worktrees.

`make postgres-broker-container-smoke` starts the Nix-built Postgres broker,
denies a Redis create request, pulls/creates/starts/inspects/deletes a real
Postgres container through the broker socket, and then cleans up.
