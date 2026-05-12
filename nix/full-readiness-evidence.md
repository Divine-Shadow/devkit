# Full Native Readiness Evidence

Date: 2026-05-12

Command shape:

```bash
kit/scripts/devkit -p dev-all ensure-ready --repo ouroboros-ide --count 1 \
  --flake .#dev-all \
  --format json

kit/scripts/devkit -p dev-all ensure-ready --repo ouroboros-ide --count 1 \
  --flake .#dev-all \
  --skip-broker \
  --format json

kit/scripts/devkit -p dev-all down --repo ouroboros-ide --count 1 \
  --format json

kit/scripts/devkit -p dev-all up --repo ouroboros-ide --count 1 \
  --flake .#dev-all \
  --skip-ready \
  --format json

kit/scripts/devkit -p dev-all ensure-ready --repo ouroboros-ide --count 1 \
  --flake .#dev-all \
  --format json

kit/scripts/devkit -p dev-all down --repo ouroboros-ide --count 1 \
  --format json
```

## Result

- Cold `ensure-ready` exit code: `0`.
- `ensure-ready --skip-broker` exit code: `0`.
- `up --skip-ready` exit code: `0`.
- `ensure-ready` after `up --skip-ready` exit code: `0`.
- `down` exit code: `0` for both cleanup calls.
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

`ensure-ready` starts or reuses the managed broker by default. Use
`--skip-broker` only when intentionally checking current broker state without
starting it first. A stopped-broker probe with `--skip-broker --skip-repo-checks`
returned exit code `2`, reported `broker-socket` as false, and left broker
status at `running: false`.
