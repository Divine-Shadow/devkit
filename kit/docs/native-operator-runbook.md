# Native Operator Runbook

Devkit's supported runtime is Nix flakes plus the native sandbox. Use
`kit/scripts/devkit` as the operator entrypoint. It execs `kit/bin/devctl` and
fails loudly if the compiled binary is missing.

## Build

```bash
make -C cli/devctl build
```

The build writes `kit/bin/devctl`. Direct binary invocation is allowed for power
users, but scripts and runbooks should use `kit/scripts/devkit`.

## Launch

Start one or more agents for an overlay:

```bash
kit/scripts/devkit -p dev-all up --repo ouroboros-ide --count 2
kit/scripts/devkit -p dev-all status --repo ouroboros-ide --ready
```

For a single-overlay repo, use that overlay:

```bash
kit/scripts/devkit -p ouroboros-static-front-end up --repo ouroboros-static-front-end --count 1
```

## Exec And Attach

Run a non-interactive command inside an agent sandbox:

```bash
kit/scripts/devkit -p dev-all exec 1 --repo ouroboros-ide -- bash -lc 'pwd && codex --version'
```

Open an interactive shell:

```bash
kit/scripts/devkit -p dev-all attach 1 --repo ouroboros-ide
```

The sandbox should expose:

- `DEVKIT_NATIVE_AGENT=1`
- `CODEX_HOME` under the agent state root
- `DOCKER_HOST=unix://...` only when brokered Docker is configured
- no direct `/var/run/docker.sock` bind for standard agents

## Scale

Resize the agent set:

```bash
kit/scripts/devkit -p dev-all scale 3 --repo ouroboros-ide
kit/scripts/devkit -p dev-all status --repo ouroboros-ide --ready
```

`scale` updates native manifests and preserves per-agent state under the native
state root.

## Down

Stop broker state for the overlay and clean lifecycle metadata:

```bash
kit/scripts/devkit -p dev-all down --repo ouroboros-ide --count 3
```

Use the same broker socket and state-root overrides on `down` that were used on
`up` when running isolated smokes.

## Troubleshooting

Broker:

```bash
kit/scripts/devkit -p dev-all broker status --format json
kit/scripts/devkit -p dev-all logs --repo ouroboros-ide --tail 50 --format json
```

If brokered Docker commands fail, confirm that `DOCKER_HOST` points at the
broker socket in `exec`, that the socket exists, and that the requested image is
allowed by overlay policy.

Egress:

```bash
kit/scripts/devkit -p dev-all exec 1 --repo ouroboros-ide -- bash -lc 'env | grep -E "HTTP_PROXY|HTTPS_PROXY|NO_PROXY"'
```

A blocked host should fail through the proxy; an allowed host should connect.
`make native-runtime-smoke` exercises both paths.

Nix:

Overlay runtime refs are intentionally overlay-local, for example
`./overlays/dev-all#default`. Automation must pass `--no-write-lock-file` when
running overlay flakes directly. Do not commit generated
`overlays/*/flake.lock` files.

Decision: keep overlay flakes lockless. The root `flake.lock` remains the
single pin source; committing one lock per overlay would remove the warning but
would duplicate pins and create drift risk. The supported friction reduction is
to use devkit commands or smoke scripts, all of which pass `--no-write-lock-file`
for direct overlay checks.

Auth:

Codex and SSH state live under each agent home. To inspect the active state:

```bash
kit/scripts/devkit -p dev-all exec 1 --repo ouroboros-ide -- bash -lc 'printf "HOME=%s\nCODEX_HOME=%s\n" "$HOME" "$CODEX_HOME"; ls -la "$CODEX_HOME"'
```

Use the native reseed commands when auth needs to be refreshed:

```bash
kit/scripts/devkit -p dev-all codex-auth reseed 1 --repo ouroboros-ide
kit/scripts/devkit -p dev-all ssh-setup /path/to/id_ed25519 --index 1
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
kit/scripts/devkit --dry-run -p dev-all image-matrix --all --check
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
make native-runtime-smoke
make native-readiness-audit
make postgres-broker-container-smoke
```

`make postgres-broker-container-smoke` starts the Nix-built Postgres broker,
denies a Redis create request, pulls/creates/starts/inspects/deletes a real
Postgres container through the broker socket, and then cleans up.
