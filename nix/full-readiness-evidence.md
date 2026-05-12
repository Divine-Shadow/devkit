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

- Command exit code: `0`.
- Native runtime: `runtime_ready: 1/1`.
- Launchable capacity: `capacity_available: 1/1`.
- Repo readiness: `repo_ready: 0/1`.
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

The frontend test evidence included `10` Vitest files and `26` tests passing
after lockfile install, the Nix `purs` shim, `better-sqlite3` rebuild, Spago
build, generated PureScript typings, and Vitest.

## Remaining Blocker

`core-check` is the only failing readiness check:

```bash
bash scripts/sbt2 "Compile / compile"
```

Observed failure class:

- `scripts/sbt2` starts successfully under the native Nix runtime.
- Java, SBT, filesystem writes, and repo access are available.
- The failure occurs during the `ouroboros-ide` Scala build.
- Representative errors include missing repo symbols such as
  `annotations.canonicalName`, `documentationPath`, and `model.*` imports in
  `tools/submit-to-ci` and backend sources.
- Later failures include Dotty backend write errors and
  `NullPointerException: Cannot invoke "dotty.tools.io.AbstractFile.jpath()"
  because "clsFile" is null`.

Classification: external `ouroboros-ide` repo/build blocker. The native runtime
now reaches the repo build and exposes source/build-graph failures; the
remaining failure is not a missing devkit runtime tool, Compose fallback, broker
failure, proxy issue, Playwright issue, Git/SSH setup issue, or `/usr/bin/env`
sandbox issue.
