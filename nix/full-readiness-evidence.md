# Full Native Readiness Evidence

Date: 2026-05-12

Command shape:

```bash
smoke_dir=$(mktemp -d /tmp/devkit-full-ready.XXXXXX)
sock="$smoke_dir/broker.sock"
state="$smoke_dir/broker-state"

kit/scripts/devkit -p dev-all up --repo ouroboros-ide --count 1 \
  --flake .#dev-all \
  --broker-socket "$sock" \
  --broker-state-root "$state" \
  --allow-image postgres:latest \
  --allow-pulls \
  --skip-ready \
  --format json

kit/scripts/devkit -p dev-all ensure-ready --repo ouroboros-ide --count 1 \
  --flake .#dev-all \
  --broker-socket "$sock" \
  --format json

kit/scripts/devkit -p dev-all down --repo ouroboros-ide --count 1 \
  --broker-socket "$sock" \
  --broker-state-root "$state" \
  --format json
```

## Result

- `up --skip-ready` exit code: `0`.
- `ensure-ready` exit code: `0`.
- `down` exit code: `0`.
- Native runtime: `runtime_ready: 1/1`.
- Launchable capacity: `capacity_available: 1/1`.
- Repo readiness: `repo_ready: 1/1`.
- Broker cleanup: final `down` reported `running: false`.

## Passing Checks

- `prepare-state`
- `sandbox-command`
- `broker-socket`
- `required-tools`
- `purescript-spago-netlify`
- `playwright-browser`
- `git-remote`
- `frontend-warm`
- `frontend-playwright-browser`
- `frontend-typecheck`
- `frontend-test`
- `core-check`

The frontend test evidence included `10` Vitest files and `26` tests passing
after lockfile install, the Nix `purs` shim, `better-sqlite3` rebuild, Spago
build, generated PureScript typings, and Vitest.

The core check now passes:

```bash
bash scripts/sbt2 "Compile / compile"
```

Observed SBT result:

- `submit-to-ci`, `backend`, `invariants`, `services`, and the remaining
  aggregate compile graph completed.
- Representative final line:
  `[success] elapsed time: 23 s, cache 100%, 402 disk cache hits`.

## Additional Probe

The same core check also passed in an isolated clean worktree mounted through
the native agent1 path:

```bash
kit/scripts/devkit -p dev-all native exec \
  --repo ouroboros-ide-core-readiness \
  --flake .#dev-all \
  -- bash -lc 'bash scripts/sbt2 "Compile / compile"'
```

Result: exit code `0`, `[success] elapsed time: 245 s (0:04:05.0), cache 77%,
312 disk cache hits, 90 onsite tasks`.

## Operational Caveat

Running cold `ensure-ready` without a running broker correctly reports
`runtime_ready: 0/1`, `capacity_available: 0/1`, and a failing `broker-socket`
check. Use `up` or `up --skip-ready` first when a no-skip readiness pass should
include brokered Testcontainers capacity.
